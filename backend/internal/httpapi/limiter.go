package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rateLimiter — счётчик попыток с фиксированным окном, отдельный на каждый ключ.
// Живёт в памяти одного процесса, а для нескольких экземпляров API нужен общий счётчик (например, Redis).
type rateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	swept   time.Time
}

type bucket struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: time.Now, buckets: map[string]*bucket{}}
}

// allow считает попытку и говорит, можно ли её пропустить. Если нет, возвращает, через сколько окно закончится.
func (l *rateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok || now.Sub(b.start) >= l.window {
		b = &bucket{start: now}
		l.buckets[key] = b
	}
	b.count++
	if b.count > l.limit {
		return false, b.start.Add(l.window).Sub(now)
	}
	return true, 0
}

// sweep раз в окно выбрасывает записи клиентов, которые давно ничего не присылали,
// чтобы память не росла от каждого нового адреса.
func (l *rateLimiter) sweep(now time.Time) {
	if now.Sub(l.swept) < l.window {
		return
	}
	l.swept = now
	for k, b := range l.buckets {
		if now.Sub(b.start) >= l.window {
			delete(l.buckets, k)
		}
	}
}

// Попытки подбора пароля и массовой регистрации идут сюда, включая смену пароля: с украденным access-токеном
// текущий пароль иначе можно было бы подбирать без ограничений. Выход (logout) не считается.
var limitedPaths = map[string]bool{
	apiPrefix + "/auth/login":                      true,
	apiPrefix + "/auth/register":                   true,
	apiPrefix + "/auth/refresh":                    true,
	apiPrefix + "/me/password":                     true,
	apiPrefix + "/auth/password-reset":             true,
	apiPrefix + "/auth/password-reset/confirm":     true,
	apiPrefix + "/auth/email-verification/confirm": true,
	apiPrefix + "/me/email-verification":           true,
}

// limitAuthAttempts ограничивает число попыток подбора пароля с одного адреса в минуту; 0 выключает ограничение.
// Клиент определяется по адресу соединения, а не по X-Forwarded-For: этот заголовок присылает сам клиент,
// и подделкой он получил бы новый лимит на каждый запрос. Исключение: соединение пришло от доверенного прокси
// (TRUSTED_PROXIES), тогда адрес клиента берётся из X-Forwarded-For, который прокси проставляет сам (см. clientIP).
func limitAuthAttempts(perMinute int, trusted []*net.IPNet) func(http.Handler) http.Handler {
	if perMinute <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	limiter := newRateLimiter(perMinute, time.Minute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limited(r) {
				if ok, retry := limiter.allow(clientIP(r, trusted)); !ok {
					w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
					writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// limited — запросы, которыми можно подбирать пароль или рассылать письма. Удаление аккаунта тоже требует пароля,
// поэтому DELETE /me считается так же, как смена пароля.
func limited(r *http.Request) bool {
	return (r.Method == http.MethodPost && limitedPaths[r.URL.Path]) ||
		(r.Method == http.MethodDelete && r.URL.Path == apiPrefix+"/me")
}

// clientIP — адрес, по которому считается лимит. Если соединение пришло не от доверенного прокси, это адрес соединения, а
// X-Forwarded-For игнорируется целиком: его мог подделать кто угодно. Если от доверенного, заголовок читается справа налево,
// и берётся первый адрес, который сам не принадлежит доверенному прокси: именно его добавил ближайший доверенный прокси,
// а то, что левее, прислал клиент. Нечитаемый или пустой заголовок возвращает к адресу соединения.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote := net.ParseIP(host)
	if remote == nil || !inNets(remote, trusted) {
		return host
	}
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			return host // мусор в заголовке: не гадаем
		}
		if !inNets(ip, trusted) {
			return ip.String()
		}
	}
	return host
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
