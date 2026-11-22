package catalog

import (
	"testing"
)

// Firepower and range follow MekBay's stat bars: the Atlas AS7-D's AC/20, LRM 20, SRM 6 and four medium lasers fire 20 + 20 + 12 + 4x5 = 72, the LRM
// reaches 21 hexes, and the damage-weighted range is 9.
func TestFirepowerAndRange(t *testing.T) {
	u, ok := ByName("BMAtlas_AS7D")
	if !ok {
		t.Fatal("no Atlas AS7-D in the catalog")
	}
	if u.Firepower != 72 || u.MaxRange != 21 || u.Range != 9 {
		t.Errorf("Atlas AS7-D: firepower %d, range %d (%d weighted), want 72, 21 (9)", u.Firepower, u.MaxRange, u.Range)
	}
}
