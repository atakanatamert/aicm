// SPDX-License-Identifier: AGPL-3.0-or-later

// Package quota stores provider-native meters and their readings and derives
// the ranking score (score.go). Readings are stored exactly as reported.
package quota

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

// Errors.
var (
	ErrNotFound = errors.New("not found")
	// ErrManualConflict: the key belongs to a meter of the other kind
	// (a manual meter can't replace a reported one, and vice versa).
	ErrManualConflict = errors.New("meter key is used by a meter of another kind")
)

// Catalog looks up a provider's catalog entry by id.
type Catalog func(providerID string) (provider.Info, bool)

// PriceFunc returns an account's marginal cost per million tokens (micro
// units) for a model family, or ok=false when unknown.
type PriceFunc func(ctx context.Context, providerID, family string) (micro int64, ok bool)

// BalanceHook sees each newly stored reading of a currency meter that never
// resets, with the previous reading of that meter (nil for the first).
type BalanceHook func(ctx context.Context, orgID, accountID string, def meter.Def, prev *meter.Reading, cur meter.Reading) error

// Service reads and writes meters and computes scores.
type Service struct {
	pool        *pgxpool.Pool
	q           *db.Queries
	log         *slog.Logger
	catalog     Catalog
	price       PriceFunc
	balanceHook BalanceHook
	now         func() time.Time
}

// NewService builds the service. price may be nil until a price source exists;
// metered accounts then rank after flat-rate ones with unknown cost.
func NewService(pool *pgxpool.Pool, log *slog.Logger, catalog Catalog, price PriceFunc) *Service {
	return &Service{pool: pool, q: db.New(pool), log: log, catalog: catalog, price: price, now: time.Now}
}

// SetBalanceHook installs the hook called for each new reading of a currency
// meter that never resets.
func (s *Service) SetBalanceHook(h BalanceHook) {
	s.balanceHook = h
}

// RecordReadings upserts a meter definition and stores readings for it.
// Adapters and the agent ingest call it. Invalid readings are skipped and
// logged, never stored. It returns how many readings were new.
func (s *Service) RecordReadings(ctx context.Context, orgID, accountID string, def meter.Def, readings []meter.Reading, machineID *string) (int, error) {
	if err := def.Validate(); err != nil {
		return 0, err
	}
	m, err := s.upsertMeter(ctx, s.q, orgID, accountID, def, false)
	if errors.Is(err, ErrManualConflict) {
		s.log.WarnContext(ctx, "reading for a manual meter key ignored", "account_id", accountID, "meter", def.Key)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range readings {
		if err := r.Validate(def); err != nil {
			s.log.WarnContext(ctx, "invalid meter reading dropped", "account_id", accountID, "meter", def.Key, "err", err)
			continue
		}
		added, err := s.insertReading(ctx, s.q, orgID, m.ID, r, machineID)
		if err != nil {
			return n, err
		}
		n += added
		if added == 1 && s.balanceHook != nil && def.Unit == meter.UnitCurrency && def.ResetKind == meter.ResetNever {
			s.notifyBalance(ctx, orgID, accountID, m.ID, def, r)
		}
	}
	return n, nil
}

// notifyBalance passes a new wallet reading and its predecessor to the
// balance hook. A failing hook is logged, never returned: it must not drop
// the reading.
func (s *Service) notifyBalance(ctx context.Context, orgID, accountID, meterID string, def meter.Def, cur meter.Reading) {
	row, err := s.q.PreviousReading(ctx, db.PreviousReadingParams{
		OrgID: orgID, MeterID: meterID, BeforeTime: cur.CapturedAt.UTC(),
	})
	var prev *meter.Reading
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		s.log.WarnContext(ctx, "loading previous reading for balance hook", "account_id", accountID, "meter", def.Key, "err", err)
		return
	default:
		p := readingOf(def.Key, row)
		prev = &p
	}
	if err := s.balanceHook(ctx, orgID, accountID, def, prev, cur); err != nil {
		s.log.WarnContext(ctx, "balance hook failed", "account_id", accountID, "meter", def.Key, "err", err)
	}
}

