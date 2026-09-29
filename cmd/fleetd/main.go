// Command fleetd is the fleet console server.
//
//	fleetd [serve]   run the HTTP server (migrates first)
//	fleetd migrate   apply database migrations
//	fleetd seed      load the sample fleet into an empty database
//	fleetd user add --role admin|release|viewer USERNAME
//	                 create a user; the password is read from the first line of stdin
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/api"
	"github.com/voeunkh/gateway-fleet-frontend/internal/auth"
	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/seed"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
	"github.com/voeunkh/gateway-fleet-frontend/web"
)

func main() {
	slog.SetDefault(newLogger())
	if err := run(os.Args[1:]); err != nil {
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

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbPath := env("FLEET_DB", "fleet.db")
	switch cmd {
	case "serve", "migrate", "seed", "user":
	default:
		return fmt.Errorf("unknown command %q (want serve, migrate, seed or user)", cmd)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	n, err := st.Migrate(ctx)
	if err != nil {
		return err
	}
	slog.Info("database ready", "path", dbPath, "migrations_applied", n)

	switch cmd {
	case "migrate":
		return nil
	case "user":
		return userCmd(ctx, st, args[1:], os.Stdin)
	case "seed":
		now := time.Now()
		if err := st.Seed(ctx, seed.Generate(now), now); err != nil {
			return err
		}
		devices, err := st.CountDevices(ctx)
		if err != nil {
			return err
		}
		slog.Info("seeded sample fleet", "devices", devices)
		return nil
	}
	return serve(ctx, st)
}

func serve(ctx context.Context, st *store.Store) error {
	addr := env("FLEET_HTTP_ADDR", ":8080")
	static, err := web.Dist()
	if err != nil {
		return fmt.Errorf("open embedded web assets: %w", err)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewRouter(api.Config{Store: st, Static: static, SecureCookie: os.Getenv("FLEET_ENV") == "prod"}),
		ReadHeaderTimeout: 10 * time.Second,
	}
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

// userCmd implements "fleetd user add --role ROLE USERNAME".
func userCmd(ctx context.Context, st *store.Store, args []string, stdin *os.File) error {
	usage := errors.New("usage: fleetd user add --role admin|release|viewer USERNAME (password on stdin)")
	if len(args) == 0 || args[0] != "add" {
		return usage
	}
	fl := flag.NewFlagSet("user add", flag.ContinueOnError)
	role := fl.String("role", "", "admin, release or viewer")
	if err := fl.Parse(args[1:]); err != nil || fl.NArg() != 1 {
		return usage
	}
	r := domain.Role(*role)
	if !r.Valid() {
		return fmt.Errorf("unknown role %q: %w", *role, usage)
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("read password from stdin: %w", err)
	}
	hash, err := auth.HashPassword(strings.TrimRight(line, "\r\n"))
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, fl.Arg(0), hash, r, time.Now()); err != nil {
		return err
	}
	slog.Info("user created", "username", fl.Arg(0), "role", r)
	return nil
}
