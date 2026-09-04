package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"athletica.ai/api/internal/config"
	"athletica.ai/api/internal/httpapi"
	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/store/memory"
)

const basePath = "/api/v1"

// testClock is a controllable clock so throttling windows and token TTLs are
// deterministic instead of wall-clock dependent.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *testClock {
	return &testClock{t: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type harness struct {
	t     *testing.T
	api   *httpapi.Server
	store store.Store
	mem   *memory.Store
	clock *testClock
	cfg   config.Config
}

// testEnv is a realistic development environment; tests tweak single knobs.
func testEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"ATHLETICA_ENV":             "development",
		"ATHLETICA_STORE_DRIVER":    "memory",
		"ATHLETICA_JWT_SECRET":      "test-secret-long-enough-for-the-http-tests",
		"ATHLETICA_BCRYPT_COST":     "4", // keep the suite fast
		"ATHLETICA_AUTH_RATE_LIMIT": "1000",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func newHarness(t *testing.T, extra map[string]string) *harness {
	t.Helper()
	return newHarnessWithStore(t, nil, extra)
}

// newHarnessWithStore allows injecting a wrapper around the in-memory store,
// e.g. one whose Ping fails.
func newHarnessWithStore(t *testing.T, wrap func(*memory.Store) store.Store, extra map[string]string) *harness {
	t.Helper()

	cfg, err := config.Load(config.MapLookup(testEnv(extra)))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	mem := memory.New()
	var st store.Store = mem
	if wrap != nil {
		st = wrap(mem)
	}

	clock := newClock()
	mem.SetClock(clock.Now)

	api, err := httpapi.New(httpapi.Deps{
		Config:  cfg,
		Store:   st,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:     clock.Now,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	return &harness{t: t, api: api, store: st, mem: mem, clock: clock, cfg: cfg}
}

type request struct {
	method  string
	path    string
	body    any
	token   string
	ip      string
	headers map[string]string
}

type response struct {
	status int
	body   []byte
	header http.Header
}

// json decodes the response body into a generic map.
func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	if len(r.body) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode response %q: %v", r.body, err)
	}
	return out
}

func (r response) str(t *testing.T, path ...string) string {
	t.Helper()
	var current any = r.json(t)
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("response %q has no object at %v", r.body, path)
		}
		current = m[key]
	}
	s, ok := current.(string)
	if !ok {
		t.Fatalf("response %q has no string at %v", r.body, path)
	}
	return s
}

func (h *harness) send(req request) response {
	h.t.Helper()

	var body io.Reader
	if req.body != nil {
		switch v := req.body.(type) {
		case string:
			body = strings.NewReader(v)
		default:
			raw, err := json.Marshal(v)
			if err != nil {
				h.t.Fatalf("encode request: %v", err)
			}
			body = bytes.NewReader(raw)
		}
	}

	r := httptest.NewRequest(req.method, req.path, body)
	r.Header.Set("Content-Type", "application/json")
	if req.token != "" {
		r.Header.Set("Authorization", "Bearer "+req.token)
	}
	if req.ip != "" {
		r.RemoteAddr = req.ip + ":40000"
	}
	for k, v := range req.headers {
		r.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.api.ServeHTTP(rec, r)
	return response{status: rec.Code, body: rec.Body.Bytes(), header: rec.Header().Clone()}
}

// account is a registered user plus its live credentials.
type account struct {
	id           string
	email        string
	password     string
	accessToken  string
	refreshToken string
}

func (h *harness) register(email, password string) account {
	h.t.Helper()

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/register", body: map[string]any{
		"email": email, "password": password,
	}})
	if res.status != http.StatusCreated {
		h.t.Fatalf("register %s: status %d, body %s", email, res.status, res.body)
	}
	return account{
		id:           res.str(h.t, "user", "id"),
		email:        email,
		password:     password,
		accessToken:  res.str(h.t, "accessToken"),
		refreshToken: res.str(h.t, "refreshToken"),
	}
}

func (h *harness) createWorkout(token string) string {
	h.t.Helper()

	res := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: token, body: map[string]any{"title": "Push day"}})
	if res.status != http.StatusCreated {
		h.t.Fatalf("create workout: status %d, body %s", res.status, res.body)
	}
	return res.str(h.t, "id")
}

func validSetBody(mutationID string) map[string]any {
	return map[string]any{
		"exerciseId":       "lat-pulldown",
		"setNumber":        2,
		"weightKg":         62.5,
		"repetitions":      10,
		"rir":              2,
		"clientMutationId": mutationID,
	}
}
