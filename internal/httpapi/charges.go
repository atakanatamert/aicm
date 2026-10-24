// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/charge"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
)

func chargeError(err error) error {
	switch {
	case errors.Is(err, charge.ErrNotFound):
		return NotFound("charge not found")
	case errors.Is(err, charge.ErrBadCursor):
		return BadRequest("invalid cursor")
	case errors.Is(err, charge.ErrConflict):
		return Conflict(err.Error())
	case errors.Is(err, charge.ErrInvalid):
		return BadRequest(err.Error())
	}
	return err
}

// chargeSettings returns the org's display currency and FX source.
func (s *Server) chargeSettings(ctx context.Context, p auth.Principal) (db.OrgSetting, error) {
	_, _, settings, err := s.Auth.Me(ctx, p)
	if err != nil {
		return db.OrgSetting{}, mapAuthError(err)
	}
	return settings, nil
}

// toCharge maps one stored charge to the API shape, converting at the
// charge's own date when a rate exists.
func (s *Server) toCharge(ctx context.Context, c db.Charge, accountName, providerID string, settings db.OrgSetting) (Charge, error) {
	out := Charge{
		Id:          c.ID,
		AccountId:   c.AccountID,
		AccountName: &accountName,
		ProviderId:  providerID,
		Kind:        ChargeKind(c.Kind),
		Status:      ChargeStatus(c.Status),
		Source:      ChargeSource(c.Source),
		Amount:      Money{AmountMinor: c.AmountMinor, Currency: c.Currency},
		OccurredOn:  openapi_types.Date{Time: c.OccurredOn},
		PeriodStart: toDate(c.PeriodStart),
		PeriodEnd:   toDate(c.PeriodEnd),
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
	}
	if s.FX != nil && money.ValidCurrency(settings.DisplayCurrency) {
		amount := money.Amount{Minor: c.AmountMinor, Currency: c.Currency}
		converted, rateDate, err := s.FX.Convert(ctx, amount, settings.DisplayCurrency, c.OccurredOn, settings.FxSource)
		switch {
		case err == nil:
			out.Converted = &ConvertedMoney{
				AmountMinor: converted.Minor,
				Currency:    converted.Currency,
				RateDate:    openapi_types.Date{Time: rateDate},
			}
		case !errors.Is(err, fx.ErrNoRate):
			return Charge{}, err
		}
	}
	return out, nil
}

// ListCharges returns visible charges newest first.
func (s *Server) ListCharges(ctx context.Context, req ListChargesRequestObject) (ListChargesResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := s.chargeSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	f := charge.ListFilter{}
	if req.Params.From != nil {
		f.From = &req.Params.From.Time
	}
	if req.Params.To != nil {
		f.To = &req.Params.To.Time
	}
	if req.Params.AccountId != nil {
		f.AccountID = *req.Params.AccountId
	}
	if req.Params.Kind != nil {
		f.Kind = string(*req.Params.Kind)
	}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	if req.Params.Cursor != nil {
		f.Cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	rows, next, err := s.Charges.List(ctx, p, f)
	if err != nil {
		return nil, chargeError(err)
	}
	page := ChargePage{Items: make([]Charge, 0, len(rows))}
	for _, row := range rows {
		item, err := s.toCharge(ctx, db.Charge{
			ID: row.ID, OrgID: row.OrgID, AccountID: row.AccountID, Kind: row.Kind,
			Status: row.Status, Source: row.Source, AmountMinor: row.AmountMinor,
			Currency: row.Currency, OccurredOn: row.OccurredOn, PeriodStart: row.PeriodStart,
			PeriodEnd: row.PeriodEnd, ExternalID: row.ExternalID, Description: row.Description,
			CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}, row.AccountName, row.ProviderID, settings)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, item)
	}
	if next != "" {
		page.NextCursor = &next
	}
	return ListCharges200JSONResponse(page), nil
}

// CreateCharge records a payment by hand.
func (s *Server) CreateCharge(ctx context.Context, req CreateChargeRequestObject) (CreateChargeResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	settings, err := s.chargeSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	in := charge.CreateInput{
		AccountID:  req.Body.AccountId,
		Kind:       string(req.Body.Kind),
		Amount:     money.Amount{Minor: req.Body.Amount.AmountMinor, Currency: req.Body.Amount.Currency},
		OccurredOn: req.Body.OccurredOn.Time,
	}
	if req.Body.PeriodStart != nil {
		in.PeriodStart = &req.Body.PeriodStart.Time
	}
	if req.Body.PeriodEnd != nil {
		in.PeriodEnd = &req.Body.PeriodEnd.Time
	}
	if req.Body.Description != nil {
		in.Description = *req.Body.Description
	}
	created, err := s.Charges.Create(ctx, p, in)
	if err != nil {
		return nil, chargeError(err)
	}
	account, err := s.Accounts.Get(ctx, p, created.AccountID)
	if err != nil {
		return nil, accountError(err)
	}
	resp, err := s.toCharge(ctx, created, account.Name, account.ProviderID, settings)
	if err != nil {
		return nil, err
	}
	return CreateCharge201JSONResponse(resp), nil
}

