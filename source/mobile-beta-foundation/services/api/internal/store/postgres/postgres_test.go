package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/store/postgres"
	"athletica.ai/api/internal/store/storetest"
	"athletica.ai/api/migrations"
)

// TestEmbeddedMigrationsArePaired needs no database: it guards the invariant
// that every migration ships both halves and that versions are ordered.
func TestEmbeddedMigrationsArePaired(t *testing.T) {
	loaded, err := postgres.LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no migrations were embedded")
	}
	for i, m := range loaded {
		if strings.TrimSpace(m.Up) == "" || strings.TrimSpace(m.Down) == "" {
			t.Fatalf("migration %04d_%s is missing a half", m.Version, m.Name)
		}
		if i > 0 && loaded[i-1].Version >= m.Version {
			t.Fatalf("migrations are not strictly ordered: %d then %d", loaded[i-1].Version, m.Version)
		}
	}
	// The idempotency guarantee must live in SQL, not only in Go.
	if !strings.Contains(loaded[0].Up, "workout_sets_user_mutation_key") {
		t.Fatal("the unique index on (user_id, client_mutation_id) is missing from the schema")
	}
}

// TestPostgresStoreConformance runs the shared suite against a real database.
// Set ATHLETICA_TEST_DATABASE_URL to enable it, e.g.
//
//	ATHLETICA_TEST_DATABASE_URL=postgres://athletica:local-development-only@localhost:5432/athletica go test ./...
func TestPostgresStoreConformance(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("ATHLETICA_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("ATHLETICA_TEST_DATABASE_URL is not set; skipping the PostgreSQL conformance suite")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pg, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pg.Close)

	if _, err := postgres.MigrateUp(ctx, pg.Pool(), nil); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	storetest.Run(t, func(t *testing.T) store.Store {
		truncate(t, pg)
		return pg
	})
}

func truncate(t *testing.T, pg *postgres.Store) {
	t.Helper()
	// The catalogue tables are truncated too: they hold no user data, but a
	// conformance subtest must start from an empty reference book.
	const q = `TRUNCATE exercise_import, exercise_code_link, exercise, exercise_code,
	                    client_mutations, workout_sets, workouts, refresh_tokens, users
	           RESTART IDENTITY CASCADE`
	if _, err := pg.Pool().Exec(context.Background(), q); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
