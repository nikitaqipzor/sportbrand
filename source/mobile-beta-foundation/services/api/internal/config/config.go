// Package config loads the API service configuration from environment
// variables and refuses to hand back a configuration that is unsafe to run.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment is the deployment tier the service believes it is running in.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// DevJWTSecret is the placeholder secret used by local development and the
// compose stack. Audit finding H5: booting production with it must be fatal.
const DevJWTSecret = "dev-only-change-me"

// MinProductionSecretLen is the shortest HMAC key we accept outside development.
const MinProductionSecretLen = 32

// Driver selects the Store implementation.
const (
	DriverPostgres = "postgres"
	DriverMemory   = "memory"
)

// bannedSecrets are well-known placeholder values that must never reach a
// production deployment, regardless of length.
var bannedSecrets = map[string]struct{}{
	"dev-only-change-me": {},
	"change-me":          {},
	"changeme":           {},
	"change_me":          {},
	"dev":                {},
	"dev-secret":         {},
	"development":        {},
	"insecure":           {},
	"local":              {},
	"placeholder":        {},
	"secret":             {},
	"test":               {},
	"testing":            {},
}

// Config is the fully resolved, validated service configuration.
type Config struct {
	Env      Environment
	Addr     string
	BasePath string
	LogLevel slog.Level

	Driver         string
	DatabaseURL    string
	MigrateOnStart bool

	JWTSecret  string
	JWTIssuer  string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	BcryptCost int

	// Auth throttling (audit finding H4).
	AuthRateLimit    int
	AuthRateWindow   time.Duration
	AuthFailureLimit int
	AuthBackoffBase  time.Duration
	AuthBackoffMax   time.Duration

	// RefreshTokenSweepInterval is how often expired and revoked refresh rows
	// are deleted in the background; zero disables the sweep.
	RefreshTokenSweepInterval time.Duration
	// RefreshTokenRetention keeps a spent row around for this long before the
	// sweep may delete it, so a support question can still be answered.
	RefreshTokenRetention time.Duration

	TrustProxyHeaders bool
	ShutdownTimeout   time.Duration

	// MetricsAddr is the *separate* listener the Prometheus page is served on;
	// empty disables it entirely. It is never part of the public router: the
	// public port answers /metrics with the same 404 as any unknown path.
	MetricsAddr string
	// MetricsToken, when set, is the bearer token /metrics demands. Binding the
	// metrics listener anywhere but loopback without one is refused at start-up.
	MetricsToken string
}

// IsLoopbackAddr reports whether addr binds only to the loopback interface.
// A bare ":9091" or "0.0.0.0:9091" is not loopback: it is reachable from
// outside the machine and therefore needs a token.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Lookup mirrors os.LookupEnv so tests can inject an environment.
type Lookup func(string) (string, bool)

