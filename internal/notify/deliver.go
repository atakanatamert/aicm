// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// DeliverAlertArgs sends one alert through one channel. Unique by its three
// ids, so a retry or a restart never queues a second delivery.
type DeliverAlertArgs struct {
	OrgID, AlertID, ChannelID string
}

// Kind implements river.JobArgs.
func (DeliverAlertArgs) Kind() string { return "alert_deliver" }

// InsertOpts makes the job unique by its arguments and retries transient
// provider failures.
func (DeliverAlertArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 8,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// DeliverAlertWorker delivers one alert to one channel.
type DeliverAlertWorker struct {
	river.WorkerDefaults[DeliverAlertArgs]
	Pool      *pgxpool.Pool
	Secrets   secrets.Store
	PublicURL string
	Log       *slog.Logger
}

// Work delivers the alert once. A delivery already recorded, a deleted or
// disabled channel and a missing alert are all no-ops. A refusal from the
// provider records last_error and returns the error so River retries.
func (w *DeliverAlertWorker) Work(ctx context.Context, job *river.Job[DeliverAlertArgs]) error {
	if w.Pool == nil {
		return errors.New("alert deliver: no database pool")
	}
	if w.Secrets == nil {
		return errors.New("alert deliver: no secret store")
	}
	q := db.New(w.Pool)

	ch, err := q.GetEnabledNotificationChannel(ctx, db.GetEnabledNotificationChannelParams{
		OrgID: job.Args.OrgID,
		ID:    job.Args.ChannelID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loading notification channel for delivery: %w", err)
	}

	delivered, err := q.AlertDeliveryExists(ctx, db.AlertDeliveryExistsParams{
		OrgID:     job.Args.OrgID,
		AlertID:   job.Args.AlertID,
		ChannelID: job.Args.ChannelID,
	})
	if err != nil {
		return fmt.Errorf("checking alert delivery: %w", err)
	}
	if delivered {
		return nil
	}

	a, err := q.GetAlertForEmail(ctx, db.GetAlertForEmailParams{
		OrgID: job.Args.OrgID,
		ID:    job.Args.AlertID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loading alert for delivery: %w", err)
	}

	var secret string
	if ch.SecretID != nil {
		secret, err = w.Secrets.Reveal(ctx, ch.OrgID, *ch.SecretID)
		if err != nil {
			return fmt.Errorf("revealing notification channel secret: %w", err)
		}
	}

	if err := sendChannel(ctx, ch, secret, messageForAlert(a, w.PublicURL)); err != nil {
		if merr := q.MarkNotificationChannelError(ctx, db.MarkNotificationChannelErrorParams{
			LastError: new(err.Error()),
			OrgID:     ch.OrgID,
			ID:        ch.ID,
		}); merr != nil && w.Log != nil {
			w.Log.WarnContext(ctx, "recording notification channel error", "channel_id", ch.ID, "err", merr)
		}
		return fmt.Errorf("delivering alert %s to channel %s: %w", a.ID, ch.ID, err)
	}

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting alert delivery record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tq := db.New(tx)
	if err := tq.InsertAlertDelivery(ctx, db.InsertAlertDeliveryParams{
		OrgID:     ch.OrgID,
		AlertID:   a.ID,
		ChannelID: ch.ID,
	}); err != nil {
		return fmt.Errorf("recording alert delivery: %w", err)
	}
	if err := tq.MarkNotificationChannelSent(ctx, db.MarkNotificationChannelSentParams{
		OrgID: ch.OrgID,
		ID:    ch.ID,
	}); err != nil {
		return fmt.Errorf("marking notification channel sent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing alert delivery record: %w", err)
	}
	if w.Log != nil {
		w.Log.InfoContext(ctx, "alert delivered", "org_id", ch.OrgID, "alert_id", a.ID, "channel_id", ch.ID, "kind", ch.Kind)
	}
	return nil
}
