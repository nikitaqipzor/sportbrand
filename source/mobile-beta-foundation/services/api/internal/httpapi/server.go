// Package httpapi wires the net/http router, middleware and handlers.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"athletica.ai/api/internal/auth"
	"athletica.ai/api/internal/config"
	"athletica.ai/api/internal/ratelimit"
	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Config config.Config
	Store  store.Store
	Logger *slog.Logger
	// Now is injectable so tests can drive TTLs and backoff windows.
	Now func() time.Time
	// Version is reported by GET /health.
	Version string
}

// Server is the composed HTTP application.
type Server struct {
	cfg      config.Config
	log      *slog.Logger
	store    store.Store
	auth     *auth.Service
	workouts *workouts.Service
	now      func() time.Time
	version  string

	// ipLimiter throttles by source address, accountLimiter by account or
	// presented credential, so neither dimension alone can be abused.
	ipLimiter      *ratelimit.Limiter
	accountLimiter *ratelimit.Limiter

	handler http.Handler
}

// New builds the server and its dependency graph.
func New(deps Deps) (*Server, error) {
	if deps.Store == nil {
		return nil, errors.New("httpapi: store is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}

	hasher, err := auth.NewHasher(deps.Config.BcryptCost)
	if err != nil {
		return nil, err
	}
	issuer, err := auth.NewTokenIssuer(deps.Config.JWTSecret, deps.Config.JWTIssuer, deps.Now)
	if err != nil {
		return nil, err
	}

	limitCfg := ratelimit.Config{
		Limit:        deps.Config.AuthRateLimit,
		Window:       deps.Config.AuthRateWindow,
		FailureLimit: deps.Config.AuthFailureLimit,
		BackoffBase:  deps.Config.AuthBackoffBase,
		BackoffMax:   deps.Config.AuthBackoffMax,
	}

	s := &Server{
		cfg:            deps.Config,
		log:            deps.Logger,
		store:          deps.Store,
		auth:           auth.NewService(deps.Store, hasher, issuer, deps.Config.AccessTTL, deps.Config.RefreshTTL, deps.Now),
		workouts:       workouts.NewService(deps.Store, deps.Now),
		now:            deps.Now,
		version:        deps.Version,
		ipLimiter:      ratelimit.New(limitCfg, deps.Now),
		accountLimiter: ratelimit.New(limitCfg, deps.Now),
	}
	s.handler = s.routes()
	return s, nil
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	base := s.cfg.BasePath

	register := func(pattern string, handler http.Handler) {
		mux.Handle(pattern, handler)
	}

	// Health lives both under the versioned base path (for clients) and at the
	// root (for container health checks and load balancers).
	health := http.HandlerFunc(s.handleHealth)
	register("GET "+base+"/health", health)
	if base != "" {
		register("GET /health", health)
	}

	register("POST "+base+"/auth/register", http.HandlerFunc(s.handleRegister))
	register("POST "+base+"/auth/login", http.HandlerFunc(s.handleLogin))
	register("POST "+base+"/auth/refresh", http.HandlerFunc(s.handleRefresh))
	register("POST "+base+"/auth/logout", http.HandlerFunc(s.handleLogout))
	register("POST "+base+"/auth/logout-all", s.authenticated(s.handleLogoutAll))

	register("GET "+base+"/auth/me", s.authenticated(s.handleMe))
	register("POST "+base+"/workouts", s.authenticated(s.handleCreateWorkout))
	register("GET "+base+"/workouts", s.authenticated(s.handleListWorkouts))
	register("GET "+base+"/workouts/{workoutId}", s.authenticated(s.handleGetWorkout))
	register("POST "+base+"/workouts/{workoutId}/status", s.authenticated(s.handleWorkoutStatus))
	register("GET "+base+"/progress", s.authenticated(s.handleProgress))
	register("POST "+base+"/workouts/{workoutId}/sets", s.authenticated(s.handleLogSet))
	register("GET "+base+"/workouts/{workoutId}/sets", s.authenticated(s.handleListSets))

	mux.HandleFunc("/", s.handleNotFound)

	var handler http.Handler = mux
	handler = withLogging(s.log, s.now)(handler)
	handler = withRecovery(s.log)(handler)
	handler = withRequestID(handler)
	return handler
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, s.log, http.StatusNotFound, codeNotFound, "no route matches "+r.Method+" "+r.URL.Path)
}
