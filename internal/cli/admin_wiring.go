package cli

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/admin/api"
)

// runAdminEndpoint starts the v2.2 admin unix-socket server. v2.2-A1
// scaffolds only the health endpoint; A2 expands to the full CLI
// AppService surface.
//
// Returns a cleanup function that shuts the server down + removes the
// socket file. cleanup is non-nil even on error so the caller can
// always defer it safely.
func runAdminEndpoint(ctx context.Context, socketPath string, logger func(string)) (cleanup func() error, err error) {
	if socketPath == "" {
		return func() error { return nil }, errors.New("admin: socket_path required")
	}
	srv := api.NewServer(socketPath)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger("admin: " + err.Error())
		}
	}()
	cleanup = func() error {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
	return cleanup, nil
}
