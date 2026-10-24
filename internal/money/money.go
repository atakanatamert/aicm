// SPDX-License-Identifier: SUL-1.0

// Package money handles amounts as integer minor units with an ISO 4217 code.
// Money is never a float. Provider responses that carry decimals (strings or
// JSON numbers) are parsed here, digit by digit, never through float math.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Amount is money in minor units of Currency (cents for USD, fen for CNY,
// yen for JPY).
type Amount struct {
	Minor    int64  `json:"amount_minor"`
	Currency string `json:"currency"`
}

// zeroDecimal and threeDecimal list ISO 4217 currencies whose minor unit is
// not 2 digits. Every other valid code uses 2.
var (
	zeroDecimal  = setOf("BIF", "CLP", "DJF", "GNF", "ISK", "JPY", "KMF", "KRW", "PYG", "RWF", "UGX", "UYI", "VND", "VUV", "XAF", "XOF", "XPF")
	threeDecimal = setOf("BHD", "IQD", "JOD", "KWD", "LYD", "OMR", "TND")
)

func setOf(codes ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		m[c] = struct{}{}
	}
	return m
}

// Exponent returns the number of minor-unit digits for an ISO 4217 code.
func Exponent(currency string) (int, error) {
	if !ValidCurrency(currency) {
		return 0, fmt.Errorf("invalid currency code %q", currency)
	}
	if _, ok := zeroDecimal[currency]; ok {
		return 0, nil
	}
	if _, ok := threeDecimal[currency]; ok {
		return 3, nil
	}
	return 2, nil
}

// ValidCurrency reports whether s looks like an ISO 4217 code (three
// upper-case letters).
func ValidCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := range 3 {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// ParseDecimal parses a decimal string such as "110.00", "-3.5" or "12" in
// major units and returns minor units. Extra fractional digits are rounded
// half away from zero ("0.125" USD is 13 cents). Exponents ("1e3") are rejected.
func ParseDecimal(s, currency string) (Amount, error) {
	exp, err := Exponent(currency)
	if err != nil {
		return Amount{}, err
	}
	minor, err := parseScaled(s, exp)
	if err != nil {
		return Amount{}, fmt.Errorf("parsing %s amount %q: %w", currency, s, err)
	}
	return Amount{Minor: minor, Currency: currency}, nil
}

// FromFloat converts a JSON number decoded as float64 into minor units. It
// goes through the shortest decimal representation of f, so 0.1 stays 0.1
// rather than 0.1000000000000000055. Prefer decoding with json.Number and
// ParseDecimal when the adapter controls decoding.
func FromFloat(f float64, currency string) (Amount, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Amount{}, fmt.Errorf("amount %v is not finite", f)
	}
	return ParseDecimal(strconv.FormatFloat(f, 'f', -1, 64), currency)
}

// MicroDigits is the scale of micro units used for token-cost estimates.
const MicroDigits = 6

// ParseMicro parses a decimal string in major units into micro units
// (1e-6), the scale used for token-cost estimates. "0.000375" is 375.
func ParseMicro(s string) (int64, error) {
	v, err := parseScaled(s, MicroDigits)
	if err != nil {
		return 0, fmt.Errorf("parsing micro amount %q: %w", s, err)
	}
	return v, nil
}

// MicroFromFloat is FromFloat for micro units.
func MicroFromFloat(f float64) (int64, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("amount %v is not finite", f)
	}
	return ParseMicro(strconv.FormatFloat(f, 'f', -1, 64))
}

// Decimal formats the amount in major units with exactly the currency's
// number of fraction digits: 1234 USD is "12.34", 1234 JPY is "1234".
func (a Amount) Decimal() string {
	exp, err := Exponent(a.Currency)
	if err != nil {
		exp = 2
	}
	return formatScaled(a.Minor, exp)
}

// String formats the amount as "12.34 USD".
func (a Amount) String() string {
	return a.Decimal() + " " + a.Currency
}

// Add returns a+b. Both must share a currency.
func (a Amount) Add(b Amount) (Amount, error) {
	if a.Currency != b.Currency {
		return Amount{}, fmt.Errorf("adding %s to %s", b.Currency, a.Currency)
	}
	sum := a.Minor + b.Minor
	if (b.Minor > 0 && sum < a.Minor) || (b.Minor < 0 && sum > a.Minor) {
		return Amount{}, errors.New("amount overflow")
	}
	return Amount{Minor: sum, Currency: a.Currency}, nil
}

var errSyntax = errors.New("not a plain decimal number")

// parseScaled parses a plain decimal and returns it multiplied by 10^digits,
// rounding half away from zero.
func parseScaled(s string, digits int) (int64, error) {
	s = strings.TrimSpace(s)
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" && frac == "" {
		return 0, errSyntax
	}
	if !allDigits(intPart) || !allDigits(frac) {
		return 0, errSyntax
	}
	if intPart == "" {
		intPart = "0"
	}
	roundUp := false
	if len(frac) > digits {
		roundUp = frac[digits] >= '5'
		frac = frac[:digits]
	}
	frac += strings.Repeat("0", digits-len(frac))
	v, err := strconv.ParseInt(intPart+frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("out of range: %w", err)
	}
	if roundUp {
		if v == math.MaxInt64 {
			return 0, errors.New("out of range")
		}
		v++
	}
	if neg {
		v = -v
	}
	return v, nil
}

func formatScaled(v int64, digits int) string {
	if digits == 0 {
		return strconv.FormatInt(v, 10)
	}
	s := strconv.FormatInt(v, 10)
	sign := ""
	if v < 0 {
		sign, s = "-", s[1:]
	}
	if len(s) <= digits {
		s = strings.Repeat("0", digits-len(s)+1) + s
	}
	return sign + s[:len(s)-digits] + "." + s[len(s)-digits:]
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
