// SPDX-License-Identifier: SUL-1.0

package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/atakanatamert/aicm/internal/alert"
	"github.com/atakanatamert/aicm/internal/db"
)

// SendAlertArgs emails one alert. Unique by alert id, so a restart or a
// retry never queues a second email for the same event.
type SendAlertArgs struct {
	OrgID, AlertID string
}

// Kind implements river.JobArgs.
func (SendAlertArgs) Kind() string { return "alert_email" }

// InsertOpts makes the job unique by its arguments and retries transient
// SMTP failures.
func (SendAlertArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 8,
		UniqueOpts:  river.UniqueOpts{ByArgs: true},
	}
}

// SMTPSource loads an org's SMTP configuration. settings.Service implements
// it; the interface keeps notify out of the settings import cycle.
type SMTPSource interface {
	SMTP(ctx context.Context, orgID string) (SMTPConfig, bool, error)
}

// SendAlertWorker emails one alert to the users who can see it.
type SendAlertWorker struct {
	river.WorkerDefaults[SendAlertArgs]
	Pool      *pgxpool.Pool
	Settings  SMTPSource
	PublicURL string
	Log       *slog.Logger
}

// Work sends the alert's email once. An alert already emailed is a no-op, so
// a retry or a duplicate delivery never sends a second message.
func (w *SendAlertWorker) Work(ctx context.Context, job *river.Job[SendAlertArgs]) error {
	if w.Pool == nil {
		return errors.New("alert email: no database pool")
	}
	if w.Settings == nil {
		return errors.New("alert email: no settings source")
	}
	q := db.New(w.Pool)

	a, err := q.GetAlertForEmail(ctx, db.GetAlertForEmailParams{
		OrgID: job.Args.OrgID,
		ID:    job.Args.AlertID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("loading alert for email: %w", err)
	}
	if a.EmailedAt != nil {
		return nil
	}

	cfg, configured, err := w.Settings.SMTP(ctx, a.OrgID)
	if err != nil {
		return fmt.Errorf("loading SMTP settings: %w", err)
	}
	if !configured {
		if w.Log != nil {
			w.Log.InfoContext(ctx, "alert email skipped: SMTP is not configured",
				"org_id", a.OrgID, "alert_id", a.ID)
		}
		return nil
	}

	recipients, err := q.ListAlertEmailRecipients(ctx, db.ListAlertEmailRecipientsParams{
		OrgID:     a.OrgID,
		AccountID: a.AccountID,
	})
	if err != nil {
		return fmt.Errorf("listing alert email recipients: %w", err)
	}
	if len(recipients) == 0 {
		if w.Log != nil {
			w.Log.InfoContext(ctx, "alert email skipped: no recipients",
				"org_id", a.OrgID, "alert_id", a.ID)
		}
		return nil
	}

	msg, err := renderMessage(a, w.PublicURL)
	if err != nil {
		return err
	}
	msg.To = recipients
	if err := NewSMTP(cfg).Send(ctx, msg); err != nil {
		return fmt.Errorf("sending alert email: %w", err)
	}

	if _, err := q.MarkAlertEmailed(ctx, db.MarkAlertEmailedParams{
		OrgID: a.OrgID,
		ID:    a.ID,
	}); err != nil {
		return fmt.Errorf("marking alert emailed: %w", err)
	}
	return nil
}

// Hook returns an alert.Hook that queues the email and one delivery per
// matching enabled channel from inside the alert evaluation job, using that
// job's River client. pool reads the org's channels; the evaluation job
// itself has no transaction around the hook call.
func Hook(pool *pgxpool.Pool) alert.Hook {
	return func(ctx context.Context, a alert.NewAlert) error {
		client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
		if err != nil {
			return fmt.Errorf("getting river client: %w", err)
		}
		if _, err := client.Insert(ctx, SendAlertArgs{
			OrgID:   a.Alert.OrgID,
			AlertID: a.Alert.ID,
		}, nil); err != nil {
			return fmt.Errorf("queueing alert email: %w", err)
		}
		if pool == nil {
			return nil
		}
		channels, err := db.New(pool).ListEnabledNotificationChannels(ctx, a.Alert.OrgID)
		if err != nil {
			return fmt.Errorf("listing alert channels: %w", err)
		}
		for _, ch := range channels {
			if !ChannelMatches(ch, a.Alert.Kind, a.Alert.Severity) {
				continue
			}
			if _, err := client.Insert(ctx, DeliverAlertArgs{
				OrgID:     a.Alert.OrgID,
				AlertID:   a.Alert.ID,
				ChannelID: ch.ID,
			}, nil); err != nil {
				return fmt.Errorf("queueing alert delivery: %w", err)
			}
		}
		return nil
	}
}
