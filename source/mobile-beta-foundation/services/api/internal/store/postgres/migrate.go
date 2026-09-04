package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"athletica.ai/api/migrations"
)

// advisoryLockKey serialises migrations across replicas starting at once.
const advisoryLockKey int64 = 8_242_025_001

// Migration is one up/down pair discovered in the embedded migrations FS.
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

// LoadMigrations parses NNNN_name.{up,down}.sql pairs from fsys.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("migrate: glob: %w", err)
	}

	byVersion := map[int]*Migration{}
	for _, name := range entries {
		base := path.Base(name)
		var direction string
		switch {
		case strings.HasSuffix(base, ".up.sql"):
			direction = "up"
		case strings.HasSuffix(base, ".down.sql"):
			direction = "down"
		default:
			return nil, fmt.Errorf("migrate: %q must end with .up.sql or .down.sql", base)
		}

		stem := strings.TrimSuffix(strings.TrimSuffix(base, ".up.sql"), ".down.sql")
		prefix, label, found := strings.Cut(stem, "_")
		if !found {
			return nil, fmt.Errorf("migrate: %q must be named NNNN_name.%s.sql", base, direction)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migrate: %q has a non-numeric version prefix: %w", base, err)
		}

		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("migrate: read %q: %w", base, err)
		}

		m, ok := byVersion[version]
		if !ok {
			m = &Migration{Version: version, Name: label}
			byVersion[version] = m
		}
		if direction == "up" {
			m.Up = string(body)
		} else {
			m.Down = string(body)
		}
	}

	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if strings.TrimSpace(m.Up) == "" || strings.TrimSpace(m.Down) == "" {
			return nil, fmt.Errorf("migrate: version %04d is missing its up or down half", m.Version)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// EmbeddedMigrations returns the migrations shipped inside the binary.
func EmbeddedMigrations() ([]Migration, error) { return LoadMigrations(migrations.FS) }

// MigrateUp applies every pending migration in order and returns how many ran.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) (int, error) {
	all, err := EmbeddedMigrations()
	if err != nil {
		return 0, err
	}

	conn, release, err := lockedConn(ctx, pool)
	if err != nil {
		return 0, err
	}
	defer release()

	if err := ensureSchemaTable(ctx, conn); err != nil {
		return 0, err
	}
	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, m := range all {
		if applied[m.Version] {
			continue
		}
		if err := runOne(ctx, conn, m, true, log); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// MigrateDown rolls back the newest `steps` applied migrations (0 means one).
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, steps int, log *slog.Logger) (int, error) {
	if steps <= 0 {
		steps = 1
	}
	all, err := EmbeddedMigrations()
	if err != nil {
		return 0, err
	}

	conn, release, err := lockedConn(ctx, pool)
	if err != nil {
		return 0, err
	}
	defer release()

	if err := ensureSchemaTable(ctx, conn); err != nil {
		return 0, err
	}
	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return 0, err
	}

	count := 0
	for i := len(all) - 1; i >= 0 && count < steps; i-- {
		m := all[i]
		if !applied[m.Version] {
			continue
		}
		if err := runOne(ctx, conn, m, false, log); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func runOne(ctx context.Context, conn *pgxpool.Conn, m Migration, up bool, log *slog.Logger) error {
	direction := "down"
	body := m.Down
	if up {
		direction = "up"
		body = m.Up
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrate: begin %04d %s: %w", m.Version, direction, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("migrate: apply %04d_%s.%s.sql: %w", m.Version, m.Name, direction, err)
	}
	if up {
		_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, m.Version)
	}
	if err != nil {
		return fmt.Errorf("migrate: record %04d %s: %w", m.Version, direction, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate: commit %04d %s: %w", m.Version, direction, err)
	}
	if log != nil {
		// Not "version": the root logger already carries the build version.
		log.Info("migration applied", "migration", m.Version, "name", m.Name, "direction", direction)
	}
	return nil
}

func lockedConn(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: acquire connection: %w", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf("migrate: advisory lock: %w", err)
	}
	release := func() {
		unlockCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
		conn.Release()
	}
	return conn, release, nil
}

func ensureSchemaTable(ctx context.Context, conn *pgxpool.Conn) error {
	const q = `CREATE TABLE IF NOT EXISTS schema_migrations (
	    version    bigint      PRIMARY KEY,
	    name       text        NOT NULL,
	    applied_at timestamptz NOT NULL DEFAULT now()
	)`
	if _, err := conn.Exec(ctx, q); err != nil {
		return fmt.Errorf("migrate: ensure schema_migrations: %w", err)
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[int]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[int]bool{}, nil
		}
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("migrate: scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}
