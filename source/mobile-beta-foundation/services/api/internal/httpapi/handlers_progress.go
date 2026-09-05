package httpapi

import (
	"net/http"
	"time"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

type progressWindowResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type bestWeightResponse struct {
	WeightKg    float64 `json:"weightKg"`
	Repetitions int     `json:"repetitions"`
	AchievedAt  string  `json:"achievedAt"`
}

type best1RMResponse struct {
	Estimated1RMKg float64 `json:"estimated1RmKg"`
	WeightKg       float64 `json:"weightKg"`
	Repetitions    int     `json:"repetitions"`
	AchievedAt     string  `json:"achievedAt"`
}

type exerciseRecordResponse struct {
	ExerciseID       string             `json:"exerciseId"`
	Sets             int                `json:"sets"`
	Repetitions      int                `json:"repetitions"`
	VolumeKg         float64            `json:"volumeKg"`
	BestWeight       bestWeightResponse `json:"bestWeight"`
	BestEstimated1RM best1RMResponse    `json:"bestEstimated1Rm"`
	LastPerformedAt  string             `json:"lastPerformedAt"`
}

type weeklyVolumeResponse struct {
	WeekStart   string  `json:"weekStart"`
	Sets        int     `json:"sets"`
	Repetitions int     `json:"repetitions"`
	VolumeKg    float64 `json:"volumeKg"`
	Workouts    int     `json:"workouts"`
}

type weeklyAdherenceResponse struct {
	WeekStart      string  `json:"weekStart"`
	Started        int     `json:"started"`
	Completed      int     `json:"completed"`
	Cancelled      int     `json:"cancelled"`
	InProgress     int     `json:"inProgress"`
	CompletionRate float64 `json:"completionRate"`
}

type adherenceTotalsResponse struct {
	Started           int     `json:"started"`
	Completed         int     `json:"completed"`
	Cancelled         int     `json:"cancelled"`
	InProgress        int     `json:"inProgress"`
	CompletionRate    float64 `json:"completionRate"`
	WeeksInWindow     int     `json:"weeksInWindow"`
	WeeksWithTraining int     `json:"weeksWithTraining"`
}

type adherenceResponse struct {
	Weeks  []weeklyAdherenceResponse `json:"weeks"`
	Totals adherenceTotalsResponse   `json:"totals"`
}

type progressResponse struct {
	Window       progressWindowResponse   `json:"window"`
	Strength     []exerciseRecordResponse `json:"strength"`
	WeeklyVolume []weeklyVolumeResponse   `json:"weeklyVolume"`
	Adherence    adherenceResponse        `json:"adherence"`
}

// handleProgress feeds the "Прогресс" screen in one round trip: strength
// records per exercise, volume per ISO week and plan adherence per ISO week.
//
// Everything is aggregated by the store; the handler only formats. Sets that
// belong to cancelled workouts are excluded from strength and volume — a
// session the athlete threw away must not become a personal record.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request, user store.User) {
	query := r.URL.Query()

	from, err := optionalTime(query.Get("from"))
	if err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"from must be an RFC 3339 timestamp or a YYYY-MM-DD date")
		return
	}
	to, err := optionalTime(query.Get("to"))
	if err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"to must be an RFC 3339 timestamp or a YYYY-MM-DD date")
		return
	}
	limit, err := optionalInt(query.Get("exerciseLimit"), 0)
	if err != nil || limit < 0 {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"exerciseLimit must be a non-negative integer")
		return
	}

	report, err := s.workouts.Progress(r.Context(), user.ID, workouts.ProgressQuery{
		From: from, To: to, ExerciseLimit: limit,
	})
	if err != nil {
		s.internal(w, r, "progress failed", err)
		return
	}

	body := progressResponse{
		Window: progressWindowResponse{
			From: rfc3339(report.Window.From),
			To:   rfc3339(report.Window.To),
		},
		Strength:     make([]exerciseRecordResponse, 0, len(report.Strength)),
		WeeklyVolume: make([]weeklyVolumeResponse, 0, len(report.Volume)),
		Adherence: adherenceResponse{
			Weeks: make([]weeklyAdherenceResponse, 0, len(report.Adherence)),
			Totals: adherenceTotalsResponse{
				Started:           report.Totals.Started,
				Completed:         report.Totals.Completed,
				Cancelled:         report.Totals.Cancelled,
				InProgress:        report.Totals.InProgress,
				CompletionRate:    report.Totals.CompletionRate,
				WeeksInWindow:     report.Totals.WeeksInWindow,
				WeeksWithTraining: report.Totals.WeeksWithTraining,
			},
		},
	}
	for _, record := range report.Strength {
		body.Strength = append(body.Strength, exerciseRecordResponse{
			ExerciseID:  record.ExerciseID,
			Sets:        record.Sets,
			Repetitions: record.Repetitions,
			VolumeKg:    round2(record.VolumeKg),
			BestWeight: bestWeightResponse{
				WeightKg:    record.BestWeightKg,
				Repetitions: record.BestWeightReps,
				AchievedAt:  rfc3339(record.BestWeightAt),
			},
			BestEstimated1RM: best1RMResponse{
				Estimated1RMKg: round2(record.BestEstimated1RM),
				WeightKg:       record.Best1RMWeightKg,
				Repetitions:    record.Best1RMReps,
				AchievedAt:     rfc3339(record.Best1RMAt),
			},
			LastPerformedAt: rfc3339(record.LastPerformedAt),
		})
	}
	for _, week := range report.Volume {
		body.WeeklyVolume = append(body.WeeklyVolume, weeklyVolumeResponse{
			WeekStart:   rfc3339(week.WeekStart),
			Sets:        week.Sets,
			Repetitions: week.Repetitions,
			VolumeKg:    round2(week.VolumeKg),
			Workouts:    week.Workouts,
		})
	}
	for _, week := range report.Adherence {
		rate := 0.0
		if week.Started > 0 {
			rate = round4(float64(week.Completed) / float64(week.Started))
		}
		body.Adherence.Weeks = append(body.Adherence.Weeks, weeklyAdherenceResponse{
			WeekStart:      rfc3339(week.WeekStart),
			Started:        week.Started,
			Completed:      week.Completed,
			Cancelled:      week.Cancelled,
			InProgress:     week.InProgress,
			CompletionRate: rate,
		})
	}

	writeJSON(w, s.log, http.StatusOK, body)
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
