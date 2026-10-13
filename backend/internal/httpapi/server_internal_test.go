package httpapi

import (
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/config"
)

// Без этих таймаутов медленный клиент удерживает соединение сколько угодно (slowloris).
func TestNewServer_SetsAddressAndEveryTimeout(t *testing.T) {
	srv := NewServer(config.Config{HTTPPort: "9191"}, Deps{})

	if srv.Addr != ":9191" {
		t.Errorf("Addr = %q", srv.Addr)
	}
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Errorf("timeouts must all be set: %v %v %v %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.Handler == nil {
		t.Error("Handler is nil")
	}
}
