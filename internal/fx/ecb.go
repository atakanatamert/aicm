// SPDX-License-Identifier: SUL-1.0

package fx

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

const (
	// ecbURL90d covers the last 90 days; ecbURLFull is the whole history.
	ecbURL90d  = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist-90d.xml"
	ecbURLFull = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-hist.xml"

	// ecbShortHistoryDays is the range the 90-day feed covers.
	ecbShortHistoryDays = 90
)

// ecb is the fallback source: the European Central Bank reference rates.
type ecb struct {
	c *provider.HTTPClient
}

// NewECB returns a RateSource that reads the ECB's XML reference rates.
func NewECB(c *provider.HTTPClient) RateSource {
	return &ecb{c: c}
}

func (e *ecb) Name() string { return "ecb" }

// ecbCube mirrors one <Cube> element. Dated cubes nest currency cubes;
// encoding/xml fills Cubes recursively.
type ecbCube struct {
	Time     string    `xml:"time,attr"`
	Currency string    `xml:"currency,attr"`
	Rate     string    `xml:"rate,attr"`
	Cubes    []ecbCube `xml:"Cube"`
}

type ecbEnvelope struct {
	Cube ecbCube `xml:"Cube"`
}

// FetchRange reads the 90-day feed when from is recent, otherwise the full
// history, and filters it to [from, to].
func (e *ecb) FetchRange(ctx context.Context, from, to time.Time) ([]Rate, error) {
	from, to = day(from), day(to)
	if to.Before(from) {
		return nil, errors.New("ecb: range ends before it starts")
	}
	url := ecbURLFull
	if !from.Before(day(e.c.Now()).AddDate(0, 0, -ecbShortHistoryDays)) {
		url = ecbURL90d
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building ecb request: %w", err)
	}
	resp, err := e.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching ecb rates: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ecb rates: %w", err)
	}
	var envelope ecbEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decoding ecb rates: %w", err)
	}

	rates := make([]Rate, 0, 16*len(envelope.Cube.Cubes))
	var walk func(c ecbCube, dateText string) error
	walk = func(c ecbCube, dateText string) error {
		if c.Time != "" {
			dateText = c.Time
		}
		if c.Currency != "" {
			if dateText == "" {
				return errors.New("ecb: currency cube without a date")
			}
			date, err := time.Parse(time.DateOnly, dateText)
			if err != nil {
				return fmt.Errorf("parsing ecb date %q: %w", dateText, err)
			}
			rate, err := strconv.ParseFloat(c.Rate, 64)
			if err != nil {
				return fmt.Errorf("parsing ecb rate for %s on %s: %w", c.Currency, dateText, err)
			}
			if money.ValidCurrency(c.Currency) && c.Currency != baseCurrency && rate > 0 {
				date = day(date)
				if !date.Before(from) && !date.After(to) {
					rates = append(rates, Rate{Date: date, Base: baseCurrency, Quote: c.Currency, Rate: rate})
				}
			}
		}
		for _, child := range c.Cubes {
			if err := walk(child, dateText); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(envelope.Cube, ""); err != nil {
		return nil, err
	}
	sortRates(rates)
	return rates, nil
}
