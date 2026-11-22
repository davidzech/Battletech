package catalog

import (
	"testing"
)

func TestAdjustedBV(t *testing.T) {
	for _, c := range []struct{ base, g, p, want int }{
		{1000, 4, 5, 1000}, // the standard pilot
		{1000, 3, 4, 1320},
		{1000, 0, 0, 2420},
		{1000, 9, 9, 640}, // skills past 8 count as 8
		{2351, 4, 5, 2351},
	} {
		if got := AdjustedBV(c.base, c.g, c.p); got != c.want {
			t.Errorf("adjustedBV(%d, %d, %d) = %d, want %d", c.base, c.g, c.p, got, c.want)
		}
	}
}