// UpdateCharge edits a charge or confirms/dismisses a suggested one.
func (s *Server) UpdateCharge(ctx context.Context, req UpdateChargeRequestObject) (UpdateChargeResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	settings, err := s.chargeSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	in := charge.UpdateInput{Description: req.Body.Description}
	if req.Body.Status != nil {
		in.Status = new(string(*req.Body.Status))
	}
	if req.Body.Amount != nil {
		in.Amount = &money.Amount{Minor: req.Body.Amount.AmountMinor, Currency: req.Body.Amount.Currency}
	}
	if req.Body.OccurredOn != nil {
		in.OccurredOn = &req.Body.OccurredOn.Time
	}
	updated, err := s.Charges.Update(ctx, p, req.ChargeId, in)
	if err != nil {
		return nil, chargeError(err)
	}
	account, err := s.Accounts.Get(ctx, p, updated.AccountID)
	if err != nil {
		return nil, accountError(err)
	}
	resp, err := s.toCharge(ctx, updated, account.Name, account.ProviderID, settings)
	if err != nil {
		return nil, err
	}
	return UpdateCharge200JSONResponse(resp), nil
}

// GetSpend reports money out per period and provider.
func (s *Server) GetSpend(ctx context.Context, req GetSpendRequestObject) (GetSpendResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := s.chargeSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	period := string(req.Params.Period)
	if period == "" {
		period = "month"
	}
	groupBy := "provider"
	if req.Params.GroupBy != nil {
		groupBy = string(*req.Params.GroupBy)
	}
	currency := settings.DisplayCurrency
	if req.Params.Currency != nil {
		currency = *req.Params.Currency
	}
	now := utcDay(time.Now())
	from := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	to := now
	if req.Params.From != nil {
		from = req.Params.From.Time
	}
	if req.Params.To != nil {
		to = req.Params.To.Time
	}
	report, err := s.Charges.Spend(ctx, charge.SpendRequest{
		OrgID: p.OrgID, UserID: p.UserID, IsAdmin: p.IsAdmin(),
		Period: period, From: from, To: to, GroupBy: groupBy,
		Currency: currency, FXSource: settings.FxSource,
	})
	if err != nil {
		return nil, chargeError(err)
	}
	out := SpendReport{DisplayCurrency: report.DisplayCurrency, Rows: make([]SpendRow, 0, len(report.Rows)), Totals: make([]SpendTotal, 0, len(report.Totals))}
	for _, row := range report.Rows {
		item := SpendRow{
			Period: row.Period, ProviderId: row.ProviderID,
			AccountId: row.AccountID, AccountName: row.AccountName,
			Billed:           toMoney(row.Billed),
			UnconvertedCount: new(row.UnconvertedCount),
		}
		if row.Converted != nil {
			item.Converted = &Money{AmountMinor: row.Converted.Minor, Currency: row.Converted.Currency}
		}
		out.Rows = append(out.Rows, item)
	}
	for _, total := range report.Totals {
		item := SpendTotal{Period: total.Period, Billed: toMoney(total.Billed)}
		if total.Converted != nil {
			item.Converted = &Money{AmountMinor: total.Converted.Minor, Currency: total.Converted.Currency}
		}
		out.Totals = append(out.Totals, item)
	}
	return GetSpend200JSONResponse(out), nil
}

// chargesCSVResponse writes the CSV with the attachment headers the generated
// response type does not set.
type chargesCSVResponse struct {
	data []byte
}

func (r chargesCSVResponse) VisitExportChargesResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="charges.csv"`)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(r.data); err != nil {
		return fmt.Errorf("writing charges csv: %w", err)
	}
	return nil
}

// ExportCharges streams every matching charge as CSV.
func (s *Server) ExportCharges(ctx context.Context, req ExportChargesRequestObject) (ExportChargesResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := s.chargeSettings(ctx, p)
	if err != nil {
		return nil, err
	}
	f := charge.ListFilter{}
	if req.Params.From != nil {
		f.From = &req.Params.From.Time
	}
	if req.Params.To != nil {
		f.To = &req.Params.To.Time
	}
	if req.Params.AccountId != nil {
		f.AccountID = *req.Params.AccountId
	}
	if req.Params.Kind != nil {
		f.Kind = string(*req.Params.Kind)
	}
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	rows, err := s.Charges.ExportRows(ctx, p, f)
	if err != nil {
		return nil, chargeError(err)
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.Write([]string{
		"id", "occurred_on", "account_id", "account_name", "provider_id", "kind",
		"status", "source", "amount", "currency", "converted_amount",
		"converted_currency", "rate_date", "description",
	}); err != nil {
		return nil, fmt.Errorf("writing charges csv header: %w", err)
	}
	for _, row := range rows {
		amount := money.Amount{Minor: row.AmountMinor, Currency: row.Currency}
		convertedAmount, convertedCurrency, rateDate := "", "", ""
		if s.FX != nil && money.ValidCurrency(settings.DisplayCurrency) {
			converted, used, err := s.FX.Convert(ctx, amount, settings.DisplayCurrency, row.OccurredOn, settings.FxSource)
			switch {
			case err == nil:
				convertedAmount = converted.Decimal()
				convertedCurrency = converted.Currency
				rateDate = used.Format(time.DateOnly)
			case !errors.Is(err, fx.ErrNoRate):
				return nil, err
			}
		}
		if err := cw.Write([]string{
			row.ID, row.OccurredOn.Format(time.DateOnly), row.AccountID, row.AccountName,
			row.ProviderID, row.Kind, row.Status, row.Source, amount.Decimal(),
			row.Currency, convertedAmount, convertedCurrency, rateDate, row.Description,
		}); err != nil {
			return nil, fmt.Errorf("writing charges csv row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return nil, fmt.Errorf("flushing charges csv: %w", err)
	}
	return chargesCSVResponse{data: buf.Bytes()}, nil
}

func toMoney(amounts []money.Amount) []Money {
	out := make([]Money, len(amounts))
	for i, a := range amounts {
		out[i] = Money{AmountMinor: a.Minor, Currency: a.Currency}
	}
	return out
}
