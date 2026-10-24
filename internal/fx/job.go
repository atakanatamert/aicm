// SPDX-License-Identifier: SUL-1.0

package fx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"
)

// backfillDays is how much history the first run fetches.
const backfillDays = 400

// RefreshArgs runs a rate refresh. It carries no data: the worker derives the
// range from what is stored.
type RefreshArgs struct{}

// Kind implements river.JobArgs.
func (RefreshArgs) Kind() string { return "fx_refresh" }

// RefreshWorker fetches and stores reference rates.
type RefreshWorker struct {
	river.WorkerDefaults[RefreshArgs]
	Service *Service
}

// Work backfills the last 400 days when no rates exist, otherwise catches up
// from three days before the newest stored date through today.
func (w *RefreshWorker) Work(ctx context.Context, _ *river.Job[RefreshArgs]) error {
	if w.Service == nil {
		return errors.New("fx refresh: no service")
	}
	today := day(time.Now())
	var from time.Time

	count, err := w.Service.q.CountRates(ctx)
	if err != nil {
		return fmt.Errorf("counting fx rates: %w", err)
	}
	if count == 0 {
		from = today.AddDate(0, 0, -(backfillDays - 1))
	} else {
		newest, err := w.Service.q.NewestRateDate(ctx)
		if err != nil {
			return fmt.Errorf("loading newest fx rate date: %w", err)
		}
		from = day(newest).AddDate(0, 0, -3)
	}
	if err := w.Service.Refresh(ctx, from, today); err != nil {
		return fmt.Errorf("refreshing fx rates: %w", err)
	}
	return nil
}

// dailySchedule is a PeriodicSchedule that fires once a day at a fixed UTC
// time.
type dailySchedule struct {
	hour   int
	minute int
}

// Next returns the next occurrence of the schedule after now.
func (s dailySchedule) Next(now time.Time) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), s.hour, s.minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// PeriodicJob refreshes rates every day at 16:30 UTC, and once on start so a
// fresh hub backfills immediately.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		dailySchedule{hour: 16, minute: 30},
		func() (river.JobArgs, *river.InsertOpts) {
			return RefreshArgs{}, &river.InsertOpts{
				UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour},
			}
		},
		&river.PeriodicJobOpts{ID: "fx_refresh", RunOnStart: true},
	)
}
