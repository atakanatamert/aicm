// SPDX-License-Identifier: SUL-1.0

package money

import (
	"math"
	"testing"
)

func TestParseDecimal(t *testing.T) {
	tests := []struct {
		in, cur string
		want    int64
		wantErr bool
	}{
		{"110.00", "CNY", 11000, false},
		{"12", "USD", 1200, false},
		{"0.5", "USD", 50, false},
		{".5", "USD", 50, false},
		{"-3.5", "USD", -350, false},
		{"+1.01", "EUR", 101, false},
		{"0.125", "USD", 13, false},   // half rounds away from zero
		{"0.124", "USD", 12, false},   // below half rounds down
		{"-0.125", "USD", -13, false}, // away from zero for negatives too
		{"1500", "JPY", 1500, false},  // zero-decimal currency
		{"1499.6", "JPY", 1500, false},
		{"1.2345", "KWD", 1235, false}, // three-decimal currency
		{" 7.10 ", "USD", 710, false},
		{"92233720368547758.07", "USD", math.MaxInt64, false},
		{"92233720368547758.08", "USD", 0, true}, // overflow
		{"1e3", "USD", 0, true},
		{"", "USD", 0, true},
		{".", "USD", 0, true},
		{"1,000.00", "USD", 0, true},
		{"12", "usd", 0, true}, // invalid code
	}
	for _, tt := range tests {
		got, err := ParseDecimal(tt.in, tt.cur)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseDecimal(%q, %s) error = %v, wantErr %v", tt.in, tt.cur, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (got.Minor != tt.want || got.Currency != tt.cur) {
			t.Errorf("ParseDecimal(%q, %s) = %+v, want %d", tt.in, tt.cur, got, tt.want)
		}
	}
}

func TestFromFloatAvoidsBinaryError(t *testing.T) {
	tests := []struct {
		in   float64
		want int64
	}{
		{0.1 + 0.2, 30}, // 0.30000000000000004
		{25.5, 2550},
		{19.99, 1999}, // 19.99*100 = 1998.9999999999998 in float math
		{1.005, 101},  // shortest repr is "1.005", rounds half up
	}
	for _, tt := range tests {
		got, err := FromFloat(tt.in, "USD")
		if err != nil || got.Minor != tt.want {
			t.Errorf("FromFloat(%v) = %d, %v; want %d", tt.in, got.Minor, err, tt.want)
		}
	}
	if _, err := FromFloat(math.NaN(), "USD"); err == nil {
		t.Error("FromFloat(NaN) succeeded")
	}
}

func TestParseMicro(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0.000375", 375},
		{"3", 3_000_000},
		{"0.0000005", 1}, // rounds half away from zero
		{"0.0000004", 0},
	}
	for _, tt := range tests {
		got, err := ParseMicro(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseMicro(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
	if got, err := MicroFromFloat(3e-06); err != nil || got != 3 {
		t.Errorf("MicroFromFloat(3e-06) = %d, %v; want 3", got, err)
	}
}

func TestDecimal(t *testing.T) {
	tests := []struct {
		a    Amount
		want string
	}{
		{Amount{1234, "USD"}, "12.34"},
		{Amount{5, "USD"}, "0.05"},
		{Amount{-5, "USD"}, "-0.05"},
		{Amount{0, "EUR"}, "0.00"},
		{Amount{1234, "JPY"}, "1234"},
		{Amount{1234, "KWD"}, "1.234"},
		{Amount{math.MinInt64, "USD"}, "-92233720368547758.08"},
	}
	for _, tt := range tests {
		if got := tt.a.Decimal(); got != tt.want {
			t.Errorf("%+v.Decimal() = %q, want %q", tt.a, got, tt.want)
		}
	}
}

func TestAdd(t *testing.T) {
	sum, err := Amount{150, "USD"}.Add(Amount{-50, "USD"})
	if err != nil || sum != (Amount{100, "USD"}) {
		t.Errorf("Add = %+v, %v", sum, err)
	}
	if _, err := (Amount{1, "USD"}).Add(Amount{1, "EUR"}); err == nil {
		t.Error("adding different currencies succeeded")
	}
	if _, err := (Amount{math.MaxInt64, "USD"}).Add(Amount{1, "USD"}); err == nil {
		t.Error("overflow not detected")
	}
}
