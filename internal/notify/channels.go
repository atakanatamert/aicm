// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// Errors returned by the channel service.
var (
	ErrChannelNotFound  = errors.New("notification channel not found")
	ErrChannelInvalid   = errors.New("invalid notification channel")
	ErrChannelForbidden = errors.New("notification channels need an admin")
	ErrChannelSend      = errors.New("notification channel send failed")
)

// defaultNtfyServer is used when an ntfy channel does not set config.server.
const defaultNtfyServer = "https://ntfy.sh"

// Channel is one configured alert channel plus non-secret secret metadata.
// The secret value itself never leaves the service.
type Channel struct {
	ID          string
	Kind        string
	Name        string
	Config      map[string]string
	Secret      secrets.Info
	SecretSet   bool
	MinSeverity string
	Kinds       []string
	Enabled     bool
	LastSentAt  *time.Time
	LastError   *string
	CreatedAt   time.Time
}

// ChannelCreate is a new channel. Secret is the webhook URL or token; it is
// required for every kind except ntfy.
type ChannelCreate struct {
	Kind        string
	Name        string
	Config      map[string]string
	Secret      *string
	MinSeverity string
	Kinds       []string
	Enabled     *bool
}

// ChannelUpdate changes one channel. A nil field leaves that value alone;
// a present Secret replaces the stored secret and "" clears it.
type ChannelUpdate struct {
	Name        *string
	Config      *map[string]string
	Secret      *string
	MinSeverity *string
	Kinds       *[]string
	Enabled     *bool
}

// ChannelService reads and writes alert channels. The delivery worker and
// the alert hook use the same filters, so a queued delivery always matches
// the channel that was enabled when the alert fired.
type ChannelService struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	store     secrets.Store
	publicURL string
	log       *slog.Logger
}

// NewChannelService builds the channel service. publicURL is the hub's
// externally reachable base URL, used in test messages.
func NewChannelService(pool *pgxpool.Pool, store secrets.Store, log *slog.Logger, publicURL string) *ChannelService {
	return &ChannelService{
		pool:      pool,
		q:         db.New(pool),
		store:     store,
		publicURL: publicURL,
		log:       log,
	}
}

