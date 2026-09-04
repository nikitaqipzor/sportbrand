// Command api is the Athletica AI backend: health, authentication and the
// idempotent workout-set write.
//
// Usage:
//
//	api                 start the HTTP server (default)
//	api serve           same as above
//	api migrate up      apply every pending migration and exit
//	api migrate down    roll the newest migration back and exit
//	api healthcheck     probe the local /health endpoint (used by Docker)
//	api version         print the build version
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"athletica.ai/api/internal/config"
	"athletica.ai/api/internal/httpapi"
	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/store/memory"
	"athletica.ai/api/internal/store/postgres"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		// Startup failures (an unsafe JWT secret, an unreachable database) must
		// be loud and must stop the process.
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	// Printing the build version must not depend on the environment: it is the
	// one command that answers before a database URL or a signing secret exists.
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With("service", "athletica-api", "env", string(cfg.Env), "version", version)
	slog.SetDefault(log)

	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "serve":
		return serve(cfg, log)
	case "migrate":
		return migrate(cfg, log, args[1:])
	case "healthcheck":
		return healthcheck(cfg)
	default:
		return fmt.Errorf("unknown command %q (want serve, migrate, healthcheck or version)", command)
	}
}

func serve(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, cfg, log, cfg.MigrateOnStart)
	if err != nil {
		return err
	}
	defer st.Close()

	api, err := httpapi.New(httpapi.Deps{Config: cfg, Store: st, Logger: log, Version: version})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.Addr, "base_path", cfg.BasePath, "store", cfg.Driver)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown signal received", "timeout", cfg.ShutdownTimeout.String())
	}

	// Graceful shutdown: stop accepting, let in-flight requests finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

func migrate(cfg config.Config, log *slog.Logger, args []string) error {
	if cfg.Driver != config.DriverPostgres {
		return fmt.Errorf("migrate: requires ATHLETICA_STORE_DRIVER=%s", config.DriverPostgres)
	}

	direction := "up"
	if len(args) > 0 {
		direction = args[0]
	}
	steps := 1
	if len(args) > 1 {
		parsed, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("migrate: steps must be an integer, got %q", args[1])
		}
		steps = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pg, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pg.Close()

	switch direction {
	case "up":
		n, err := postgres.MigrateUp(ctx, pg.Pool(), log)
		if err != nil {
			return err
		}
		log.Info("migrations up complete", "applied", n)
		return nil
	case "down":
		n, err := postgres.MigrateDown(ctx, pg.Pool(), steps, log)
		if err != nil {
			return err
		}
		log.Info("migrations down complete", "reverted", n)
		return nil
	default:
		return fmt.Errorf("migrate: direction must be up or down, got %q", direction)
	}
}

// healthcheck probes the running server from inside its own container, so the
// image needs no shell, curl or wget for Docker's HEALTHCHECK.
func healthcheck(cfg config.Config) error {
	addr := cfg.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	url := "http://" + addr + cfg.BasePath + "/health"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %d", url, resp.StatusCode)
	}
	return nil
}

func openStore(ctx context.Context, cfg config.Config, log *slog.Logger, runMigrations bool) (store.Store, error) {
	if cfg.Driver == config.DriverMemory {
		log.Warn("using the in-memory store: all data is lost on restart")
		return memory.New(), nil
	}

	pg, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if runMigrations {
		migrateCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		n, err := postgres.MigrateUp(migrateCtx, pg.Pool(), log)
		if err != nil {
			pg.Close()
			return nil, err
		}
		log.Info("migrations checked", "applied", n)
	}
	return pg, nil
}
