package mekbay

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/davidzech/Battletech/internal/layouts"
)

// Which format the site writes new forces in. MekBay's release (mekbay.com) reads and writes version 1 forces until its next release (next.mekbay.com)
// becomes it; that release reads version 1 forces too but saves every force it changes in version 2. The site follows: while MekBay's release is the old
// one it writes version 1 forces, which players' MekBay can open, and from the day it serves version 2 (WatchRelease notices) it writes version 2 and
// converts each hangar to version 2 the next time it is changed. Whatever the mode, the site reads both formats, and a hangar that already is version 2 (a
// battle came back from MekBay in version 2) stays so.

var v2Mode atomic.Bool

// SetV2 sets whether the site writes version 2 forces.
func SetV2(on bool) {
	if v2Mode.Swap(on) != on {
		v := 1
		if on {
			v = 2
		}
		log.Printf("MekBay: writing version %d forces", v)
	}
}

// V2 says whether the site writes version 2 forces.
func V2() bool { return v2Mode.Load() }

// WatchRelease turns version 2 on once MekBay's release serves the version 2 web app (it publishes its unit files: layouts.Assets), checking at once and
// then every interval until ctx ends. Once on, it stays on.
func WatchRelease(ctx context.Context, interval time.Duration) {
	for !V2() {
		c, cancel := context.WithTimeout(ctx, time.Minute)
		_, _, _, err := layouts.Assets(c, layouts.Release)
		cancel()
		switch {
		case err == nil:
			SetV2(true)
			return
		case !errors.Is(err, layouts.ErrNoUnitFiles):
			log.Printf("MekBay: could not check its release's version: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// UpgradeHangar converts a hangar being edited (the decoded JSON of its saved data) to version 2 in place when the site writes version 2 and it is still
// version 1; otherwise it leaves it as it is.
func UpgradeHangar(force map[string]any) error {
	if !V2() || ForceVersion(force) == 2 {
		return nil
	}
	return upgradeInPlace(force)
}

// upgradeInPlace replaces a version 1 force's contents with its version 2 conversion.
func upgradeInPlace(force map[string]any) error {
	v2, err := ConvertToV2(force)
	if err != nil {
		return err
	}
	for k := range force {
		delete(force, k)
	}
	for k, v := range v2 {
		force[k] = v
	}
	return nil
}
