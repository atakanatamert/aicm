// SPDX-License-Identifier: SUL-1.0

// Package fx stores one EUR-based reference rate per currency per day and
// converts money amounts at the rate of the amount's own date.
//
// Rates are global data (fx_rates has no org_id): one row per source, base,
// quote and day. The built-in sources are Frankfurter (default) and the ECB.
package fx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
)

// baseCurrency is the implicit base of every built-in source. A rate of 1
// baseCurrency = Rate Quote.
const baseCurrency = "EUR"

// rateWindowDays is how far before the requested date a rate may be and
// still apply (weekends and holidays).
const rateWindowDays = 7

// Rate is one daily reference rate: 1 Base = Rate Quote.
type Rate struct {
	Date  time.Time // midnight UTC
	Base  string    // always "EUR" from the built-in sources
	Quote string
	Rate  float64
}

// RateSource fetches daily reference rates.
type RateSource interface {
	Name() string // "frankfurter" or "ecb", matches fx_rates.source
	// FetchRange returns every published rate for days in [from, to]
	// (inclusive, UTC dates). Weekends and holidays have no rows.
	FetchRange(ctx context.Context, from, to time.Time) ([]Rate, error)
}

// ErrNoRate means no rate exists for the currency within 7 days before the
// date.
var ErrNoRate = errors.New("no exchange rate for that date")

// Service stores rates and converts amounts. All methods are safe for
// concurrent use.
type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	sources []RateSource
	log     *slog.Logger
}

// NewService returns a service that reads and writes rates through pool and
// fetches them from sources, in order of preference.
func NewService(pool *pgxpool.Pool, log *slog.Logger, sources ...RateSource) *Service {
	return &Service{pool: pool, q: db.New(pool), sources: sources, log: log}
}

// Convert converts a to currency to at the rate for on (a calendar day): the
// newest stored rate with rate_date <= on and rate_date >= on-7 days.
// preferred is the org's fx_source; when it has no rate for the pair the
// other source is tried. Same currency returns a unchanged. EUR is the
// implicit base (rate 1).
//
// The returned time is the rate date actually used: the older of the two
// currencies' rate dates. For a same-currency conversion it is on.
func (s *Service) Convert(ctx context.Context, a money.Amount, to string, on time.Time, preferred string) (money.Amount, time.Time, error) {
	if !money.ValidCurrency(a.Currency) {
		return money.Amount{}, time.Time{}, fmt.Errorf("invalid currency %q", a.Currency)
	}
	if !money.ValidCurrency(to) {
		return money.Amount{}, time.Time{}, fmt.Errorf("invalid currency %q", to)
	}
	on = day(on)
	if a.Currency == to {
		return a, on, nil
	}

	fromRate, fromDate, toRate, toDate, err := s.lookupPair(ctx, a.Currency, to, on, preferred)
	if err != nil {
		return money.Amount{}, time.Time{}, err
	}
	expFrom, err := money.Exponent(a.Currency)
	if err != nil {
		return money.Amount{}, time.Time{}, fmt.Errorf("currency exponent for %s: %w", a.Currency, err)
	}
	expTo, err := money.Exponent(to)
	if err != nil {
		return money.Amount{}, time.Time{}, fmt.Errorf("currency exponent for %s: %w", to, err)
	}
	minor, err := convertMinor(a.Minor, expFrom, expTo, fromRate, toRate)
	if err != nil {
		return money.Amount{}, time.Time{}, err
	}
	used := fromDate
	if toDate.Before(used) {
		used = toDate
	}
	return money.Amount{Minor: minor, Currency: to}, used, nil
}

// Refresh fetches [from, to] from every source and upserts the rows. A
// failing source is logged and skipped; Refresh fails only when every source
// fails.
func (s *Service) Refresh(ctx context.Context, from, to time.Time) error {
	from, to = day(from), day(to)
	if to.Before(from) {
		return errors.New("fx refresh: range ends before it starts")
	}
	if len(s.sources) == 0 {
		return errors.New("fx refresh: no rate sources configured")
	}
	failed := 0
	for _, src := range s.sources {
		rates, err := src.FetchRange(ctx, from, to)
		if err != nil {
			failed++
			s.log.WarnContext(ctx, "fx source failed", "source", src.Name(), "from", from.Format(time.DateOnly), "to", to.Format(time.DateOnly), "err", err)
			continue
		}
		for _, r := range rates {
			err := s.q.UpsertRate(ctx, db.UpsertRateParams{
				RateDate: day(r.Date),
				Base:     r.Base,
				Quote:    r.Quote,
				Rate:     r.Rate,
				Source:   src.Name(),
			})
			if err != nil {
				return fmt.Errorf("saving %s rate for %s on %s: %w", src.Name(), r.Quote, day(r.Date).Format(time.DateOnly), err)
			}
		}
		s.log.InfoContext(ctx, "fx rates refreshed", "source", src.Name(), "rates", len(rates), "from", from.Format(time.DateOnly), "to", to.Format(time.DateOnly))
	}
	if failed == len(s.sources) {
		return fmt.Errorf("fx refresh: all %d sources failed", failed)
	}
	return nil
}

