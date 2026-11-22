package hangar

import (
	"testing"

	"github.com/davidzech/Battletech/internal/catalog"
)

func TestSwapPriceAndOptions(t *testing.T) {
	prime, ok := catalog.ByName("BMDasher_Prime")
	if !ok || prime.Omni == 0 {
		t.Fatal("the Fire Moth Prime should be an omnimech in the catalog")
	}
	opts := SwapOptions(prime)
	if len(opts) < 2 {
		t.Fatalf("a Fire Moth has several variants: %+v", opts)
	}
	var up, down *SwapOption
	for i, o := range opts {
		v, _ := catalog.ByName(o.Unit)
		if v.Chassis != prime.Chassis || o.Unit == prime.Name || v.Omni == 0 {
			t.Errorf("an option must be another omni variant of the same chassis: %+v", o)
		}
		switch {
		case o.BV > prime.BV && up == nil:
			up = &opts[i]
		case o.BV < prime.BV && down == nil:
			down = &opts[i]
		}
	}
	if up == nil || down == nil {
		t.Fatalf("expected a dearer and a cheaper variant: %+v", opts)
	}
	// Half the tonnage (20t: 10 SP) plus half the BV gained; a cheaper variant costs only the half tonnage.
	if want := 10 + (up.BV-prime.BV)/2; up.Cost != want {
		t.Errorf("up cost = %d, want %d", up.Cost, want)
	}
	if down.Cost != 10 {
		t.Errorf("down cost = %d, want 10 and no refund", down.Cost)
	}
	// An odd tonnage rounds the half up.
	if got := swapPrice(catalog.Unit{Tons: 55, BV: 1000}, catalog.Unit{BV: 1000}); got != 28 {
		t.Errorf("55t swap = %d, want 28", got)
	}
	// Being an omnimech is an advantage over selling and buying: the swap is cheaper than the new BV less half the old one.
	if up.Cost >= up.BV-prime.BV/2 {
		t.Errorf("swapping (%d) should beat selling and rebuying (%d)", up.Cost, up.BV-prime.BV/2)
	}
	// And swapping up then selling a clean unit never makes money.
	if SellValue(up.BV, true)-SellValue(prime.BV, true)-up.Cost >= 0 {
		t.Error("swap up and sell must lose the half tonnage at least")
	}
	// A unit that is not an omnimech has no variants to swap to.
	grizzly, _ := catalog.ByName("BMGrizzly")
	if grizzly.Omni != 0 || SwapOptions(grizzly) != nil {
		t.Errorf("the Grizzly is not an omnimech: %+v", SwapOptions(grizzly))
	}
}
