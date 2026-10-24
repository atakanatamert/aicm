// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/quota"
)

func quotaError(err error) error {
	switch {
	case errors.Is(err, quota.ErrNotFound):
		return NotFound("account or meter not found")
	case errors.Is(err, quota.ErrManualConflict):
		return Conflict(err.Error())
	}
	var ve interface{ Unwrap() []error }
	if errors.As(err, &ve) {
		return BadRequest(err.Error())
	}
	return err
}

func toMeterDef(d meter.Def) MeterDef {
	out := MeterDef{Key: d.Key, Label: d.Label, Unit: MeterUnit(d.Unit), ResetKind: ResetKind(d.ResetKind), Limit: d.Limit}
	if d.UnitCurrency != "" {
		out.UnitCurrency = new(d.UnitCurrency)
	}
	if d.WindowMinutes > 0 {
		out.WindowMinutes = new(d.WindowMinutes)
	}
	if d.Supplemental {
		out.Supplemental = new(true)
	}
	return out
}

func fromMeterDef(d MeterDef) meter.Def {
	out := meter.Def{Key: d.Key, Label: d.Label, Unit: meter.Unit(d.Unit), ResetKind: meter.ResetKind(d.ResetKind), Limit: d.Limit,
		Supplemental: d.Supplemental != nil && *d.Supplemental}
	if d.UnitCurrency != nil {
		out.UnitCurrency = *d.UnitCurrency
	}
	if d.WindowMinutes != nil {
		out.WindowMinutes = *d.WindowMinutes
	}
	return out
}

func toMeterReading(r db.QuotaReading) MeterReading {
	return MeterReading{
		Used: r.Used, Remaining: r.Remaining, Limit: r.LimitValue, ResetsAt: r.ResetsAt,
		CapturedAt: r.CapturedAt, Source: ReadingSource(r.Source), MachineId: r.MachineID,
	}
}

func toAccountScore(a quota.AccountScore) AccountScore {
	return AccountScore{Feasible: a.Feasible, Headroom: a.Headroom, ExpiringShare: a.Expiring, BindingMeter: optString(a.Binding)}
}

// ListQuotas returns every visible account's meters with scores.
func (s *Server) ListQuotas(ctx context.Context, req ListQuotasRequestObject) (ListQuotasResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	cards, err := s.Quota.Overview(ctx, p, req.Params.AccountId)
	if err != nil {
		return nil, quotaError(err)
	}
	out := make(ListQuotas200JSONResponse, 0, len(cards))
	for _, c := range cards {
		aq := AccountQuota{Account: toAccountSummary(c.Account, c.Provider), Score: toAccountScore(c.Score), Meters: make([]MeterView, 0, len(c.Meters))}
		for _, m := range c.Meters {
			v := MeterView{
				Def:    toMeterDef(m.Def),
				Manual: new(m.Manual),
				Score: MeterScore{
					BurnPerHour: m.Score.BurnPerHour, HoursToEmpty: m.Score.HoursToEmpty, HoursToReset: m.Score.HoursToReset,
					Headroom: m.Score.Headroom, ExpiringShare: m.Score.Expiring, Stale: m.Score.Stale,
				},
			}
			if m.Latest != nil {
				v.Latest = new(toMeterReading(*m.Latest))
			}
			aq.Meters = append(aq.Meters, v)
		}
		out = append(out, aq)
	}
	return out, nil
}

// UpsertManualMeter declares or changes a manual meter.
func (s *Server) UpsertManualMeter(ctx context.Context, req UpsertManualMeterRequestObject) (UpsertManualMeterResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	def, err := s.Quota.UpsertManualMeter(ctx, p, req.AccountId, fromMeterDef(*req.Body))
	if err != nil {
		return nil, quotaError(err)
	}
	return UpsertManualMeter200JSONResponse(toMeterDef(def)), nil
}

