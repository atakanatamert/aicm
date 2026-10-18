// SPDX-License-Identifier: AGPL-3.0-or-later

package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"
)

// RollForwardArgs runs the daily renewal roll-forward. It carries no data.
type RollForwardArgs struct{}

// Kind implements river.JobArgs.
func (RollForwardArgs) Kind() string { return "account_roll_forward" }

// RollForwardWorker advances renewals that passed while the hub was idle.
type RollForwardWorker struct {
	river.WorkerDefaults[RollForwardArgs]
	Service *Service
	Hook    OnRenewal
}

// Work advances every due account to today.
func (w *RollForwardWorker) Work(ctx context.Context, _ *river.Job[RollForwardArgs]) error {
	if w.Service == nil {
		return errors.New("account roll-forward: no service")
	}
	advanced, err := w.Service.RollForwardDue(ctx, time.Now().UTC(), w.Hook)
	if err != nil {
		return fmt.Errorf("rolling renewals forward: %w", err)
	}
	w.Service.log.InfoContext(ctx, "renewals rolled forward", "accounts", advanced)
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

// PeriodicJob rolls renewals forward every day at 00:10 UTC, and once on
// start so a hub that missed days catches up immediately.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		dailySchedule{hour: 0, minute: 10},
		func() (river.JobArgs, *river.InsertOpts) {
			return RollForwardArgs{}, &river.InsertOpts{
				UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour},
			}
		},
		&river.PeriodicJobOpts{ID: "account_roll_forward", RunOnStart: true},
	)
}
