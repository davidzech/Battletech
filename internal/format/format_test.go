package format

import (
	"testing"
)

func TestWithCommas(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 6000: "6,000", 12500: "12,500", 1234567: "1,234,567",
		-1: "-1", -500: "-500", -1000: "-1,000", -123456: "-123,456", -1234567: "-1,234,567"} {
		if got := Commas(n); got != want {
			t.Errorf("withCommas(%d) = %q, want %q", n, got, want)
		}
	}
}