// ListMeterReadings returns a meter's readings, newest first.
func (s *Server) ListMeterReadings(ctx context.Context, req ListMeterReadingsRequestObject) (ListMeterReadingsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	to := time.Now().UTC()
	if req.Params.To != nil {
		to = *req.Params.To
	}
	from := to.Add(-7 * 24 * time.Hour)
	if req.Params.From != nil {
		from = *req.Params.From
	}
	if !from.Before(to) {
		return nil, BadRequest("from must be before to")
	}
	limit := 100
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > 500 {
		return nil, BadRequest("limit must be between 1 and 500")
	}
	rs, err := s.Quota.Readings(ctx, p, req.AccountId, req.MeterKey, from, to, limit)
	if err != nil {
		return nil, quotaError(err)
	}
	out := make(ListMeterReadings200JSONResponse, len(rs))
	for i, r := range rs {
		out[i] = toMeterReading(r)
	}
	return out, nil
}

// RecordManualReading stores a reading typed in by a user.
func (s *Server) RecordManualReading(ctx context.Context, req RecordManualReadingRequestObject) (RecordManualReadingResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	b := req.Body
	r := meter.Reading{Used: b.Used, Remaining: b.Remaining, Limit: b.Limit, ResetsAt: b.ResetsAt}
	if b.CapturedAt != nil {
		r.CapturedAt = *b.CapturedAt
	}
	saved, err := s.Quota.RecordManualReading(ctx, p, req.AccountId, req.MeterKey, r)
	if err != nil {
		return nil, quotaError(err)
	}
	return RecordManualReading201JSONResponse{
		Used: saved.Used, Remaining: saved.Remaining, Limit: saved.Limit, ResetsAt: saved.ResetsAt,
		CapturedAt: saved.CapturedAt, Source: ReadingSource(saved.Source),
	}, nil
}

// RecommendAccount ranks accounts for a job.
func (s *Server) RecommendAccount(ctx context.Context, req RecommendAccountRequestObject) (RecommendAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	family, size, horizon, limit := "", quota.SizeSmall, time.Hour, 5
	if req.Params.ModelFamily != nil {
		family = *req.Params.ModelFamily
	}
	if req.Params.Size != nil {
		size = quota.JobSize(*req.Params.Size)
	}
	if h := req.Params.HorizonHours; h != nil {
		if *h < 0 || *h > 168 {
			return nil, BadRequest("horizon_hours must be between 0 and 168")
		}
		horizon = time.Duration(*h * float64(time.Hour))
	}
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > 50 {
		return nil, BadRequest("limit must be between 1 and 50")
	}
	recs, err := s.Quota.Recommend(ctx, p, family, size, horizon, limit)
	if err != nil {
		return nil, quotaError(err)
	}
	out := RecommendAccount200JSONResponse{GeneratedAt: time.Now().UTC(), Ranked: make([]RankedAccount, len(recs))}
	for i, r := range recs {
		out.Ranked[i] = RankedAccount{
			Rank:                i + 1,
			Account:             toAccountSummary(r.Card.Account, r.Card.Provider),
			Score:               toAccountScore(r.Score),
			MarginalCostPerMtok: MicroMoney{AmountMicro: r.MarginalCostMicro, Currency: r.CostCurrency},
			Reasons:             r.Reasons,
		}
	}
	return out, nil
}

// ForecastCapacity projects each account's score hour by hour.
func (s *Server) ForecastCapacity(ctx context.Context, req ForecastCapacityRequestObject) (ForecastCapacityResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	hours := 24
	if req.Params.Hours != nil {
		hours = *req.Params.Hours
	}
	if hours < 1 || hours > 168 {
		return nil, BadRequest("hours must be between 1 and 168")
	}
	series, order, err := s.Quota.Forecast(ctx, p, req.Params.AccountId, hours)
	if err != nil {
		return nil, quotaError(err)
	}
	out := make(ForecastCapacity200JSONResponse, 0, len(order))
	for _, id := range order {
		f := CapacityForecast{AccountId: id}
		for _, pt := range series[id] {
			f.Points = append(f.Points, struct {
				At       time.Time `json:"at"`
				Feasible bool      `json:"feasible"`
				Headroom float64   `json:"headroom"`
			}{At: pt.At, Feasible: pt.Feasible, Headroom: pt.Headroom})
		}
		out = append(out, f)
	}
	return out, nil
}
