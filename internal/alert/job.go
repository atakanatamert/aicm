// SPDX-License-Identifier: SUL-1.0

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

// quarterHourSchedule fires at minutes 0, 15, 30 and 45, in UTC.
type quarterHourSchedule struct{}

// Next returns the next quarter hour after now.
func (quarterHourSchedule) Next(now time.Time) time.Time {
	now = now.UTC()
	next := now.Truncate(15 * time.Minute).Add(15 * time.Minute)
	return time.Date(next.Year(), next.Month(), next.Day(), next.Hour(), next.Minute(), 0, 0, time.UTC)
}

// PeriodicJob evaluates rules every 15 minutes, and once on start so a fresh
// hub fills its inbox immediately. UniqueOpts keeps a delayed tick from
// queueing the same period twice.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		quarterHourSchedule{},
		func() (river.JobArgs, *river.InsertOpts) {
			return EvaluateArgs{}, &river.InsertOpts{
				UniqueOpts: river.UniqueOpts{ByPeriod: 15 * time.Minute},
			}
		},
		&river.PeriodicJobOpts{ID: "alert_evaluate", RunOnStart: true},
	)
}
