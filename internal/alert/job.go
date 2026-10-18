// SPDX-License-Identifier: AGPL-3.0-or-later

package alert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"
)

// EvaluateArgs carries nothing: the worker evaluates at the time it runs.
type EvaluateArgs struct{}

// Kind implements river.JobArgs.
func (EvaluateArgs) Kind() string { return "alert_evaluate" }

// EvaluateWorker runs one alert evaluation pass.
type EvaluateWorker struct {
	river.WorkerDefaults[EvaluateArgs]
	Service *Service
}

// Work evaluates every org's rules at the current time.
func (w *EvaluateWorker) Work(ctx context.Context, _ *river.Job[EvaluateArgs]) error {
	if w.Service == nil {
		return errors.New("alert evaluate: no service")
	}
	n, err := w.Service.Evaluate(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("evaluating alerts: %w", err)
	}
	if n > 0 && w.Service.log != nil {
		w.Service.log.InfoContext(ctx, "new alerts", "count", n)
	}
	return nil
}

// hourlySchedule fires once an hour at a fixed minute, in UTC.
type hourlySchedule struct {
	minute int
}

// Next returns the next occurrence of the schedule after now.
func (s hourlySchedule) Next(now time.Time) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), s.minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(time.Hour)
	}
	return next
}

// PeriodicJob evaluates rules every hour at minute 5, and once on start so a
// fresh hub fills its inbox immediately.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		hourlySchedule{minute: 5},
		func() (river.JobArgs, *river.InsertOpts) {
			return EvaluateArgs{}, &river.InsertOpts{
				UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour},
			}
		},
		&river.PeriodicJobOpts{ID: "alert_evaluate", RunOnStart: true},
	)
}
