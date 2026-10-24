// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
)

type usageCSVResponse struct {
	rows []db.ExportUsageRowsRow
}

func (r usageCSVResponse) VisitExportUsageResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="usage.csv"`)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"day", "account_id", "account_name", "provider_id", "model", "project", "source",
		"requests", "input_tokens", "output_tokens", "cache_read_tokens",
		"cache_write_tokens", "reasoning_tokens", "cost", "cost_currency",
		"cost_reported",
	}); err != nil {
		return fmt.Errorf("writing usage csv header: %w", err)
	}
	for _, row := range r.rows {
		if err := cw.Write([]string{
			row.Day.Format(time.DateOnly), row.AccountID, row.AccountName,
			row.ProviderID, row.Model, row.Project, row.Source,
			strconv.FormatInt(row.Requests, 10), strconv.FormatInt(row.InputTokens, 10),
			strconv.FormatInt(row.OutputTokens, 10), strconv.FormatInt(row.CacheReadTokens, 10),
			strconv.FormatInt(row.CacheWriteTokens, 10), strconv.FormatInt(row.ReasoningTokens, 10),
			microDecimal(row.CostMicro), row.CostCurrency, strconv.FormatBool(row.CostReported),
		}); err != nil {
			return fmt.Errorf("writing usage csv row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing usage csv: %w", err)
	}
	return nil
}

// ExportUsage writes usage_daily rows as CSV, with the same filters as
// GET /api/usage.
func (s *Server) ExportUsage(ctx context.Context, req ExportUsageRequestObject) (ExportUsageResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := utcDay(now)
	if req.Params.From != nil {
		from = utcDay(req.Params.From.Time)
	}
	if req.Params.To != nil {
		to = utcDay(req.Params.To.Time)
	}
	if from.After(to) {
		return nil, BadRequest("from must not be after to")
	}
	if to.Sub(from) > 400*24*time.Hour {
		return nil, BadRequest("range must be at most 400 days")
	}

	q, err := s.exports()
	if err != nil {
		return nil, err
	}
	rows, err := q.ExportUsageRows(ctx, db.ExportUsageRowsParams{
		OrgID:      p.OrgID,
		UserID:     p.UserID,
		IsAdmin:    p.IsAdmin(),
		FromDay:    from,
		ToDay:      to,
		AccountID:  req.Params.AccountId,
		ProviderID: req.Params.ProviderId,
	})
	if err != nil {
		return nil, fmt.Errorf("exporting usage: %w", err)
	}
	return usageCSVResponse{rows: rows}, nil
}

type readingsCSVResponse struct {
	rows []db.ExportReadingRowsRow
}

func (r readingsCSVResponse) VisitExportReadingsResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="readings.csv"`)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"captured_at", "account_id", "account_name", "provider_id",
		"meter_key", "meter_label", "unit", "unit_currency", "used",
		"remaining", "limit", "resets_at", "source",
	}); err != nil {
		return fmt.Errorf("writing readings csv header: %w", err)
	}
	for _, row := range r.rows {
		currency := ""
		if row.UnitCurrency != nil {
			currency = *row.UnitCurrency
		}
		reset := ""
		if row.ResetsAt != nil {
			reset = row.ResetsAt.UTC().Format(time.RFC3339)
		}
		if err := cw.Write([]string{
			row.CapturedAt.UTC().Format(time.RFC3339), row.AccountID,
			row.AccountName, row.ProviderID, row.MeterKey, row.MeterLabel,
			row.Unit, currency,
			readingValue(row.Used, row.Unit, currency),
			readingValue(row.Remaining, row.Unit, currency),
			readingValue(row.LimitValue, row.Unit, currency),
			reset, row.Source,
		}); err != nil {
			return fmt.Errorf("writing readings csv row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing readings csv: %w", err)
	}
	return nil
}

// ExportReadings writes quota readings as CSV, with the same filters as the
// quota views.
func (s *Server) ExportReadings(ctx context.Context, req ExportReadingsRequestObject) (ExportReadingsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from := now.Add(-30 * 24 * time.Hour)
	to := now
	if req.Params.From != nil {
		from = req.Params.From.UTC()
	}
	if req.Params.To != nil {
		to = req.Params.To.UTC()
	}
	if from.After(to) {
		return nil, BadRequest("from must not be after to")
	}

	q, err := s.exports()
	if err != nil {
		return nil, err
	}
	rows, err := q.ExportReadingRows(ctx, db.ExportReadingRowsParams{
		OrgID:     p.OrgID,
		UserID:    p.UserID,
		IsAdmin:   p.IsAdmin(),
		FromTime:  from,
		ToTime:    to,
		AccountID: req.Params.AccountId,
	})
	if err != nil {
		return nil, fmt.Errorf("exporting readings: %w", err)
	}
	return readingsCSVResponse{rows: rows}, nil
}

// exports returns the generated queries. The pool satisfies db.DBTX.
func (s *Server) exports() (*db.Queries, error) {
	conn, ok := s.DB.(db.DBTX)
	if !ok {
		return nil, fmt.Errorf("database does not support queries")
	}
	return db.New(conn), nil
}

// microDecimal formats micro units with six fraction digits using integer
// math: 375 is "0.000375".
func microDecimal(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	frac := strconv.FormatInt(v%1_000_000, 10)
	for len(frac) < money.MicroDigits {
		frac = "0" + frac
	}
	return sign + strconv.FormatInt(v/1_000_000, 10) + "." + frac
}

// readingValue formats one meter value. Currency meters carry minor units of
// their own currency; every other meter keeps the provider's number.
func readingValue(v *float64, unit, currency string) string {
	if v == nil {
		return ""
	}
	if unit == "currency" && currency != "" {
		return money.Amount{Minor: int64(*v), Currency: currency}.Decimal()
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}
