package httpapi

import (
	"context"
	"log/slog"
	"time"
)

// PruneRefreshTokens deletes refresh rows that expired or were revoked longer
// than the configured retention ago, and reports how many disappeared. It is
// exported so `api prune-tokens` and the background sweeper share one path.
func (s *Server) PruneRefreshTokens(ctx context.Context) (int64, error) {
	return s.auth.PruneRefreshTokens(ctx, s.cfg.RefreshTokenRetention)
}

// RunRefreshTokenSweeper prunes expired refresh tokens until ctx is cancelled.
//
// A failed sweep is logged and retried on the next tick: housekeeping must
// never take the API down. Returns immediately when the interval is zero, which
// is how a deployment opts out and runs `api prune-tokens` from cron instead.
func (s *Server) RunRefreshTokenSweeper(ctx context.Context) {
	interval := s.cfg.RefreshTokenSweepInterval
	if interval <= 0 {
		s.log.Info("refresh-token sweeper disabled", "reason", "ATHLETICA_REFRESH_TOKEN_SWEEP_INTERVAL=0")
		return
	}

	s.log.Info("refresh-token sweeper started",
		"interval", interval.String(), "retention", s.cfg.RefreshTokenRetention.String())

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.sweepOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) sweepOnce(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	deleted, err := s.PruneRefreshTokens(sweepCtx)
	if err != nil {
		s.log.Warn("refresh-token sweep failed", "error", err.Error())
		return
	}
	if deleted > 0 {
		s.log.Info("refresh-token sweep", "deleted", deleted, slog.String("retention", s.cfg.RefreshTokenRetention.String()))
	}
}
