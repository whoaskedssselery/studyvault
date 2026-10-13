package httpapi

import (
	"testing"
	"time"
)

func newTestLimiter(limit int) (*rateLimiter, *time.Time) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(limit, time.Minute)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestRateLimiter_AllowsUpToTheLimitThenBlocks(t *testing.T) {
	l, _ := newTestLimiter(3)

	for i := 1; i <= 3; i++ {
		if ok, _ := l.allow("1.2.3.4"); !ok {
			t.Fatalf("attempt %d of 3 must be allowed", i)
		}
	}
	ok, retry := l.allow("1.2.3.4")
	if ok {
		t.Fatal("the 4th attempt within the window must be blocked")
	}
	if retry <= 0 || retry > time.Minute {
		t.Errorf("retry-after = %v, want within the window", retry)
	}
}

func TestRateLimiter_WindowResets(t *testing.T) {
	l, now := newTestLimiter(2)
	l.allow("k")
	l.allow("k")
	if ok, _ := l.allow("k"); ok {
		t.Fatal("must be blocked")
	}

	*now = now.Add(time.Minute + time.Second)

	if ok, _ := l.allow("k"); !ok {
		t.Error("a new window must start fresh")
	}
}

func TestRateLimiter_RetryAfterShrinksAsTheWindowPasses(t *testing.T) {
	l, now := newTestLimiter(1)
	l.allow("k")
	_, first := l.allow("k")

	*now = now.Add(40 * time.Second)
	_, later := l.allow("k")

	if later >= first || later > 20*time.Second {
		t.Errorf("retry-after must count down: %v then %v", first, later)
	}
}

func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(1)
	l.allow("a")

	if ok, _ := l.allow("b"); !ok {
		t.Error("another client must have its own budget")
	}
	if ok, _ := l.allow("a"); ok {
		t.Error("the first client is still limited")
	}
}

func TestRateLimiter_ForgetsExpiredClients(t *testing.T) {
	l, now := newTestLimiter(5)
	for i := 0; i < 50; i++ {
		l.allow(string(rune('a' + i)))
	}

	*now = now.Add(3 * time.Minute)
	l.allow("fresh") // обращение запускает уборку устаревших записей

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) != 1 {
		t.Errorf("memory must not grow without bound: %d entries remain, want 1", len(l.buckets))
	}
}
