// SPDX-License-Identifier: AGPL-3.0-or-later

package fx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

// frankfurterBaseURL is the Frankfurter v1 API root.
const frankfurterBaseURL = "https://api.frankfurter.dev/v1"

// frankfurter is the default source: daily EUR-based rates from the
// Frankfurter API, which republishes ECB data.
type frankfurter struct {
	c *provider.HTTPClient
}

// NewFrankfurter returns a RateSource that reads api.frankfurter.dev.
func NewFrankfurter(c *provider.HTTPClient) RateSource {
	return &frankfurter{c: c}
}

func (f *frankfurter) Name() string { return "frankfurter" }

// FetchRange fetches {base}/{from}..{to}?base=EUR. The range form is also
// used for a single day.
func (f *frankfurter) FetchRange(ctx context.Context, from, to time.Time) ([]Rate, error) {
	from, to = day(from), day(to)
	if to.Before(from) {
		return nil, errors.New("frankfurter: range ends before it starts")
	}
	url := fmt.Sprintf("%s/%s..%s?base=%s",
		frankfurterBaseURL, from.Format(time.DateOnly), to.Format(time.DateOnly), baseCurrency)
	var body struct {
		Rates map[string]map[string]float64 `json:"rates"`
	}
	if err := f.c.GetJSON(ctx, url, nil, &body); err != nil {
		return nil, fmt.Errorf("fetching frankfurter rates: %w", err)
	}

	rates := make([]Rate, 0, len(body.Rates)*16)
	for dateText, quotes := range body.Rates {
		date, err := time.Parse(time.DateOnly, dateText)
		if err != nil {
			return nil, fmt.Errorf("parsing frankfurter date %q: %w", dateText, err)
		}
		date = day(date)
		if date.Before(from) || date.After(to) {
			continue
		}
		for quote, rate := range quotes {
			if !money.ValidCurrency(quote) || quote == baseCurrency || rate <= 0 {
				continue
			}
			rates = append(rates, Rate{Date: date, Base: baseCurrency, Quote: quote, Rate: rate})
		}
	}
	sortRates(rates)
	return rates, nil
}
