package watch

import (
	"time"

	"github.com/ericlitman/threadbear/internal/state"
)

func archiveEligible(record state.TaskRecord, now time.Time, days int) bool {
	if record.Status != state.StatusComplete || days <= 0 {
		return false
	}
	start := record.LastSubstantiveActivity
	if record.StateStartedAt.After(start) {
		start = record.StateStartedAt
	}
	return !now.Before(start.Add(time.Duration(days) * 24 * time.Hour))
}

func laterActivity(previous, observed, fallback time.Time) time.Time {
	result := previous.UTC()
	if result.IsZero() {
		result = fallback.UTC()
	}
	if observed.After(result) {
		result = observed.UTC()
	}
	return result
}
