package httpapi_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
)

func limitedHandler(perMinute int) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{
		DB: fakePinger{}, JWTSecret: testSecret, AuthRateLimit: perMinute,
		Auth: &fakeAuth{loginErr: auth.ErrInvalidCredentials},
	})
}

func attempt(h http.Handler, method, path, remoteAddr, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"email":"a@x.io","password":"wrong"}`))
	req.RemoteAddr = remoteAddr
	if header != "" {
		req.Header.Set("X-Forwarded-For", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRateLimit_StopsPasswordGuessing(t *testing.T) {
	h := limitedHandler(3)

	for i := 1; i <= 3; i++ {
		if rec := attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:5000", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (a wrong password, not yet limited)", i, rec.Code)
		}
	}
	rec := attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:5000", "")

	if rec.Code != http.StatusTooManyRequests || errorCode(t, rec) != "rate_limited" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || secs < 1 || secs > 60 {
		t.Errorf("Retry-After = %q, want 1..60 seconds", rec.Header().Get("Retry-After"))
	}
}

func TestAuthRateLimit_BudgetIsSharedByLoginRegisterAndRefresh(t *testing.T) {
	h := limitedHandler(2)

	attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:1", "")
	attempt(h, http.MethodPost, "/v1/auth/register", "192.0.2.1:2", "") // другой порт, тот же клиент

	if rec := attempt(h, http.MethodPost, "/v1/auth/refresh", "192.0.2.1:3", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429: a client must not get a fresh budget per endpoint or per source port", rec.Code)
	}
}

func TestAuthRateLimit_OtherClientsAreNotAffected(t *testing.T) {
	h := limitedHandler(1)
	attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:1", "")

	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:2", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatal("the first client must be limited")
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "198.51.100.7:1", ""); rec.Code == http.StatusTooManyRequests {
		t.Error("a different client must not be limited by someone else's attempts")
	}
}

func TestAuthRateLimit_SpoofedForwardedHeaderGivesNoFreshBudget(t *testing.T) {
	h := limitedHandler(1)
	attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:1", "")

	rec := attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:2", "203.0.113.99")

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429: the limit is keyed by the real connection, not by a header the client controls", rec.Code)
	}
}

func TestAuthRateLimit_OnlyCountsTheAuthEndpoints(t *testing.T) {
	h := limitedHandler(1)

	for i := 0; i < 5; i++ {
		if rec := attempt(h, http.MethodGet, "/v1/health", "192.0.2.1:1", ""); rec.Code != http.StatusOK {
			t.Fatalf("/v1/health must never be limited, status = %d", rec.Code)
		}
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/logout", "192.0.2.1:1", ""); rec.Code == http.StatusTooManyRequests {
		t.Error("logout is not a guessing target and must not be limited")
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:1", ""); rec.Code == http.StatusTooManyRequests {
		t.Error("the other traffic must not have used up the login budget")
	}
}

func TestAuthRateLimit_ZeroDisablesIt(t *testing.T) {
	h := limitedHandler(0)

	for i := 0; i < 50; i++ {
		if rec := attempt(h, http.MethodPost, "/v1/auth/login", "192.0.2.1:1", ""); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was limited although the limit is off", i+1)
		}
	}
}

func proxied(perMinute int, proxies ...string) http.Handler {
	var nets []*net.IPNet
	for _, p := range proxies {
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			panic(err)
		}
		nets = append(nets, n)
	}
	return httpapi.NewHandler(httpapi.Deps{
		DB: fakePinger{}, JWTSecret: testSecret, AuthRateLimit: perMinute, TrustedProxies: nets,
		Auth: &fakeAuth{loginErr: auth.ErrInvalidCredentials},
	})
}

// За доверенным прокси лимит считается по настоящему клиенту, а не по прокси: иначе один подбирающий пароль
// заблокировал бы вход всем.
func TestAuthRateLimit_BehindATrustedProxy_CountsEachClientSeparately(t *testing.T) {
	h := proxied(2, "172.16.0.0/12")
	proxy := "172.18.0.5:4000"

	for i := 0; i < 2; i++ {
		attempt(h, http.MethodPost, "/v1/auth/login", proxy, "203.0.113.7")
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", proxy, "203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the guessing client must be limited, status = %d", rec.Code)
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", proxy, "203.0.113.8"); rec.Code != http.StatusUnauthorized {
		t.Errorf("another client behind the same proxy must not be blocked, status = %d", rec.Code)
	}
}

// Прислать свой X-Forwarded-For клиент может всегда. Значение, подставленное им слева, не должно давать новый лимит:
// доверенный прокси дописывает настоящий адрес справа.
func TestAuthRateLimit_ForgedForwardedForBehindAProxyDoesNotHelpTheClient(t *testing.T) {
	h := proxied(2, "172.16.0.0/12")
	proxy := "172.18.0.5:4000"

	for i := 0; i < 2; i++ {
		attempt(h, http.MethodPost, "/v1/auth/login", proxy, fmt.Sprintf("1.1.1.%d, 203.0.113.7", i)) // подделка слева, настоящий адрес справа
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", proxy, "9.9.9.9, 203.0.113.7"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d: a forged left part must not reset the limit", rec.Code)
	}
}

func TestAuthRateLimit_ChainOfTrustedProxies_UsesTheFirstUntrustedAddress(t *testing.T) {
	h := proxied(1, "172.16.0.0/12", "10.0.0.0/8")
	for i := 0; i < 1; i++ {
		attempt(h, http.MethodPost, "/v1/auth/login", "172.18.0.5:1", "203.0.113.7, 10.1.2.3")
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "172.18.0.5:1", "203.0.113.7, 10.9.9.9"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("both requests come from 203.0.113.7 through two trusted hops: status = %d", rec.Code)
	}
}

func TestAuthRateLimit_ForwardedForFromAnUntrustedPeerIsIgnored(t *testing.T) {
	h := proxied(2, "172.16.0.0/12")
	for i := 0; i < 2; i++ {
		attempt(h, http.MethodPost, "/v1/auth/login", "198.51.100.9:1", fmt.Sprintf("5.5.5.%d", i))
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "198.51.100.9:2", "6.6.6.6"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("a peer that is not a trusted proxy cannot choose its own address: status = %d", rec.Code)
	}
}

func TestAuthRateLimit_UnreadableForwardedFor_FallsBackToTheProxyAddress(t *testing.T) {
	h := proxied(1, "172.16.0.0/12")
	attempt(h, http.MethodPost, "/v1/auth/login", "172.18.0.5:1", "not-an-address")
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "172.18.0.5:2", "also garbage"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d: with no usable header the proxy's own address is counted", rec.Code)
	}
	if rec := attempt(h, http.MethodPost, "/v1/auth/login", "172.18.0.6:2", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("another proxy address with no header: status = %d", rec.Code)
	}
}
