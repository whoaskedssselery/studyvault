package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
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
	apiPrefix + "/auth/login":    true,
	apiPrefix + "/auth/register": true,
	apiPrefix + "/auth/refresh":  true,
	apiPrefix + "/me/password":   true,
}

// limitAuthAttempts ограничивает число POST-запросов к /auth/* и /me/password с одного адреса в минуту; 0 выключает ограничение.
// Клиент определяется по адресу соединения, а не по X-Forwarded-For: этот заголовок присылает сам клиент,
// и подделкой он получил бы новый лимит на каждый запрос. За обратным прокси все пользователи
// выглядят как один адрес, так что там лимит нужно задавать на самом прокси.
func limitAuthAttempts(perMinute int) func(http.Handler) http.Handler {
	if perMinute <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	limiter := newRateLimiter(perMinute, time.Minute)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && limitedPaths[r.URL.Path] {
				if ok, retry := limiter.allow(clientIP(r)); !ok {
					w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
					writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