// lookupPair resolves both currencies from the same source, trying the
// preferred source first and then the remaining ones in order.
func (s *Service) lookupPair(ctx context.Context, from, to string, on time.Time, preferred string) (fromRate float64, fromDate time.Time, toRate float64, toDate time.Time, err error) {
	for _, src := range s.ordered(preferred) {
		fr, fd, ok, err := s.lookup(ctx, src.Name(), from, on)
		if err != nil {
			return 0, time.Time{}, 0, time.Time{}, err
		}
		if !ok {
			continue
		}
		tr, td, ok, err := s.lookup(ctx, src.Name(), to, on)
		if err != nil {
			return 0, time.Time{}, 0, time.Time{}, err
		}
		if !ok {
			continue
		}
		return fr, fd, tr, td, nil
	}
	return 0, time.Time{}, 0, time.Time{}, ErrNoRate
}

// lookup returns the newest rate for currency from source within the window
// before on. EUR is 1 at on. The bool is false when no rate exists.
func (s *Service) lookup(ctx context.Context, source, currency string, on time.Time) (float64, time.Time, bool, error) {
	if currency == baseCurrency {
		return 1, on, true, nil
	}
	row, err := s.q.RateOnOrBefore(ctx, db.RateOnOrBeforeParams{
		Source:   source,
		Quote:    currency,
		AsOf:     on,
		Earliest: on.AddDate(0, 0, -rateWindowDays),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf("loading %s rate for %s: %w", source, currency, err)
	}
	return row.Rate, day(row.RateDate), true, nil
}

// ordered returns the sources with preferred first, then the rest in
// construction order.
func (s *Service) ordered(preferred string) []RateSource {
	if preferred == "" {
		return s.sources
	}
	out := make([]RateSource, 0, len(s.sources))
	for _, src := range s.sources {
		if src.Name() == preferred {
			out = append(out, src)
		}
	}
	for _, src := range s.sources {
		if src.Name() != preferred {
			out = append(out, src)
		}
	}
	return out
}

// convertMinor applies
//
//	minor_to = minor_from × 10^(exp_to − exp_from) × rTo / rFrom
//
// in exact rational arithmetic and rounds half away from zero.
func convertMinor(minor int64, expFrom, expTo int, rFrom, rTo float64) (int64, error) {
	rFromRat, err := rateRat(rFrom)
	if err != nil {
		return 0, err
	}
	rToRat, err := rateRat(rTo)
	if err != nil {
		return 0, err
	}
	v := new(big.Rat).SetInt64(minor)
	v.Mul(v, pow10(expTo-expFrom))
	v.Mul(v, rToRat)
	v.Quo(v, rFromRat)
	return roundHalfAway(v)
}

func rateRat(rate float64) (*big.Rat, error) {
	r := new(big.Rat).SetFloat64(rate)
	if r == nil || rate <= 0 {
		return nil, fmt.Errorf("invalid rate %v", rate)
	}
	return r, nil
}

// pow10 returns 10^n as a rational.
func pow10(n int) *big.Rat {
	if n >= 0 {
		return new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil))
	}
	return new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-n)), nil))
}

// roundHalfAway rounds r to the nearest integer, halves away from zero.
func roundHalfAway(r *big.Rat) (int64, error) {
	num, den := r.Num(), r.Denom() // den is always positive
	q, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	twiceRem := new(big.Int).Lsh(new(big.Int).Abs(rem), 1)
	if twiceRem.Cmp(den) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("converted amount %s overflows int64", q)
	}
	return q.Int64(), nil
}

// day truncates t to midnight UTC.
func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// sortRates orders rates by date, then quote, for deterministic output.
func sortRates(rates []Rate) {
	sort.Slice(rates, func(i, j int) bool {
		if !rates[i].Date.Equal(rates[j].Date) {
			return rates[i].Date.Before(rates[j].Date)
		}
		return rates[i].Quote < rates[j].Quote
	})
}