// ChannelMatches reports whether a channel receives an alert. A disabled
// channel never matches; otherwise the alert's severity must reach the
// channel's minimum and its kind must be selected (an empty list means all).
func ChannelMatches(ch db.NotificationChannel, kind, severity string) bool {
	if !ch.Enabled {
		return false
	}
	if severityRank(severity) < severityRank(ch.MinSeverity) {
		return false
	}
	if len(ch.Kinds) == 0 {
		return true
	}
	for _, k := range ch.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// severityRank orders info < warning < critical. An unknown severity ranks
// as warning so a malformed row still sends rather than disappearing.
func severityRank(severity string) int {
	switch severity {
	case "info":
		return 0
	case "critical":
		return 2
	default:
		return 1
	}
}

// List returns every channel of the org, oldest first.
func (s *ChannelService) List(ctx context.Context, p auth.Principal) ([]Channel, error) {
	rows, err := s.q.ListNotificationChannels(ctx, p.OrgID)
	if err != nil {
		return nil, fmt.Errorf("listing notification channels: %w", err)
	}
	out := make([]Channel, 0, len(rows))
	for _, row := range rows {
		ch, err := s.fromRow(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, nil
}

// Create validates and stores a channel, its secret and one audit row.
func (s *ChannelService) Create(ctx context.Context, p auth.Principal, in ChannelCreate) (Channel, error) {
	if !p.IsAdmin() {
		return Channel{}, ErrChannelForbidden
	}

	name := strings.TrimSpace(in.Name)
	severity := in.MinSeverity
	if severity == "" {
		severity = "warning"
	}
	kinds := append([]string{}, in.Kinds...)
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	config := copyConfig(in.Config)
	normalizeConfig(in.Kind, config)

	var secretValue string
	if in.Secret != nil {
		secretValue = strings.TrimSpace(*in.Secret)
	}
	if err := validateChannel(in.Kind, name, config, secretValue != "", secretValue, severity, kinds); err != nil {
		return Channel{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Channel{}, fmt.Errorf("starting notification channel create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	var secretID *string
	if secretValue != "" {
		info, err := s.store.Put(ctx, p.OrgID, secretValue)
		if err != nil {
			return Channel{}, fmt.Errorf("storing notification channel secret: %w", err)
		}
		secretID = &info.ID
	}

	encoded, err := encodeConfig(config)
	if err != nil {
		return Channel{}, err
	}
	row, err := q.InsertNotificationChannel(ctx, db.InsertNotificationChannelParams{
		OrgID:       p.OrgID,
		Kind:        in.Kind,
		Name:        name,
		Config:      encoded,
		SecretID:    secretID,
		MinSeverity: severity,
		Kinds:       kinds,
		Enabled:     enabled,
	})
	if err != nil {
		if secretID != nil {
			_ = s.store.Delete(ctx, p.OrgID, *secretID)
		}
		return Channel{}, fmt.Errorf("creating notification channel: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "notification_channel.create",
		TargetKind: "notification_channel",
		TargetID:   row.ID,
		After:      channelSnapshot(row),
	}); err != nil {
		return Channel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Channel{}, fmt.Errorf("committing notification channel create: %w", err)
	}
	return s.fromRow(ctx, row)
}

// Update applies a partial change, replacing or clearing the secret when
// present, and records one audit row.
func (s *ChannelService) Update(ctx context.Context, p auth.Principal, id string, in ChannelUpdate) (Channel, error) {
	if !p.IsAdmin() {
		return Channel{}, ErrChannelForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Channel{}, fmt.Errorf("starting notification channel update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{OrgID: p.OrgID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, ErrChannelNotFound
	}
	if err != nil {
		return Channel{}, fmt.Errorf("loading notification channel: %w", err)
	}

	name := before.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	severity := before.MinSeverity
	if in.MinSeverity != nil {
		severity = *in.MinSeverity
	}
	kinds := append([]string{}, before.Kinds...)
	if in.Kinds != nil {
		kinds = append([]string{}, (*in.Kinds)...)
	}
	enabled := before.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	config, err := decodeConfig(before.Config)
	if err != nil {
		return Channel{}, err
	}
	if in.Config != nil {
		config = copyConfig(*in.Config)
	}
	normalizeConfig(before.Kind, config)

	// Validate the URL or token only when this request supplies it; the
	// stored one was validated when it was written.
	var supplied string
	if in.Secret != nil {
		supplied = strings.TrimSpace(*in.Secret)
	}
	secretPresent := supplied != ""
	if in.Secret == nil {
		secretPresent = before.SecretID != nil
	}
	if err := validateChannel(before.Kind, name, config, secretPresent, supplied, severity, kinds); err != nil {
		return Channel{}, err
	}

	var secretID *string
	var clearedSecret string
	if in.Secret != nil {
		switch {
		case supplied == "":
			if before.SecretID != nil {
				clearedSecret = *before.SecretID
			}
		case before.SecretID != nil:
			info, err := s.store.Replace(ctx, p.OrgID, *before.SecretID, supplied)
			if err != nil {
				return Channel{}, fmt.Errorf("replacing notification channel secret: %w", err)
			}
			secretID = &info.ID
		default:
			info, err := s.store.Put(ctx, p.OrgID, supplied)
			if err != nil {
				return Channel{}, fmt.Errorf("storing notification channel secret: %w", err)
			}
			secretID = &info.ID
		}
	} else {
		secretID = before.SecretID
	}

	encoded, err := encodeConfig(config)
	if err != nil {
		return Channel{}, err
	}
	row, err := q.UpdateNotificationChannel(ctx, db.UpdateNotificationChannelParams{
		Name:        name,
		Config:      encoded,
		SecretID:    secretID,
		MinSeverity: severity,
		Kinds:       kinds,
		Enabled:     enabled,
		OrgID:       p.OrgID,
		ID:          id,
	})
	if err != nil {
		return Channel{}, fmt.Errorf("updating notification channel: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "notification_channel.update",
		TargetKind: "notification_channel",
		TargetID:   row.ID,
		Before:     channelSnapshot(before),
		After:      channelSnapshot(row),
	}); err != nil {
		return Channel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Channel{}, fmt.Errorf("committing notification channel update: %w", err)
	}

	if clearedSecret != "" {
		if err := s.store.Delete(ctx, p.OrgID, clearedSecret); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return Channel{}, fmt.Errorf("deleting cleared notification channel secret: %w", err)
		}
	}
	return s.fromRow(ctx, row)
}

// Delete removes a channel and its secret, and records one audit row.
func (s *ChannelService) Delete(ctx context.Context, p auth.Principal, id string) error {
	if !p.IsAdmin() {
		return ErrChannelForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting notification channel delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{OrgID: p.OrgID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("loading notification channel: %w", err)
	}
	deleted, err := q.DeleteNotificationChannel(ctx, db.DeleteNotificationChannelParams{OrgID: p.OrgID, ID: id})
	if err != nil {
		return fmt.Errorf("deleting notification channel: %w", err)
	}
	if deleted == 0 {
		return ErrChannelNotFound
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "notification_channel.delete",
		TargetKind: "notification_channel",
		TargetID:   id,
		Before:     channelSnapshot(before),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing notification channel delete: %w", err)
	}
	if before.SecretID != nil {
		if err := s.store.Delete(ctx, p.OrgID, *before.SecretID); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return fmt.Errorf("deleting notification channel secret: %w", err)
		}
	}
	return nil
}

// Test sends one fixed message synchronously and records the outcome.
func (s *ChannelService) Test(ctx context.Context, p auth.Principal, id string) error {
	if !p.IsAdmin() {
		return ErrChannelForbidden
	}
	row, err := s.q.GetNotificationChannel(ctx, db.GetNotificationChannelParams{OrgID: p.OrgID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("loading notification channel: %w", err)
	}

	secret, err := s.reveal(ctx, row)
	if err != nil {
		return err
	}
	sendErr := sendChannel(ctx, row, secret, testMessage(s.publicURL))
	if sendErr != nil {
		if err := s.q.MarkNotificationChannelError(ctx, db.MarkNotificationChannelErrorParams{
			LastError: new(sendErr.Error()),
			OrgID:     p.OrgID,
			ID:        id,
		}); err != nil && s.log != nil {
			s.log.WarnContext(ctx, "recording notification channel test error", "channel_id", id, "err", err)
		}
		if err := audit.Write(ctx, s.q, p.Actor(), audit.Entry{
			Action:     "notification_channel.test",
			TargetKind: "notification_channel",
			TargetID:   id,
			After:      map[string]any{"ok": false, "error": sendErr.Error()},
		}); err != nil && s.log != nil {
			s.log.WarnContext(ctx, "auditing notification channel test", "channel_id", id, "err", err)
		}
		return fmt.Errorf("%w: %w", ErrChannelSend, sendErr)
	}

	if err := s.q.MarkNotificationChannelSent(ctx, db.MarkNotificationChannelSentParams{
		OrgID: p.OrgID,
		ID:    id,
	}); err != nil {
		return fmt.Errorf("marking notification channel sent: %w", err)
	}
	if err := audit.Write(ctx, s.q, p.Actor(), audit.Entry{
		Action:     "notification_channel.test",
		TargetKind: "notification_channel",
		TargetID:   id,
		After:      map[string]any{"ok": true},
	}); err != nil {
		return err
	}
	return nil
}

func (s *ChannelService) fromRow(ctx context.Context, row db.NotificationChannel) (Channel, error) {
	config, err := decodeConfig(row.Config)
	if err != nil {
		return Channel{}, err
	}
	out := Channel{
		ID:          row.ID,
		Kind:        row.Kind,
		Name:        row.Name,
		Config:      config,
		MinSeverity: row.MinSeverity,
		Kinds:       append([]string{}, row.Kinds...),
		Enabled:     row.Enabled,
		LastSentAt:  row.LastSentAt,
		LastError:   row.LastError,
		CreatedAt:   row.CreatedAt,
	}
	if row.SecretID != nil {
		info, err := s.store.Info(ctx, row.OrgID, *row.SecretID)
		switch {
		case errors.Is(err, secrets.ErrNotFound):
			// The referenced secret is gone; report it as unset.
		case err != nil:
			return Channel{}, fmt.Errorf("loading notification channel secret info: %w", err)
		default:
			out.Secret = info
			out.SecretSet = true
		}
	}
	return out, nil
}

func (s *ChannelService) reveal(ctx context.Context, row db.NotificationChannel) (string, error) {
	if row.SecretID == nil {
		return "", nil
	}
	value, err := s.store.Reveal(ctx, row.OrgID, *row.SecretID)
	if err != nil {
		return "", fmt.Errorf("revealing notification channel secret: %w", err)
	}
	return value, nil
}

func channelSnapshot(ch db.NotificationChannel) map[string]any {
	return map[string]any{
		"kind":           ch.Kind,
		"name":           ch.Name,
		"config":         ch.Config,
		"credential_set": ch.SecretID != nil,
		"min_severity":   ch.MinSeverity,
		"kinds":          ch.Kinds,
		"enabled":        ch.Enabled,
	}
}

var alertKinds = map[string]struct{}{
	"renewal":         {},
	"promo_ending":    {},
	"low_balance":     {},
	"meter_exhausted": {},
	"quota_waste":     {},
	"sync_failing":    {},
	"agent_offline":   {},
	"meter_reset":     {},
	"budget":          {},
	"spend_anomaly":   {},
	"wrong_wallet":    {},
}

var channelKinds = map[string]struct{}{
	"webhook":  {},
	"ntfy":     {},
	"telegram": {},
	"discord":  {},
	"slack":    {},
}

// configKeys lists the config keys each kind accepts; anything else is a
// client mistake and rejected.
var configKeys = map[string]map[string]struct{}{
	"webhook":  {},
	"discord":  {},
	"slack":    {},
	"ntfy":     {"topic": {}, "server": {}},
	"telegram": {"chat_id": {}},
}

func validateChannel(kind, name string, config map[string]string, secretPresent bool, secret, minSeverity string, kinds []string) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrChannelInvalid, fmt.Sprintf(format, args...))
	}
	if _, ok := channelKinds[kind]; !ok {
		return invalid("unknown kind %q", kind)
	}
	if name == "" {
		return invalid("name is required")
	}
	switch minSeverity {
	case "info", "warning", "critical":
	default:
		return invalid("min_severity must be info, warning or critical")
	}
	for _, k := range kinds {
		if _, ok := alertKinds[k]; !ok {
			return invalid("unknown kind %q", k)
		}
	}
	allowed := configKeys[kind]
	for key := range config {
		if _, ok := allowed[key]; !ok {
			return invalid("config key %q is not allowed for kind %s", key, kind)
		}
	}

	switch kind {
	case "webhook", "discord", "slack":
		if !secretPresent {
			return invalid("%s needs a secret URL", kind)
		}
		if secret != "" && !validChannelURL(secret) {
			return invalid("%s secret must be an https URL", kind)
		}
	case "ntfy":
		if strings.TrimSpace(config["topic"]) == "" {
			return invalid("ntfy needs config.topic")
		}
		if server := config["server"]; server != "" && !validChannelURL(server) {
			return invalid("ntfy config.server must be an https URL")
		}
	case "telegram":
		if !secretPresent {
			return invalid("telegram needs a bot token as its secret")
		}
		if strings.TrimSpace(config["chat_id"]) == "" {
			return invalid("telegram needs config.chat_id")
		}
	}
	return nil
}

// validChannelURL accepts https everywhere and http only for a local host:
// localhost, 127.0.0.1 or a single-label name such as "nas" or "ntfy", which
// is how a LAN tool is usually reached.
func validChannelURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || !strings.Contains(host, ".")
	default:
		return false
	}
}

// normalizeConfig fills the ntfy server default so the stored config is the
// one the sender uses.
func normalizeConfig(kind string, config map[string]string) {
	if kind != "ntfy" {
		return
	}
	if strings.TrimSpace(config["server"]) == "" {
		config["server"] = defaultNtfyServer
	}
}

func copyConfig(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func decodeConfig(raw json.RawMessage) (map[string]string, error) {
	out := map[string]string{}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding notification channel config: %w", err)
	}
	return out, nil
}

func encodeConfig(config map[string]string) (json.RawMessage, error) {
	if config == nil {
		config = map[string]string{}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encoding notification channel config: %w", err)
	}
	return encoded, nil
}
