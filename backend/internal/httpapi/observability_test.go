package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// captureLogs подменяет логгер по умолчанию на JSON в буфер (через тот же обработчик, что и в main).
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(NewLogHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func observed(h http.HandlerFunc) http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, accessLog, recoverer)
	r.Get("/v1/notes/{id}", h)
	r.Get("/v1/health", h)
	return r
}

func TestRequestID_GeneratedWhenAbsent(t *testing.T) {
	rec := httptest.NewRecorder()
	observed(func(http.ResponseWriter, *http.Request) {}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/notes/1", nil))

	if id := rec.Header().Get("X-Request-ID"); !validRequestID.MatchString(id) {
		t.Errorf("X-Request-ID = %q, want a generated id", id)
	}
}

func TestRequestID_ClientValueIsKept(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/notes/1", nil)
	req.Header.Set("X-Request-ID", "trace-abc.123")
	rec := httptest.NewRecorder()
	observed(func(http.ResponseWriter, *http.Request) {}).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "trace-abc.123" {
		t.Errorf("X-Request-ID = %q, want the client's id", got)
	}
}

func TestRequestID_UnsafeClientValueIsReplaced(t *testing.T) {
	for _, bad := range []string{"has space", "semi;colon", strings.Repeat("a", 65), "кириллица", `quote"`} {
		req := httptest.NewRequest(http.MethodGet, "/v1/notes/1", nil)
		req.Header.Set("X-Request-ID", bad)
		rec := httptest.NewRecorder()
		observed(func(http.ResponseWriter, *http.Request) {}).ServeHTTP(rec, req)

		got := rec.Header().Get("X-Request-ID")
		if got == bad || !validRequestID.MatchString(got) {
			t.Errorf("client id %q gave %q, want a fresh safe id", bad, got)
		}
	}
}

func TestAccessLog_RecordsRouteStatusAndRequestID(t *testing.T) {
	buf := captureLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/notes/8f1c1e0a-0000-0000-0000-000000000000?q=secret+words", nil)
	req.Header.Set("X-Request-ID", "req-1")
	req.Header.Set("Authorization", "Bearer super-secret-token")
	observed(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }).ServeHTTP(httptest.NewRecorder(), req)

	lines := logLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %s", len(lines), buf)
	}
	l := lines[0]
	if l["route"] != "/v1/notes/{id}" || l["status"] != float64(http.StatusTeapot) || l["method"] != "GET" || l["request_id"] != "req-1" {
		t.Errorf("unexpected record: %v", l)
	}
	// Ни query string, ни заголовки, ни конкретный id заметки в лог не попадают.
	for _, leak := range []string{"secret", "super-secret-token", "8f1c1e0a"} {
		if strings.Contains(buf.String(), leak) {
			t.Errorf("log leaks %q: %s", leak, buf)
		}
	}
}

func TestAccessLog_ServerErrorsAreErrorLevel_HealthIsDebug(t *testing.T) {
	buf := captureLogs(t)
	observed(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/notes/1", nil))
	observed(func(http.ResponseWriter, *http.Request) {}).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	lines := logLines(t, buf)
	if len(lines) != 2 || lines[0]["level"] != "ERROR" || lines[1]["level"] != "DEBUG" {
		t.Errorf("levels = %v", lines)
	}
}

func TestAccessLog_UnmatchedRouteHasFixedLabel(t *testing.T) {
	buf := captureLogs(t)
	observed(func(http.ResponseWriter, *http.Request) {}).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no/such/path/12345", nil))

	lines := logLines(t, buf)
	if len(lines) != 1 || lines[0]["route"] != "unmatched" || lines[0]["status"] != float64(http.StatusNotFound) {
		t.Errorf("record = %v", lines)
	}
}

func TestRecoverer_PanicBecomesJSON500WithRequestID(t *testing.T) {
	buf := captureLogs(t)
	rec := httptest.NewRecorder()
	observed(func(http.ResponseWriter, *http.Request) { panic("boom") }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/notes/1", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var body Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "internal" {
		t.Errorf("body = %q, want the API error format (err %v)", rec.Body.String(), err)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("the panic value must not reach the client")
	}
	id := rec.Header().Get("X-Request-ID")
	var panicLine, accessLine map[string]any
	for _, l := range logLines(t, buf) {
		switch l["msg"] {
		case "panic in handler":
			panicLine = l
		case "request":
			accessLine = l
		}
	}
	if panicLine == nil || panicLine["request_id"] != id || panicLine["stack"] == "" {
		t.Errorf("panic must be logged with the request id and a stack: %v", panicLine)
	}
	if accessLine == nil || accessLine["status"] != float64(500) {
		t.Errorf("the access log must record the 500 produced by the recoverer: %v", accessLine)
	}
}

func TestRecoverer_AbortHandlerIsRethrown(t *testing.T) {
	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler passed through", rec)
		}
	}()
	recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestErrorLogsInHandlersCarryTheRequestID(t *testing.T) {
	buf := captureLogs(t)
	h := NewHandler(Deps{DB: failingPinger{}, JWTSecret: []byte("test-secret-test-secret-test-secret")})
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("X-Request-ID", "req-health")
	h.ServeHTTP(httptest.NewRecorder(), req)

	for _, l := range logLines(t, buf) {
		if l["msg"] == "health check: database ping failed" {
			if l["request_id"] != "req-health" {
				t.Errorf("request_id = %v", l["request_id"])
			}
			return
		}
	}
	t.Errorf("health failure was not logged: %s", buf)
}

type failingPinger struct{}

func (failingPinger) Ping(context.Context) error { return errors.New("down") }

func TestLogHandler_KeepsRequestIDThroughWithAttrsAndGroups(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewLogHandler(slog.NewJSONHandler(&buf, nil))).With("component", "x").WithGroup("g")
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-42")

	logger.InfoContext(ctx, "hello", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	// request_id добавляется внутрь открытой группы, но он обязан быть в записи.
	group, _ := rec["g"].(map[string]any)
	if rec["component"] != "x" || group["k"] != "v" || (rec["request_id"] != "req-42" && group["request_id"] != "req-42") {
		t.Errorf("record = %v", rec)
	}
}

func TestLogHandler_NoRequestIDOutsideARequest(t *testing.T) {
	var buf bytes.Buffer
	slog.New(NewLogHandler(slog.NewJSONHandler(&buf, nil))).Info("startup")
	if strings.Contains(buf.String(), "request_id") {
		t.Errorf("a record made outside a request must not invent an id: %s", buf.String())
	}
}