// MapLookup adapts a map to Lookup, for tests.
func MapLookup(env map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

// FromEnv loads the configuration from the process environment.
func FromEnv() (Config, error) { return Load(os.LookupEnv) }

// Load resolves and validates the configuration from the given lookup.
func Load(look Lookup) (Config, error) {
	if look == nil {
		look = func(string) (string, bool) { return "", false }
	}

	env, err := parseEnvironment(str(look, "ATHLETICA_ENV", string(EnvDevelopment)))
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:      env,
		Addr:     str(look, "ATHLETICA_HTTP_ADDR", ":8080"),
		BasePath: normalizeBasePath(str(look, "ATHLETICA_BASE_PATH", "/api/v1")),
		Driver:   strings.ToLower(str(look, "ATHLETICA_STORE_DRIVER", DriverPostgres)),
		// AUTH_TOKEN_SECRET is the name used by the audited service; keep it as
		// an alias so an existing deployment cannot silently lose its secret.
		JWTSecret: firstNonEmpty(str(look, "ATHLETICA_JWT_SECRET", ""), str(look, "AUTH_TOKEN_SECRET", "")),
		JWTIssuer: str(look, "ATHLETICA_JWT_ISSUER", "athletica-api"),
	}
	cfg.DatabaseURL = firstNonEmpty(str(look, "ATHLETICA_DATABASE_URL", ""), str(look, "DATABASE_URL", ""))

	if cfg.LogLevel, err = parseLogLevel(str(look, "ATHLETICA_LOG_LEVEL", "info")); err != nil {
		return Config{}, err
	}
	if cfg.MigrateOnStart, err = boolean(look, "ATHLETICA_MIGRATE_ON_START", true); err != nil {
		return Config{}, err
	}
	if cfg.TrustProxyHeaders, err = boolean(look, "ATHLETICA_TRUST_PROXY_HEADERS", false); err != nil {
		return Config{}, err
	}
	if cfg.AccessTTL, err = duration(look, "ATHLETICA_ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTTL, err = duration(look, "ATHLETICA_REFRESH_TOKEN_TTL", 30*24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration(look, "ATHLETICA_SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.BcryptCost, err = integer(look, "ATHLETICA_BCRYPT_COST", 12); err != nil {
		return Config{}, err
	}
	if cfg.AuthRateLimit, err = integer(look, "ATHLETICA_AUTH_RATE_LIMIT", 10); err != nil {
		return Config{}, err
	}
	if cfg.AuthRateWindow, err = duration(look, "ATHLETICA_AUTH_RATE_WINDOW", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.AuthFailureLimit, err = integer(look, "ATHLETICA_AUTH_FAILURE_LIMIT", 5); err != nil {
		return Config{}, err
	}
	if cfg.AuthBackoffBase, err = duration(look, "ATHLETICA_AUTH_BACKOFF_BASE", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.AuthBackoffMax, err = duration(look, "ATHLETICA_AUTH_BACKOFF_MAX", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenSweepInterval, err = duration(look, "ATHLETICA_REFRESH_TOKEN_SWEEP_INTERVAL", time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenRetention, err = duration(look, "ATHLETICA_REFRESH_TOKEN_RETENTION", 24*time.Hour); err != nil {
		return Config{}, err
	}
	// Loopback by default: the metrics page carries operational data and must
	// not be reachable from outside the host unless somebody says so — and
	// then only with a token.
	cfg.MetricsAddr = strings.TrimSpace(str(look, "ATHLETICA_METRICS_ADDR", "127.0.0.1:9091"))
	if raw, ok := look("ATHLETICA_METRICS_ADDR"); ok && strings.TrimSpace(raw) == "" {
		cfg.MetricsAddr = ""
	}
	cfg.MetricsToken = strings.TrimSpace(str(look, "ATHLETICA_METRICS_TOKEN", ""))

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.Driver {
	case DriverPostgres, DriverMemory:
	default:
		return fmt.Errorf("config: ATHLETICA_STORE_DRIVER must be %q or %q, got %q", DriverPostgres, DriverMemory, c.Driver)
	}
	if c.Driver == DriverPostgres && strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("config: ATHLETICA_DATABASE_URL is required when ATHLETICA_STORE_DRIVER=%s", DriverPostgres)
	}
	if c.Driver == DriverMemory && c.Env == EnvProduction {
		return fmt.Errorf("config: ATHLETICA_STORE_DRIVER=%s loses every write on restart and must never run in production", DriverMemory)
	}
	if c.AccessTTL <= 0 || c.RefreshTTL <= 0 {
		return fmt.Errorf("config: token TTLs must be positive (access=%s refresh=%s)", c.AccessTTL, c.RefreshTTL)
	}
	if c.AccessTTL > time.Hour {
		return fmt.Errorf("config: ATHLETICA_ACCESS_TOKEN_TTL must stay short-lived (<= 1h), got %s", c.AccessTTL)
	}
	if c.BcryptCost < 4 || c.BcryptCost > 31 {
		return fmt.Errorf("config: ATHLETICA_BCRYPT_COST must be between 4 and 31, got %d", c.BcryptCost)
	}
	if c.AuthRateLimit < 1 || c.AuthRateWindow <= 0 {
		return fmt.Errorf("config: auth rate limit must allow at least one request per positive window")
	}
	if c.RefreshTokenSweepInterval < 0 {
		return fmt.Errorf("config: ATHLETICA_REFRESH_TOKEN_SWEEP_INTERVAL must not be negative (0 disables the sweep), got %s", c.RefreshTokenSweepInterval)
	}
	if c.RefreshTokenSweepInterval > 0 && c.RefreshTokenSweepInterval < time.Minute {
		return fmt.Errorf("config: ATHLETICA_REFRESH_TOKEN_SWEEP_INTERVAL must be at least 1m, got %s", c.RefreshTokenSweepInterval)
	}
	if c.RefreshTokenRetention < 0 {
		return fmt.Errorf("config: ATHLETICA_REFRESH_TOKEN_RETENTION must not be negative, got %s", c.RefreshTokenRetention)
	}
	if c.AuthFailureLimit < 1 {
		return fmt.Errorf("config: ATHLETICA_AUTH_FAILURE_LIMIT must be >= 1, got %d", c.AuthFailureLimit)
	}
	if c.MetricsAddr != "" {
		if _, _, err := net.SplitHostPort(c.MetricsAddr); err != nil {
			return fmt.Errorf("config: ATHLETICA_METRICS_ADDR must be host:port, got %q", c.MetricsAddr)
		}
		if c.MetricsToken == "" && !IsLoopbackAddr(c.MetricsAddr) {
			return fmt.Errorf(
				"config: ATHLETICA_METRICS_ADDR=%q is reachable from outside this host while "+
					"ATHLETICA_METRICS_TOKEN is empty; refusing to serve metrics anonymously — "+
					"either bind 127.0.0.1 or set a token", c.MetricsAddr)
		}
		if c.MetricsAddr == c.Addr {
			return fmt.Errorf(
				"config: ATHLETICA_METRICS_ADDR must not equal ATHLETICA_HTTP_ADDR (%q); "+
					"metrics never share the public listener", c.Addr)
		}
	}
	return c.validateSecret()
}

// validateSecret implements audit finding H5. Outside development a missing,
// placeholder or too-short signing key is a hard start-up failure.
func (c *Config) validateSecret() error {
	secret := strings.TrimSpace(c.JWTSecret)
	hardened := c.Env == EnvProduction || c.Env == EnvStaging

	if !hardened {
		if secret == "" {
			c.JWTSecret = DevJWTSecret
		}
		return nil
	}

	switch {
	case secret == "":
		return fmt.Errorf(
			"config: ATHLETICA_JWT_SECRET (or AUTH_TOKEN_SECRET) is empty while ATHLETICA_ENV=%s; "+
				"refusing to start — generate a unique secret of at least %d characters, e.g. `openssl rand -base64 48`",
			c.Env, MinProductionSecretLen)
	case isBannedSecret(secret):
		return fmt.Errorf(
			"config: ATHLETICA_JWT_SECRET is the development placeholder %q while ATHLETICA_ENV=%s; "+
				"refusing to start — every non-development deployment needs its own secret of at least %d characters",
			secret, c.Env, MinProductionSecretLen)
	case len(secret) < MinProductionSecretLen:
		return fmt.Errorf(
			"config: ATHLETICA_JWT_SECRET is %d characters while ATHLETICA_ENV=%s requires at least %d; refusing to start",
			len(secret), c.Env, MinProductionSecretLen)
	}
	c.JWTSecret = secret
	return nil
}

func isBannedSecret(secret string) bool {
	_, banned := bannedSecrets[strings.ToLower(strings.TrimSpace(secret))]
	return banned
}

func parseEnvironment(raw string) (Environment, error) {
	switch Environment(strings.ToLower(strings.TrimSpace(raw))) {
	case EnvDevelopment, "":
		return EnvDevelopment, nil
	case EnvStaging:
		return EnvStaging, nil
	case EnvProduction:
		return EnvProduction, nil
	default:
		return "", fmt.Errorf("config: ATHLETICA_ENV must be development, staging or production, got %q", raw)
	}
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: ATHLETICA_LOG_LEVEL must be debug, info, warn or error, got %q", raw)
	}
}

func normalizeBasePath(raw string) string {
	p := strings.TrimSpace(raw)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func str(look Lookup, key, fallback string) string {
	if v, ok := look(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func boolean(look Lookup, key string, fallback bool) (bool, error) {
	raw, ok := look(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("config: %s must be a boolean, got %q", key, raw)
	}
	return v, nil
}

func integer(look Lookup, key string, fallback int) (int, error) {
	raw, ok := look(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer, got %q", key, raw)
	}
	return v, nil
}

func duration(look Lookup, key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := look(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a Go duration such as 15m, got %q", key, raw)
	}
	return v, nil
}
