// SPDX-License-Identifier: AGPL-3.0-or-later

package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/quota"
)

// alertData is stored in alerts.data so the inbox and the email can render
// the price and the provider's catalog buttons without another lookup.
type alertData struct {
	Price *money.Amount   `json:"price,omitempty"`
	Links *provider.Links `json:"links,omitempty"`
}

// candidate is one event before it is inserted.
type candidate struct {
	orgID     string
	accountID *string
	machineID *string
	kind      string
	eventKey  string
	severity  string
	title     string
	body      string
	data      alertData
}

// ruleKey identifies one stored rule. accountID is "" for org defaults.
type ruleKey struct {
	orgID     string
	accountID string
	kind      string
}

type ruleSet map[ruleKey]db.AlertRule

// get returns the account's rule when present, else the org default.
func (rs ruleSet) get(orgID, accountID, kind string) (db.AlertRule, bool) {
	if accountID != "" {
		if r, ok := rs[ruleKey{orgID: orgID, accountID: accountID, kind: kind}]; ok {
			return r, true
		}
	}
	r, ok := rs[ruleKey{orgID: orgID, kind: kind}]
	return r, ok
}

// Evaluate checks every org's rules at now and inserts new alerts. It returns
// how many were new. Duplicates (same event_key) are skipped by the database,
// so the hook runs once per event.
func (s *Service) Evaluate(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC()
	today := dayUTC(now)

	accounts, err := s.q.ListAlertAccounts(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing accounts for alerts: %w", err)
	}
	rules, err := s.q.ListAlertRules(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing alert rules: %w", err)
	}
	meters, err := s.q.ListAlertMeters(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing meters for alerts: %w", err)
	}
	readings, err := s.q.ListAlertReadings(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		return 0, fmt.Errorf("listing readings for alerts: %w", err)
	}
	machines, err := s.q.ListAlertMachines(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing machines for alerts: %w", err)
	}

	rulesByKey := make(ruleSet, len(rules))
	for _, r := range rules {
		id := ""
		if r.AccountID != nil {
			id = *r.AccountID
		}
		rulesByKey[ruleKey{orgID: r.OrgID, accountID: id, kind: r.Kind}] = r
	}
	metersByAccount := map[string][]db.QuotaMeter{}
	for _, m := range meters {
		metersByAccount[m.AccountID] = append(metersByAccount[m.AccountID], m)
	}
	readingsByMeter := map[string][]db.QuotaReading{}
	for _, r := range readings {
		readingsByMeter[r.MeterID] = append(readingsByMeter[r.MeterID], r)
	}

	var fresh []db.Alert
	emit := func(c candidate) error {
		data, err := json.Marshal(c.data)
		if err != nil {
			return fmt.Errorf("encoding alert data: %w", err)
		}
		row, err := s.q.InsertAlert(ctx, db.InsertAlertParams{
			OrgID:     c.orgID,
			AccountID: c.accountID,
			MachineID: c.machineID,
			Kind:      c.kind,
			EventKey:  c.eventKey,
			Severity:  c.severity,
			Title:     c.title,
			Body:      c.body,
			Data:      data,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // the event key already exists
		}
		if err != nil {
			return fmt.Errorf("inserting alert %q: %w", c.eventKey, err)
		}
		fresh = append(fresh, row)
		return nil
	}

	for _, a := range accounts {
		info := s.infoFor(a.ProviderID)
		name := displayName(a, info)
		links := linksOf(info)

		if r, ok := rulesByKey.get(a.OrgID, a.ID, "renewal"); ok && ruleActive(r, now) &&
			a.AutoRenew && a.RenewsOn != nil {
			p := paramsOf(r)
			for _, n := range daysList(p.DaysBefore, []int{7, 1}) {
				if daysUntil(*a.RenewsOn, today) != n {
					continue
				}
				severity := "warning"
				if n <= 1 {
					severity = "critical"
				}
				if err := emit(candidate{
					orgID:     a.OrgID,
					accountID: &a.ID,
					kind:      "renewal",
					eventKey:  fmt.Sprintf("renewal:%s:%s:%dd", a.ID, dateOf(*a.RenewsOn), n),
					severity:  severity,
					title:     fmt.Sprintf("%s renews in %s", name, dayCount(n)),
					body:      renewalBody(a, *a.RenewsOn, firstLink(info.Links.ManageSubscription, info.Links.Billing, info.Links.Cancel)),
					data:      alertData{Price: priceOf(a), Links: links},
				}); err != nil {
					return len(fresh), err
				}
			}
		}

		if r, ok := rulesByKey.get(a.OrgID, a.ID, "promo_ending"); ok && ruleActive(r, now) &&
			a.PromoEndsOn != nil {
			p := paramsOf(r)
			for _, n := range daysList(p.DaysBefore, []int{3}) {
				if daysUntil(*a.PromoEndsOn, today) != n {
					continue
				}
				if err := emit(candidate{
					orgID:     a.OrgID,
					accountID: &a.ID,
					kind:      "promo_ending",
					eventKey:  fmt.Sprintf("promo:%s:%s:%dd", a.ID, dateOf(*a.PromoEndsOn), n),
					severity:  "warning",
					title:     fmt.Sprintf("%s promo ends in %s", name, dayCount(n)),
					body:      promoBody(a, *a.PromoEndsOn, firstLink(info.Links.ManageSubscription, info.Links.Billing)),
					data:      alertData{Price: priceOf(a), Links: links},
				}); err != nil {
					return len(fresh), err
				}
			}
		}

		if r, ok := rulesByKey.get(a.OrgID, a.ID, "sync_failing"); ok && ruleActive(r, now) &&
			a.SyncFailingSince != nil {
			p := paramsOf(r)
			hours := floatOr(p.AfterHours, 24)
			if now.Sub(*a.SyncFailingSince).Hours() > hours {
				if err := emit(candidate{
					orgID:     a.OrgID,
					accountID: &a.ID,
					kind:      "sync_failing",
					eventKey:  fmt.Sprintf("sync:%s:%s", a.ID, dateOf(*a.SyncFailingSince)),
					severity:  "warning",
					title:     fmt.Sprintf("%s sync has been failing", name),
					body:      fmt.Sprintf("Sync has been failing since %s.", readableTime(*a.SyncFailingSince)),
					data:      alertData{Links: links},
				}); err != nil {
					return len(fresh), err
				}
			}
		}

		for _, m := range metersByAccount[a.ID] {
			def := defOf(m)
			rows := readingsByMeter[m.ID]
			in := quota.MeterInput{Def: def, Readings: make([]meter.Reading, len(rows))}
			for i, row := range rows {
				in.Readings[i] = readingOf(def.Key, row)
			}
			score := quota.ScoreMeter(in, now)
			latest := len(in.Readings) > 0

			if r, ok := rulesByKey.get(a.OrgID, a.ID, "low_balance"); ok && ruleActive(r, now) &&
				def.ResetKind == meter.ResetNever && def.Unit == meter.UnitCurrency && latest {
				p := paramsOf(r)
				below := floatOr(p.BelowPercent, 20)
				daysLeft := floatOr(p.DaysLeft, 7)
				low := false
				if score.Remaining != nil {
					high := highRemaining(def, rows, now.Add(-30*24*time.Hour))
					if high > 0 && *score.Remaining < high*below/100 {
						low = true
					}
					if score.HoursToEmpty != nil && *score.HoursToEmpty < daysLeft*24 {
						low = true
					}
				}
				if low {
					topUp := firstLink(info.Links.TopUp, info.Links.Billing)
					if err := emit(candidate{
						orgID:     a.OrgID,
						accountID: &a.ID,
						kind:      "low_balance",
						eventKey:  fmt.Sprintf("low_balance:%s:%s:%s", a.ID, m.Key, dateOf(now)),
						severity:  "warning",
						title:     fmt.Sprintf("%s %s is running low", name, def.Label),
						body:      lowBalanceBody(def, *score.Remaining, topUp),
						data:      alertData{Links: links},
					}); err != nil {
						return len(fresh), err
					}
				}
			}

			if r, ok := rulesByKey.get(a.OrgID, a.ID, "meter_exhausted"); ok && ruleActive(r, now) &&
				!score.Stale && !score.NoData {
				p := paramsOf(r)
				used := floatOr(p.UsedPercent, 90)
				if pct, ok := usedPercent(def, in.Readings, score); ok && pct >= used {
					severity := "warning"
					if pct >= 100 {
						severity = "critical"
					}
					window := "none"
					if score.ResetsAt != nil {
						window = score.ResetsAt.UTC().Format(time.RFC3339)
					}
					reset := "Resets at an unknown time."
					if score.ResetsAt != nil {
						reset = "Resets " + untilReset(*score.ResetsAt, now) + "."
					}
					if err := emit(candidate{
						orgID:     a.OrgID,
						accountID: &a.ID,
						kind:      "meter_exhausted",
						eventKey:  fmt.Sprintf("meter:%s:%s:%s", a.ID, m.Key, window),
						severity:  severity,
						title:     fmt.Sprintf("%s %s is %d%% used", name, def.Label, int(math.Round(pct))),
						body:      reset,
						data:      alertData{Links: links},
					}); err != nil {
						return len(fresh), err
					}
				}
			}

			if r, ok := rulesByKey.get(a.OrgID, a.ID, "quota_waste"); ok && ruleActive(r, now) {
				p := paramsOf(r)
				unused := floatOr(p.UnusedPercent, 50)
				hours := floatOr(p.HoursBeforeReset, 12)
				if score.ResetsAt != nil && score.HoursToReset != nil &&
					score.Expiring >= unused/100 && *score.HoursToReset <= hours {
					if err := emit(candidate{
						orgID:     a.OrgID,
						accountID: &a.ID,
						kind:      "quota_waste",
						eventKey:  fmt.Sprintf("waste:%s:%s:%s", a.ID, m.Key, score.ResetsAt.UTC().Format(time.RFC3339)),
						severity:  "info",
						title:     fmt.Sprintf("%s %s: %d%% unused before reset", name, def.Label, int(math.Round(score.Expiring*100))),
						body:      fmt.Sprintf("Resets %s with quota left. Use it or lose it.", untilReset(*score.ResetsAt, now)),
						data:      alertData{Links: links},
					}); err != nil {
						return len(fresh), err
					}
				}
			}
		}
	}

	for _, m := range machines {
		r, ok := rulesByKey.get(m.OrgID, "", "agent_offline")
		if !ok || !ruleActive(r, now) || m.LastSeenAt == nil {
			continue
		}
		hours := floatOr(paramsOf(r).AfterHours, 48)
		if now.Sub(*m.LastSeenAt).Hours() <= hours {
			continue
		}
		if err := emit(candidate{
			orgID:     m.OrgID,
			machineID: &m.ID,
			kind:      "agent_offline",
			eventKey:  fmt.Sprintf("offline:%s:%s", m.ID, dateOf(*m.LastSeenAt)),
			severity:  "warning",
			title:     fmt.Sprintf("%s is offline", m.Name),
			body:      fmt.Sprintf("Last seen %s.", readableTime(*m.LastSeenAt)),
		}); err != nil {
			return len(fresh), err
		}
	}

	for _, a := range fresh {
		if s.hook == nil {
			break
		}
		if err := s.hook(ctx, NewAlert{Alert: a}); err != nil && s.log != nil {
			s.log.WarnContext(ctx, "alert hook failed", "alert_id", a.ID, "kind", a.Kind, "err", err)
		}
	}
	return len(fresh), nil
}

func ruleActive(r db.AlertRule, now time.Time) bool {
	return r.Enabled && (r.SnoozedUntil == nil || !r.SnoozedUntil.After(now))
}

func paramsOf(r db.AlertRule) RuleParams {
	var p RuleParams
	if len(r.Params) > 0 {
		_ = json.Unmarshal(r.Params, &p)
	}
	return p
}

func (s *Service) infoFor(providerID string) provider.Info {
	for _, p := range s.providers {
		if info := p.Info(); info.ID == providerID {
			return info
		}
	}
	return provider.Info{ID: providerID, Name: providerID}
}

func displayName(a db.Account, info provider.Info) string {
	if a.Name != "" {
		return a.Name
	}
	if info.Name != "" {
		return info.Name
	}
	return a.ProviderID
}

// readableTime formats t for alert text read in the inbox and in email,
// where the reader's time zone is unknown: "Oct 18, 17:07 UTC".
func readableTime(t time.Time) string {
	return t.UTC().Format("Jan 2, 15:04 UTC")
}

// untilReset reads "in 4h 10m (Oct 18, 17:07 UTC)", or "now" when the reset
// has already passed.
func untilReset(resetsAt, now time.Time) string {
	d := resetsAt.Sub(now).Round(time.Minute)
	if d <= 0 {
		return "now"
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	rel := fmt.Sprintf("%dm", m)
	switch {
	case h >= 48:
		rel = fmt.Sprintf("%dd", h/24)
	case h > 0:
		rel = fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("in %s (%s)", rel, readableTime(resetsAt))
}

func linksOf(info provider.Info) *provider.Links {
	l := info.Links
	if l.TopUp == "" && l.Billing == "" && l.ManageSubscription == "" && l.Cancel == "" &&
		l.Usage == "" && l.Keys == "" && l.Docs == "" {
		return nil
	}
	return &l
}

func priceOf(a db.Account) *money.Amount {
	if a.PriceMinor == nil {
		return nil
	}
	return &money.Amount{Minor: *a.PriceMinor, Currency: a.Currency}
}

func renewalBody(a db.Account, on time.Time, link string) string {
	var b strings.Builder
	if a.PriceMinor != nil {
		fmt.Fprintf(&b, "%s on %s.", money.Amount{Minor: *a.PriceMinor, Currency: a.Currency}.String(), dateOf(on))
	} else {
		fmt.Fprintf(&b, "Renews on %s.", dateOf(on))
	}
	if link != "" {
		fmt.Fprintf(&b, " Manage: %s", link)
	}
	return b.String()
}

func promoLabel(a db.Account) string {
	if a.PromoKind != nil {
		return *a.PromoKind + " on " + a.Name
	}
	return a.Name
}

func promoBody(a db.Account, on time.Time, link string) string {
	body := fmt.Sprintf("%s ends on %s.", promoLabel(a), dateOf(on))
	if link != "" {
		body += " Manage: " + link
	}
	return body
}

func lowBalanceBody(def meter.Def, remaining float64, link string) string {
	body := formatValue(def, remaining) + " remaining."
	if link != "" {
		body += " Top up: " + link
	}
	return body
}

func firstLink(links ...string) string {
	for _, l := range links {
		if l != "" {
			return l
		}
	}
	return ""
}

func daysList(got []int, fallback []int) []int {
	if len(got) > 0 {
		return got
	}
	return fallback
}

func floatOr(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

func dayCount(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func dateOf(t time.Time) string {
	return t.UTC().Format(time.DateOnly)
}

func daysUntil(t, today time.Time) int {
	return int(dayUTC(t).Sub(dayUTC(today)).Hours() / 24)
}

func defOf(m db.QuotaMeter) meter.Def {
	d := meter.Def{
		Key:       m.Key,
		Label:     m.Label,
		Unit:      meter.Unit(m.Unit),
		ResetKind: meter.ResetKind(m.ResetKind),
		Limit:     m.LimitValue,
	}
	if m.UnitCurrency != nil {
		d.UnitCurrency = *m.UnitCurrency
	}
	if m.WindowMinutes != nil {
		d.WindowMinutes = int(*m.WindowMinutes)
	}
	return d
}

func readingOf(key string, r db.QuotaReading) meter.Reading {
	return meter.Reading{
		Key:        key,
		Used:       r.Used,
		Remaining:  r.Remaining,
		Limit:      r.LimitValue,
		ResetsAt:   r.ResetsAt,
		CapturedAt: r.CapturedAt,
		Source:     meter.Source(r.Source),
	}
}

func limitOf(d meter.Def, r db.QuotaReading) *float64 {
	switch {
	case r.LimitValue != nil:
		return r.LimitValue
	case d.Limit != nil:
		return d.Limit
	case d.Unit == meter.UnitPercent:
		return new(100.0)
	}
	return nil
}

func remainingOf(d meter.Def, r db.QuotaReading) *float64 {
	if r.Remaining != nil {
		return r.Remaining
	}
	if l := limitOf(d, r); l != nil && r.Used != nil {
		return new(*l - *r.Used)
	}
	return nil
}

// highRemaining is the wallet's highest remaining value inside the lookback.
func highRemaining(def meter.Def, rows []db.QuotaReading, since time.Time) float64 {
	high := 0.0
	for _, r := range rows {
		if r.CapturedAt.Before(since) {
			continue
		}
		if v := remainingOf(def, r); v != nil && *v > high {
			high = *v
		}
	}
	return high
}

func usedPercent(def meter.Def, readings []meter.Reading, score quota.MeterScore) (float64, bool) {
	if len(readings) == 0 {
		return 0, false
	}
	latest := readings[len(readings)-1]
	if def.Unit == meter.UnitPercent {
		// Percent meters report the used share directly; compare it to the
		// threshold rather than deriving it from a possible limit override.
		if latest.Used != nil {
			return *latest.Used, true
		}
		if latest.Remaining != nil {
			return 100 - *latest.Remaining, true
		}
		return 0, false
	}
	if score.Limit != nil && *score.Limit > 0 && score.Remaining != nil {
		return (*score.Limit - *score.Remaining) / *score.Limit * 100, true
	}
	return 0, false
}

func formatValue(def meter.Def, v float64) string {
	switch def.Unit {
	case meter.UnitCurrency:
		return money.Amount{Minor: int64(math.Round(v)), Currency: def.UnitCurrency}.String()
	case meter.UnitPercent:
		return fmt.Sprintf("%.0f%%", v)
	default:
		return fmt.Sprintf("%.0f %s", v, def.Unit)
	}
}
