// Command fleetd is the fleet console server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/api"
	"github.com/voeunkh/gateway-fleet-frontend/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fleetd exited", "err", err)
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	if os.Getenv("FLEET_ENV") == "prod" {
		return slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func run() error {
	slog.SetDefault(newLogger())

	addr := os.Getenv("FLEET_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	static, err := web.Dist()
	if err != nil {
		return fmt.Errorf("open embedded web assets: %w", err)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewRouter(static),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}
