package codec

import (
	"math"
	"testing"
)

func TestCoreSchemaNumberMatchesRunnerConversion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want float64
		ok   bool
	}{
		{"42", 42, true},
		{"-7", -7, true},
		{"1.5e3", 1500, true},
		{".5", 0.5, true},
		{"0x10", 16, true},
		{"0o17", 15, true},
		{"0x7fffffff", math.MaxInt32, true},
		{"0xffffffff", -1, true},
		{"0x80000000", math.MinInt32, true},
		{"0x100000000", 0, false},
		{".Inf", math.Inf(1), true},
		{"-.INF", math.Inf(-1), true},
		{"1e400", 0, false},
		{"0b1", 0, false},
		{"1_000", 0, false},
		{"text", 0, false},
	} {
		got, ok := CoreSchemaNumber(tc.text)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("CoreSchemaNumber(%q) = %v, %t, want %v, %t", tc.text, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := CoreSchemaNumber(".NaN"); !ok || !math.IsNaN(got) {
		t.Errorf("CoreSchemaNumber(.NaN) = %v, %t, want NaN, true", got, ok)
	}
}

func TestCoreSchemaRadixIntRejectsDecimal(t *testing.T) {
	t.Parallel()
	if _, err := CoreSchemaRadixInt("10"); err == nil {
		t.Fatal("CoreSchemaRadixInt accepted a decimal integer")
	}
}
