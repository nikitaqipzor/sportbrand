// Command api is the Athletica AI backend: health, authentication and the
// idempotent workout-set write.
//
// Usage:
//
//	api                 start the HTTP server (default)
//	api serve           same as above
//	api migrate up      apply every pending migration and exit
//	api migrate down    roll the newest migration back and exit
//	api prune-tokens    delete expired/revoked refresh tokens once and exit
//	api healthcheck     probe the local /health endpoint (used by Docker)
//	api version         print the build version
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"athletica.ai/api/internal/config"
	"athletica.ai/api/internal/httpapi"
	"athletica.ai/api/internal/metrics"
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
	case "prune-tokens":
		return pruneTokens(cfg, log)
	case "healthcheck":
		return healthcheck(cfg)
	default:
		return fmt.Errorf("unknown command %q (want serve, migrate, prune-tokens, healthcheck or version)", command)
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
	attachStoreMetrics(ctx, api.Metrics(), st, log)

	// Metrics live on their own listener. The public port has no /metrics route
	// at all, and config refuses a non-loopback bind without a bearer token.
	stopMetrics, err := serveMetrics(cfg, api, log)
	if err != nil {
		return err
	}
	defer stopMetrics()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Housekeeping: expired and revoked refresh rows are deleted in the
	// background. It stops with the server, and a failure never blocks serving.
	sweeperDone := make(chan struct{})
	go func() {
		defer close(sweeperDone)
		api.RunRefreshTokenSweeper(ctx)
	}()

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
	<-sweeperDone
	log.Info("shutdown complete")
	return nil
}

// pruneTokens runs one refresh-token sweep and exits, for deployments that
// prefer a cron job over the in-process sweeper.
func pruneTokens(cfg config.Config, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	st, err := openStore(ctx, cfg, log, false)
	if err != nil {
		return err
	}
	defer st.Close()

	api, err := httpapi.New(httpapi.Deps{Config: cfg, Store: st, Logger: log, Version: version})
	if err != nil {
		return err
	}
	deleted, err := api.PruneRefreshTokens(ctx)
	if err != nil {
		return err
	}
	log.Info("refresh-token sweep complete", "deleted", deleted, "retention", cfg.RefreshTokenRetention.String())
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

// serveMetrics starts the separate observability listener. It returns a stop
// function; when metrics are disabled both are no-ops.
func serveMetrics(cfg config.Config, api *httpapi.Server, log *slog.Logger) (func(), error) {
	if cfg.MetricsAddr == "" {
		log.Info("metrics listener disabled", "reason", "ATHLETICA_METRICS_ADDR is empty")
		return func() {}, nil
	}

	listener, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		return nil, fmt.Errorf("metrics listener on %s: %w", cfg.MetricsAddr, err)
	}

	srv := &http.Server{
		Handler:           api.MetricsHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	go func() {
		log.Info("metrics listening",
			"addr", cfg.MetricsAddr,
			"loopback_only", config.IsLoopbackAddr(cfg.MetricsAddr),
			"token_required", cfg.MetricsToken != "")
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Losing metrics must never take the API down with it.
			log.Error("metrics listener stopped", "error", err.Error())
		}
	}()

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}, nil
}

// attachStoreMetrics reports the database pool state and the migration queue.
// Neither carries anything about a person.
func attachStoreMetrics(ctx context.Context, registry *metrics.Registry, st store.Store, log *slog.Logger) {
	pg, ok := st.(*postgres.Store)
	if !ok {
		// The in-memory store has no pool and no schema; the gauges are then
		// simply absent rather than reported as a misleading zero.
		return
	}

	registry.SetPoolSource(func() metrics.PoolStats {
		stat := pg.Pool().Stat()
		return metrics.PoolStats{
			MaxConns:          stat.MaxConns(),
			TotalConns:        stat.TotalConns(),
			AcquiredConns:     stat.AcquiredConns(),
			IdleConns:         stat.IdleConns(),
			ConstructingConns: stat.ConstructingConns(),
			AcquireCount:      stat.AcquireCount(),
			EmptyAcquireCount: stat.EmptyAcquireCount(),
			CanceledAcquire:   stat.CanceledAcquireCount(),
		}
	})

	pendingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pending, err := postgres.PendingMigrations(pendingCtx, pg.Pool())
	if err != nil {
		log.Warn("could not measure the migration queue", "error", err.Error())
		return
	}
	registry.SetMigrationQueue(pending, migrationsAppliedAtStart)
	if pending > 0 {
		log.Warn("migrations are pending", "pending", pending)
	}
}

// migrationsAppliedAtStart is how many migrations this process ran during
// start-up; openStore records it so the metrics page can report both halves.
var migrationsAppliedAtStart int

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
		migrationsAppliedAtStart = n
		log.Info("migrations checked", "applied", n)
	}
	return pg, nil
}
