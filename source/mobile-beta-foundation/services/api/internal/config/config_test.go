package config_test

import (
	"strings"
	"testing"
	"time"

	"athletica.ai/api/internal/config"
)

func baseEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"ATHLETICA_DATABASE_URL": "postgres://athletica:local@localhost:5432/athletica",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// Audit finding H5: production must refuse to boot without a real secret.
func TestProductionRefusesUnsafeSecrets(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		want   string
	}{
		{"empty", "", "is empty"},
		{"whitespace only", "    ", "is empty"},
		{"dev placeholder", config.DevJWTSecret, "development placeholder"},
		{"dev placeholder cased", "Dev-Only-Change-Me", "development placeholder"},
		{"other placeholder", "change-me", "development placeholder"},
		{"too short", "0123456789abcdef", "characters while ATHLETICA_ENV"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv(map[string]string{
				"ATHLETICA_ENV":        "production",
				"ATHLETICA_JWT_SECRET": tc.secret,
			})
			_, err := config.Load(config.MapLookup(env))
			if err == nil {
				t.Fatalf("expected production start-up to fail for secret %q", tc.secret)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not explain the problem (want substring %q)", err, tc.want)
			}
			if !strings.Contains(err.Error(), "ATHLETICA_JWT_SECRET") {
				t.Fatalf("error %q should name the offending variable", err)
			}
		})
	}
}

func TestProductionRejectsDevSecretViaLegacyAlias(t *testing.T) {
	env := baseEnv(map[string]string{
		"ATHLETICA_ENV":     "production",
		"AUTH_TOKEN_SECRET": config.DevJWTSecret,
	})
	if _, err := config.Load(config.MapLookup(env)); err == nil {
		t.Fatal("AUTH_TOKEN_SECRET=dev-only-change-me must also be fatal in production")
	}
}

func TestStagingIsHardenedToo(t *testing.T) {
	env := baseEnv(map[string]string{"ATHLETICA_ENV": "staging", "ATHLETICA_JWT_SECRET": config.DevJWTSecret})
	if _, err := config.Load(config.MapLookup(env)); err == nil {
		t.Fatal("staging must refuse the development placeholder secret")
	}
}

func TestProductionAcceptsStrongSecret(t *testing.T) {
	secret := strings.Repeat("s3cr3t-", 8)
	env := baseEnv(map[string]string{"ATHLETICA_ENV": "production", "ATHLETICA_JWT_SECRET": secret})

	cfg, err := config.Load(config.MapLookup(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JWTSecret != secret {
		t.Fatalf("secret = %q, want %q", cfg.JWTSecret, secret)
	}
	if cfg.Env != config.EnvProduction {
		t.Fatalf("env = %q", cfg.Env)
	}
}

func TestProductionRejectsMemoryStore(t *testing.T) {
	env := baseEnv(map[string]string{
		"ATHLETICA_ENV":          "production",
		"ATHLETICA_JWT_SECRET":   strings.Repeat("k", 40),
		"ATHLETICA_STORE_DRIVER": "memory",
	})
	if _, err := config.Load(config.MapLookup(env)); err == nil {
		t.Fatal("the in-memory store must not be usable in production")
	}
}

func TestDevelopmentDefaults(t *testing.T) {
	cfg, err := config.Load(config.MapLookup(baseEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != config.EnvDevelopment {
		t.Fatalf("env = %q, want development", cfg.Env)
	}
	if cfg.JWTSecret != config.DevJWTSecret {
		t.Fatalf("development should fall back to the placeholder secret, got %q", cfg.JWTSecret)
	}
	if cfg.BasePath != "/api/v1" {
		t.Fatalf("base path = %q, want /api/v1", cfg.BasePath)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("addr = %q, want :8080", cfg.Addr)
	}
	if cfg.AccessTTL != 15*time.Minute {
		t.Fatalf("access ttl = %s, want 15m", cfg.AccessTTL)
	}
}

func TestPostgresDriverRequiresDatabaseURL(t *testing.T) {
	if _, err := config.Load(config.MapLookup(map[string]string{})); err == nil {
		t.Fatal("expected an error when ATHLETICA_DATABASE_URL is missing")
	}
}

func TestRejectsInvalidValues(t *testing.T) {
	cases := map[string]map[string]string{
		"env":       {"ATHLETICA_ENV": "prod"},
		"log level": {"ATHLETICA_LOG_LEVEL": "verbose"},
		"driver":    {"ATHLETICA_STORE_DRIVER": "sqlite"},
		"duration":  {"ATHLETICA_ACCESS_TOKEN_TTL": "15 minutes"},
		"long ttl":  {"ATHLETICA_ACCESS_TOKEN_TTL": "24h"},
		"boolean":   {"ATHLETICA_MIGRATE_ON_START": "yes-please"},
		"cost":      {"ATHLETICA_BCRYPT_COST": "99"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(config.MapLookup(baseEnv(extra))); err == nil {
				t.Fatalf("expected a configuration error for %v", extra)
			}
		})
	}
}

func TestBasePathIsNormalized(t *testing.T) {
	cfg, err := config.Load(config.MapLookup(baseEnv(map[string]string{"ATHLETICA_BASE_PATH": "api/v2/"})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BasePath != "/api/v2" {
		t.Fatalf("base path = %q, want /api/v2", cfg.BasePath)
	}
}

// The metrics listener must never be reachable from outside the host without a
// token. Binding it wide open is a start-up failure, not a warning.
func TestMetricsListenerRefusesAnonymousExposure(t *testing.T) {
	cases := map[string]string{
		"all interfaces": "0.0.0.0:9091",
		"bare port":      ":9091",
		"a routed ip":    "10.1.2.3:9091",
	}
	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(config.MapLookup(baseEnv(map[string]string{
				"ATHLETICA_METRICS_ADDR": addr,
			})))
			if err == nil {
				t.Fatalf("%s with no token was accepted", addr)
			}
			if !strings.Contains(err.Error(), "ATHLETICA_METRICS_TOKEN") {
				t.Fatalf("error does not name the fix: %v", err)
			}
		})
	}

	// The same address with a token is fine.
	if _, err := config.Load(config.MapLookup(baseEnv(map[string]string{
		"ATHLETICA_METRICS_ADDR":  "0.0.0.0:9091",
		"ATHLETICA_METRICS_TOKEN": "a-real-scrape-token",
	}))); err != nil {
		t.Fatalf("a token should permit a wider bind: %v", err)
	}

	// And so is loopback with none.
	cfg, err := config.Load(config.MapLookup(baseEnv(nil)))
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if cfg.MetricsAddr != "127.0.0.1:9091" || !config.IsLoopbackAddr(cfg.MetricsAddr) {
		t.Fatalf("metrics default = %q, want a loopback address", cfg.MetricsAddr)
	}

	// Metrics never share the public listener.
	if _, err := config.Load(config.MapLookup(baseEnv(map[string]string{
		"ATHLETICA_HTTP_ADDR":     "127.0.0.1:8080",
		"ATHLETICA_METRICS_ADDR":  "127.0.0.1:8080",
		"ATHLETICA_METRICS_TOKEN": "a-real-scrape-token",
	}))); err == nil {
		t.Fatal("metrics on the public listener address were accepted")
	}

	// An empty value disables the listener entirely.
	cfg, err = config.Load(config.MapLookup(baseEnv(map[string]string{"ATHLETICA_METRICS_ADDR": ""})))
	if err != nil || cfg.MetricsAddr != "" {
		t.Fatalf("disabling metrics = (%q, %v)", cfg.MetricsAddr, err)
	}
}
