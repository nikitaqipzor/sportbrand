package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"athletica.ai/api/internal/auth"
	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyUser
)

// RequestIDHeader is echoed back so mobile logs and server logs can be joined.
const RequestIDHeader = "X-Request-Id"

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// withRequestID assigns or reuses a request correlation ID.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if id == "" || len(id) > 64 {
			id = ids.NewUUID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

// withRecovery turns a panic into a 500 instead of a dropped connection.
func withRecovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						"request_id", RequestIDFrom(r.Context()),
						"path", r.URL.Path,
						"panic", rec)
					writeError(w, log, http.StatusInternalServerError, codeInternal, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// withLogging emits one structured line per request. It never logs bodies,
// tokens or e-mail addresses.
func withLogging(log *slog.Logger, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			log.Info("http request",
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", now().Sub(start).Milliseconds(),
			)
		})
	}
}

// RequestIDFrom returns the correlation ID stored on the context.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// UserFrom returns the authenticated user stored on the context.
func UserFrom(ctx context.Context) (store.User, bool) {
	user, ok := ctx.Value(ctxKeyUser).(store.User)
	return user, ok
}

// authenticated wraps a handler so it only runs for a valid bearer token. The
// resolved user — and therefore every user ID used downstream — comes from the
// signed token subject and from nowhere else (audit finding H1).
func (s *Server) authenticated(next func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="athletica"`)
			writeError(w, s.log, http.StatusUnauthorized, codeUnauthorized, "missing bearer access token")
			return
		}

		user, err := s.auth.Authenticate(r.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrTokenExpired):
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="expired"`)
				writeError(w, s.log, http.StatusUnauthorized, codeUnauthorized, "access token expired")
			case errors.Is(err, auth.ErrTokenMalformed), errors.Is(err, auth.ErrTokenSignature), errors.Is(err, auth.ErrTokenClaims):
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				writeError(w, s.log, http.StatusUnauthorized, codeUnauthorized, "invalid access token")
			default:
				s.log.Error("authenticate failed", "request_id", RequestIDFrom(r.Context()), "error", err.Error())
				writeError(w, s.log, http.StatusInternalServerError, codeInternal, "internal error")
			}
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), ctxKeyUser, user)), user)
	}
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

// clientIP resolves the throttling key for a request. Forwarded headers are
// only honoured when the deployment explicitly declares it sits behind a proxy;
// otherwise a client could forge its own rate-limit bucket.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxyHeaders {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-Ip")); real != "" {
			return real
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
