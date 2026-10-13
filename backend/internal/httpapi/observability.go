package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const requestIDHeader = "X-Request-ID"

type requestIDKey struct{}

// validRequestID: id от клиента принимается, только если он короткий и состоит из безопасных символов.
// Он попадает в логи и заголовок ответа, поэтому произвольной строке там не место.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID возвращает id запроса из контекста (пусто вне запроса).
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestID кладёт id в контекст и в заголовок ответа: по нему ответ клиенту сопоставляется со строками лога.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand не возвращает ошибку на поддерживаемых платформах
	return hex.EncodeToString(b)
}

// accessLog пишет по одной строке на запрос. В строке нет query string и заголовков: в них бывают поисковые
// запросы и токены. Путь логируется шаблоном маршрута, поэтому id заметок не плодят уникальные значения.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case r.URL.Path == "/health" || r.URL.Path == apiPrefix+"/health":
			level = slog.LevelDebug // проверки живости раз в несколько секунд не должны забивать лог
		}
		slog.LogAttrs(r.Context(), level, "request",
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
	})
}

// recoverer превращает панику обработчика в обычный ответ 500 в формате API и пишет стек в лог.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			slog.ErrorContext(r.Context(), "panic in handler", "panic", rec, "stack", string(debug.Stack()))
			internalError(w)
		}()
		next.ServeHTTP(w, r)
	})
}

// contextHandler добавляет id запроса в каждую запись, сделанную с контекстом запроса.
type contextHandler struct{ slog.Handler }

// NewLogHandler оборачивает обработчик slog так, что записи с контекстом запроса получают request_id.
func NewLogHandler(inner slog.Handler) slog.Handler { return contextHandler{inner} }

func (h contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestID(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
