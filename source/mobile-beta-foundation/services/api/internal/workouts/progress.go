package workouts

import (
	"context"
	"math"
	"time"

	"athletica.ai/api/internal/store"
)

// Progress window bounds. They are part of the published contract.
const (
	// DefaultProgressWeeks is how far back GET /progress looks by default.
	DefaultProgressWeeks = 12
	// MaxProgressWeeks caps the window a client may ask for, so one request
	// cannot make the database scan an unbounded history.
	MaxProgressWeeks = 104
	// DefaultExerciseLimit caps the strength table by default.
	DefaultExerciseLimit = 50
	// MaxExerciseLimit is the hard ceiling for that table.
	MaxExerciseLimit = 200
)

// ProgressQuery asks for one progress report. Both bounds are optional; the
// service resolves them to a whole number of ISO weeks ending "now".
type ProgressQuery struct {
	From          *time.Time
	To            *time.Time
	ExerciseLimit int
}

// ProgressReport is everything the "Прогресс" screen needs, in one round trip.
type ProgressReport struct {
	Window    store.ProgressWindow
	Strength  []store.ExerciseRecord
	Volume    []store.WeeklyVolume
	Adherence []store.WeeklyAdherence
	Totals    AdherenceTotals
}

// AdherenceTotals summarises plan adherence over the whole window.
type AdherenceTotals struct {
	Started           int
	Completed         int
	Cancelled         int
	InProgress        int
	CompletionRate    float64
	WeeksInWindow     int
	WeeksWithTraining int
}

// Progress builds the report. Every aggregate is computed by the store — the
// service adds up already-aggregated weeks and never touches a set row.
func (s *Service) Progress(ctx context.Context, userID string, query ProgressQuery) (ProgressReport, error) {
	window := s.resolveWindow(query)

	limit := query.ExerciseLimit
	switch {
	case limit <= 0:
		limit = DefaultExerciseLimit
	case limit > MaxExerciseLimit:
		limit = MaxExerciseLimit
	}

	strength, err := s.store.ExerciseRecords(ctx, userID, window, limit)
	if err != nil {
		return ProgressReport{}, err
	}
	volume, err := s.store.WeeklyVolume(ctx, userID, window)
	if err != nil {
		return ProgressReport{}, err
	}
	adherence, err := s.store.WeeklyAdherence(ctx, userID, window)
	if err != nil {
		return ProgressReport{}, err
	}

	report := ProgressReport{Window: window, Strength: strength, Volume: volume, Adherence: adherence}
	for _, week := range adherence {
		report.Totals.Started += week.Started
		report.Totals.Completed += week.Completed
		report.Totals.Cancelled += week.Cancelled
		report.Totals.InProgress += week.InProgress
	}
	if report.Totals.Started > 0 {
		report.Totals.CompletionRate = round4(float64(report.Totals.Completed) / float64(report.Totals.Started))
	}
	for _, week := range volume {
		if week.Sets > 0 {
			report.Totals.WeeksWithTraining++
		}
	}
	report.Totals.WeeksInWindow = int(math.Round(window.To.Sub(window.From).Hours() / (24 * 7)))
	return report, nil
}

// resolveWindow snaps the requested range to whole ISO weeks and clamps its
// length, so `from` and `to` can never be used to force a full-table scan.
func (s *Service) resolveWindow(query ProgressQuery) store.ProgressWindow {
	now := s.now().UTC()

	to := store.WeekStart(now).AddDate(0, 0, 7) // end of the current week
	if query.To != nil {
		to = store.WeekStart(*query.To).AddDate(0, 0, 7)
	}
	from := to.AddDate(0, 0, -7*DefaultProgressWeeks)
	if query.From != nil {
		from = store.WeekStart(*query.From)
	}

	if !from.Before(to) {
		from = to.AddDate(0, 0, -7)
	}
	if earliest := to.AddDate(0, 0, -7*MaxProgressWeeks); from.Before(earliest) {
		from = earliest
	}
	return store.ProgressWindow{From: from, To: to}
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
