package main

import "testing"

// Money is a decimal string at the currency's precision. Coercing "29.00" to a
// float would be both wrong and lossy, and it is the single easiest way for a
// generated CLI to corrupt an amount.
func TestCoerceLeavesDecimalsAsStrings(t *testing.T) {
	for _, v := range []string{"29.00", "0.50", "1000.00"} {
		if got := coerce(v); got != any(v) {
			t.Errorf("coerce(%q) = %#v, want the string unchanged", v, got)
		}
	}
}

func TestCoerceBooleansAndIntegers(t *testing.T) {
	cases := map[string]any{
		"true":  true,
		"false": false,
		"5":     int64(5),
		"100":   int64(100),
		"NGN":   "NGN",
		"":      "",
	}
	for in, want := range cases {
		if got := coerce(in); got != want {
			t.Errorf("coerce(%q) = %#v, want %#v", in, got, want)
		}
	}
}