func (s *Service) upsertMeter(ctx context.Context, q *db.Queries, orgID, accountID string, def meter.Def, manual bool) (db.QuotaMeter, error) {
	var window *int32
	if def.WindowMinutes > 0 {
		window = new(int32(def.WindowMinutes)) //nolint:gosec // window minutes are small
	}
	var unitCurrency *string
	if def.UnitCurrency != "" {
		unitCurrency = new(def.UnitCurrency)
	}
	m, err := q.UpsertMeter(ctx, db.UpsertMeterParams{
		OrgID: orgID, AccountID: accountID, Key: def.Key, Label: def.Label, Unit: string(def.Unit),
		UnitCurrency: unitCurrency, ResetKind: string(def.ResetKind), WindowMinutes: window,
		LimitValue: def.Limit, Manual: manual,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrManualConflict
	}
	if err != nil {
		return m, fmt.Errorf("upserting meter %s: %w", def.Key, err)
	}
	return m, nil
}

func (s *Service) insertReading(ctx context.Context, q *db.Queries, orgID, meterID string, r meter.Reading, machineID *string) (int, error) {
	var resets *time.Time
	if r.ResetsAt != nil {
		resets = new(r.ResetsAt.UTC())
	}
	n, err := q.InsertReading(ctx, db.InsertReadingParams{
		OrgID: orgID, MeterID: meterID, Used: r.Used, Remaining: r.Remaining, LimitValue: r.Limit,
		ResetsAt: resets, CapturedAt: r.CapturedAt.UTC(), Source: string(r.Source), MachineID: machineID,
	})
	if err != nil {
		return 0, fmt.Errorf("inserting reading: %w", err)
	}
	return int(n), nil
}

// UpsertManualMeter declares or changes a manual meter on an account the
// principal may see.
func (s *Service) UpsertManualMeter(ctx context.Context, p auth.Principal, accountID string, def meter.Def) (meter.Def, error) {
	if err := def.Validate(); err != nil {
		return def, err
	}
	if _, err := s.visibleAccount(ctx, p, accountID); err != nil {
		return def, err
	}
	if _, err := s.upsertMeter(ctx, s.q, p.OrgID, accountID, def, true); err != nil {
		return def, err
	}
	if err := audit.Write(ctx, s.q, p.Actor(), audit.Entry{
		Action:     "meter.upsert",
		TargetKind: "meter",
		TargetID:   accountID + "/" + def.Key,
		After:      def,
	}); err != nil {
		return def, err
	}
	return def, nil
}

// RecordManualReading stores a reading typed in by a user.
func (s *Service) RecordManualReading(ctx context.Context, p auth.Principal, accountID, key string, r meter.Reading) (meter.Reading, error) {
	if _, err := s.visibleAccount(ctx, p, accountID); err != nil {
		return r, err
	}
	m, err := s.q.GetMeter(ctx, db.GetMeterParams{OrgID: p.OrgID, AccountID: accountID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("loading meter: %w", err)
	}
	r.Key = key
	r.Source = meter.SourceManual
	if r.CapturedAt.IsZero() {
		r.CapturedAt = s.now().UTC().Truncate(time.Second)
	}
	if err := r.Validate(defOf(m)); err != nil {
		return r, err
	}
	if _, err := s.insertReading(ctx, s.q, p.OrgID, m.ID, r, nil); err != nil {
		return r, err
	}
	if err := audit.Write(ctx, s.q, p.Actor(), audit.Entry{
		Action:     "reading.create",
		TargetKind: "meter",
		TargetID:   accountID + "/" + key,
		After:      r,
	}); err != nil {
		return r, err
	}
	return r, nil
}

// Readings lists a meter's readings, newest first.
func (s *Service) Readings(ctx context.Context, p auth.Principal, accountID, key string, from, to time.Time, limit int) ([]db.QuotaReading, error) {
	if _, err := s.visibleAccount(ctx, p, accountID); err != nil {
		return nil, err
	}
	m, err := s.q.GetMeter(ctx, db.GetMeterParams{OrgID: p.OrgID, AccountID: accountID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading meter: %w", err)
	}
	rs, err := s.q.ListMeterReadings(ctx, db.ListMeterReadingsParams{
		OrgID: p.OrgID, MeterID: m.ID, FromTime: from, ToTime: to, RowLimit: int32(limit), //nolint:gosec // limit is validated by the handler
	})
	if err != nil {
		return nil, fmt.Errorf("listing readings: %w", err)
	}
	return rs, nil
}

func (s *Service) visibleAccount(ctx context.Context, p auth.Principal, accountID string) (db.Account, error) {
	a, err := s.q.GetVisibleAccount(ctx, db.GetVisibleAccountParams{OrgID: p.OrgID, AccountID: accountID, UserID: userIDOf(p), IsAdmin: p.IsAdmin()})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("loading account: %w", err)
	}
	return a, nil
}

// userIDOf returns a uuid for account_visible; non-user principals have
// none, and only see what admins see if their role is admin.
func userIDOf(p auth.Principal) string {
	if p.UserID == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return p.UserID
}

// MeterView is one meter with its latest reading and score.
type MeterView struct {
	Def    meter.Def
	Manual bool
	Latest *db.QuotaReading
	Score  MeterScore
}

// AccountQuota is one Quotas-page card.
type AccountQuota struct {
	Account  db.Account
	Provider provider.Info
	Meters   []MeterView
	Score    AccountScore
	inputs   []MeterInput
}

// Overview returns quota cards for the visible accounts, sorted by urgency:
// infeasible first, then lowest headroom, then soonest renewal.
func (s *Service) Overview(ctx context.Context, p auth.Principal, accountID *string) ([]AccountQuota, error) {
	now := s.now()
	accounts, err := s.q.ListVisibleQuotaAccounts(ctx, db.ListVisibleQuotaAccountsParams{
		OrgID: p.OrgID, AccountID: accountID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing accounts: %w", err)
	}
	if len(accounts) == 0 {
		return []AccountQuota{}, nil
	}
	ids := make([]string, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ID
	}
	meters, err := s.q.ListMetersForAccounts(ctx, db.ListMetersForAccountsParams{OrgID: p.OrgID, AccountIds: ids})
	if err != nil {
		return nil, fmt.Errorf("listing meters: %w", err)
	}
	meterIDs := make([]string, len(meters))
	for i, m := range meters {
		meterIDs[i] = m.ID
	}
	readings, err := s.q.ListRecentReadings(ctx, db.ListRecentReadingsParams{OrgID: p.OrgID, MeterIds: meterIDs, Since: now.Add(-7 * 24 * time.Hour)})
	if err != nil {
		return nil, fmt.Errorf("listing readings: %w", err)
	}
	byMeter := map[string][]db.QuotaReading{}
	for _, r := range readings {
		byMeter[r.MeterID] = append(byMeter[r.MeterID], r)
	}
	byAccount := map[string][]db.QuotaMeter{}
	for _, m := range meters {
		byAccount[m.AccountID] = append(byAccount[m.AccountID], m)
	}

	out := make([]AccountQuota, 0, len(accounts))
	for _, a := range accounts {
		info, ok := s.catalog(a.ProviderID)
		if !ok {
			info = provider.Info{ID: a.ProviderID, Name: a.ProviderID, Currency: a.Currency}
		}
		card := AccountQuota{Account: a, Provider: info}
		scores := make([]MeterScore, 0, len(byAccount[a.ID]))
		for _, m := range byAccount[a.ID] {
			def := defOf(m)
			rows := byMeter[m.ID]
			in := MeterInput{Def: def, Readings: make([]meter.Reading, len(rows))}
			for i, r := range rows {
				in.Readings[i] = readingOf(def.Key, r)
			}
			sc := ScoreMeter(in, now)
			view := MeterView{Def: def, Manual: m.Manual, Score: sc}
			if len(rows) > 0 {
				view.Latest = &rows[len(rows)-1]
			}
			card.Meters = append(card.Meters, view)
			card.inputs = append(card.inputs, in)
			scores = append(scores, sc)
		}
		card.Score = ScoreAccount(scores)
		out = append(out, card)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score.Feasible != b.Score.Feasible {
			return !a.Score.Feasible
		}
		if !nearlyEqual(a.Score.Headroom, b.Score.Headroom) {
			return a.Score.Headroom < b.Score.Headroom
		}
		ra, rb := a.Account.RenewsOn, b.Account.RenewsOn
		switch {
		case ra != nil && rb != nil && !ra.Equal(*rb):
			return ra.Before(*rb)
		case (ra == nil) != (rb == nil):
			return ra != nil
		}
		return a.Account.Name < b.Account.Name
	})
	return out, nil
}

// JobSize is a rough job size for recommendations.
type JobSize string

// Job sizes and the share of each limit that must remain after the horizon.
const (
	SizeSmall  JobSize = "small"
	SizeMedium JobSize = "medium"
	SizeLarge  JobSize = "large"
)

func (j JobSize) reserve() float64 {
	switch j {
	case SizeMedium:
		return 0.10
	case SizeLarge:
		return 0.30
	}
	return 0
}

// Recommendation is one ranked account with its reasons.
type Recommendation struct {
	Card              AccountQuota
	Score             AccountScore
	MarginalCostMicro int64
	CostCurrency      string
	Reasons           []string
}

// Recommend ranks visible accounts for a job.
func (s *Service) Recommend(ctx context.Context, p auth.Principal, family string, size JobSize, horizon time.Duration, limit int) ([]Recommendation, error) {
	cards, err := s.Overview(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var cands []Candidate
	recs := map[string]*Recommendation{}
	for _, c := range cards {
		if family != "" && !slices.Contains(c.Provider.ModelFamilies, family) && !slices.Contains(c.Provider.ModelFamilies, "*") {
			continue
		}
		rec := &Recommendation{Card: c, Score: c.Score, CostCurrency: "USD"}
		flat := c.Account.AuthKind == string(provider.AuthAgentCapture) || (c.Account.BillingCycle != "none" && c.Account.AuthKind == string(provider.AuthNone))
		switch {
		case flat:
			rec.Reasons = append(rec.Reasons, "flat-rate subscription: no marginal cost")
		case s.price != nil:
			if micro, ok := s.price(ctx, c.Account.ProviderID, priceFamily(family, c.Provider)); ok {
				rec.MarginalCostMicro = micro
				rec.Reasons = append(rec.Reasons, fmt.Sprintf("pay-per-use: about $%.2f per million tokens", float64(micro)/1e6))
			} else {
				rec.MarginalCostMicro = math.MaxInt64
				rec.Reasons = append(rec.Reasons, "pay-per-use: price unknown, ranked after priced accounts")
			}
		default:
			rec.MarginalCostMicro = math.MaxInt64
			rec.Reasons = append(rec.Reasons, "pay-per-use: price unknown, ranked after priced accounts")
		}
		if need := size.reserve(); need > 0 && rec.Score.Feasible {
			if short := shortfall(c.inputs, now, horizon, need); short != "" {
				rec.Score.Feasible = false
				rec.Reasons = append(rec.Reasons, short)
			}
		}
		rec.Reasons = append(rec.Reasons, meterReasons(c)...)
		recs[c.Account.ID] = rec
		cands = append(cands, Candidate{AccountID: c.Account.ID, Score: rec.Score, MarginalCostMicro: rec.MarginalCostMicro})
	}
	Rank(cands)
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]Recommendation, len(cands))
	for i, c := range cands {
		out[i] = *recs[c.AccountID]
	}
	return out, nil
}

// shortfall reports a meter that would drop below need × limit within the
// horizon at the current pace, or "" when every meter keeps enough.
func shortfall(inputs []MeterInput, now time.Time, horizon time.Duration, need float64) string {
	for _, in := range inputs {
		sc := ScoreMeter(in, now)
		if sc.Remaining == nil || sc.Limit == nil || *sc.Limit <= 0 {
			continue
		}
		b := 0.0
		if sc.BurnPerHour != nil {
			b = *sc.BurnPerHour
		}
		left := *sc.Remaining - b*horizon.Hours()
		if sc.ResetsAt != nil && sc.ResetsAt.Before(now.Add(horizon)) {
			continue // resets inside the horizon
		}
		if left < need**sc.Limit {
			return fmt.Sprintf("%s would fall below %.0f%% within %s at the current pace", in.Def.Label, need*100, roundDuration(horizon))
		}
	}
	return ""
}

func meterReasons(c AccountQuota) []string {
	var out []string
	for _, m := range c.Meters {
		sc := m.Score
		switch {
		case sc.NoData:
			continue
		case sc.Exhausted:
			when := "until it resets"
			if sc.ResetsAt != nil {
				when = "until " + sc.ResetsAt.UTC().Format("Mon 15:04 UTC")
			}
			out = append(out, fmt.Sprintf("%s: %s exhausted %s", c.Provider.Name, m.Def.Label, when))
		case sc.Stale:
			out = append(out, fmt.Sprintf("%s: %s has reset since the last reading", c.Provider.Name, m.Def.Label))
		case sc.Expiring >= 0.05 && sc.HoursToReset != nil:
			out = append(out, fmt.Sprintf("%s: %.0f%% of the %s expires in %s unused", c.Provider.Name, sc.Expiring*100, m.Def.Label, roundDuration(time.Duration(*sc.HoursToReset*float64(time.Hour)))))
		case sc.Headroom < 1 && sc.HoursToEmpty != nil:
			out = append(out, fmt.Sprintf("%s: %s runs out in about %s at the current pace", c.Provider.Name, m.Def.Label, roundDuration(time.Duration(*sc.HoursToEmpty*float64(time.Hour)))))
		}
	}
	return out
}

// Forecast projects each visible account's score hourly.
func (s *Service) Forecast(ctx context.Context, p auth.Principal, accountID *string, hours int) (map[string][]ForecastPoint, []string, error) {
	cards, err := s.Overview(ctx, p, accountID)
	if err != nil {
		return nil, nil, err
	}
	now := s.now().UTC().Truncate(time.Hour)
	out := make(map[string][]ForecastPoint, len(cards))
	order := make([]string, 0, len(cards))
	for _, c := range cards {
		out[c.Account.ID] = Forecast(c.inputs, now, hours)
		order = append(order, c.Account.ID)
	}
	return out, order, nil
}

func roundDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24+0.5))
	case d >= time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()+0.5))
	default:
		return fmt.Sprintf("%d min", int(d.Minutes()+0.5))
	}
}

func defOf(m db.QuotaMeter) meter.Def {
	d := meter.Def{Key: m.Key, Label: m.Label, Unit: meter.Unit(m.Unit), ResetKind: meter.ResetKind(m.ResetKind), Limit: m.LimitValue}
	if m.UnitCurrency != nil {
		d.UnitCurrency = *m.UnitCurrency
	}
	if m.WindowMinutes != nil {
		d.WindowMinutes = int(*m.WindowMinutes)
	}
	return d
}

func readingOf(key string, r db.QuotaReading) meter.Reading {
	return meter.Reading{Key: key, Used: r.Used, Remaining: r.Remaining, Limit: r.LimitValue, ResetsAt: r.ResetsAt, CapturedAt: r.CapturedAt, Source: meter.Source(r.Source)}
}
