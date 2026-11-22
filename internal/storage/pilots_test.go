package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
)

// abilityStore checks choosing and resetting Edge abilities on both databases, in a campaign of its own (run from storeContract).
func abilityStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp = "skillcamp"
	// 180 SP in Edge abilities is level 2, so two abilities to choose.
	a, err := s.AddPilot(ctx, id, camp, "Alrek", "", pilot.SPAllocation{EdgeAbilities: 180}, 4)
	if err != nil {
		t.Fatal(err)
	}
	get := func() pilot.NamedPilot {
		ps, _ := s.Pilots(ctx, id, camp)
		for _, p := range ps {
			if p.ID == a.ID {
				return p
			}
		}
		t.Fatal("pilot vanished")
		return pilot.NamedPilot{}
	}
	if p := get(); p.EdgeAbilities() != 2 || p.PendingAbilities() != 2 || len(p.Learned) != 0 {
		t.Fatalf("two abilities earned, none chosen: %+v", p)
	}
	for name, c := range map[string]struct {
		ids  []string
		want error
	}{
		"none":           {nil, ErrAbilityCount},
		"too few":        {[]string{"marksman"}, ErrAbilityCount},
		"too many":       {[]string{"marksman", "nimble", "patient"}, ErrAbilityCount},
		"unknown":        {[]string{"marksman", "nope"}, ErrUnknownAbility},
		"the same twice": {[]string{"marksman", "marksman"}, ErrAbilityLearned},
	} {
		if err := s.LearnAbilities(ctx, id, camp, a.ID, c.ids); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if p := get(); len(p.Learned) != 0 {
		t.Fatalf("refused choices changed the pilot: %+v", p.Learned)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman"}); err != nil {
		t.Fatal(err)
	}
	if p := get(); strings.Join(p.Learned, ",") != "nimble,marksman" || p.PendingAbilities() != 0 {
		t.Fatalf("learned in the order chosen: %+v", p.Learned)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"patient"}); !errors.Is(err, ErrNoPendingAbilities) {
		t.Fatalf("nothing pending: %v", err)
	}
	// A new level brings a new choice; an ability can only be learned once.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 180}); err != nil { // 360: level 3
		t.Fatal(err)
	}
	if p := get(); p.PendingAbilities() != 1 {
		t.Fatalf("level 3 with two learned: %d pending", p.PendingAbilities())
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"marksman"}); !errors.Is(err, ErrAbilityLearned) {
		t.Fatalf("already learned: %v", err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"bulwark"}); err != nil {
		t.Fatal(err)
	}
	if p := get(); strings.Join(p.Learned, ",") != "nimble,marksman,bulwark" {
		t.Fatalf("order after a later choice: %+v", p.Learned)
	}
	// Resetting the SP allocation forgets the learned abilities too: they depended on the Edge abilities SP that has just been taken back.
	if err := s.ResetPilotSP(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if p := get(); len(p.Learned) != 0 || p.EdgeAbilities() != 0 || p.PendingAbilities() != 0 {
		t.Fatalf("an SP reset forgets learned abilities: %+v level %d pending %d", p.Learned, p.EdgeAbilities(), p.PendingAbilities())
	}
	// Setting the total: at or above the allocation touches nothing, refused below it, and below it with the reset agreed forgets them.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 360}); err != nil { // level 3
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman", "patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 1000, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 100, false); !errors.Is(err, ErrTotalBelowAllocated) {
		t.Fatalf("below the allocation: %v", err)
	}
	if p := get(); len(p.Learned) != 3 {
		t.Fatalf("a raised or refused total must keep the abilities: %+v", p.Learned)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if p := get(); len(p.Learned) != 0 || p.SP.Total() != 0 || p.TotalSP != 100 {
		t.Fatalf("a total below the allocation, with the reset, forgets the abilities: %+v", p)
	}
	// The explicit reset forgets them, for the pilot to choose again.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 360}); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman", "patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetEdgeAbilities(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if p := get(); len(p.Learned) != 0 || p.PendingAbilities() != 3 {
		t.Fatalf("after the reset: %+v pending %d", p.Learned, p.PendingAbilities())
	}
	if err := s.LearnAbilities(ctx, id, camp, "nobody", []string{"nimble"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	if err := s.ResetEdgeAbilities(ctx, id, camp, "nobody"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("reset of an unknown pilot: %v", err)
	}
	if err := s.LearnAbilities(ctx, id, "another", a.ID, []string{"nimble", "marksman", "patient"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}
	// A pilot Killed in Action can't choose, but their abilities can still be reset.
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble", "marksman", "patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFallen(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"cautious"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the fallen choose nothing: %v", err)
	}
	count := func() int {
		var n int
		if err := s.db.QueryRow(s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), a.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 3 {
		t.Fatalf("learned rows: %d", count())
	}
	if err := s.ResetEdgeAbilities(ctx, id, camp, a.ID); err != nil || count() != 0 {
		t.Fatalf("reset a fallen pilot: %v %d", err, count())
	}
	// Deleting a pilot or a whole campaign takes their abilities with it.
	if err := s.LearnAbilities(ctx, id, camp, a.ID, []string{"nimble"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("still fallen")
	}
	b, err := s.AddPilot(ctx, id, camp, "Katarina", "", pilot.SPAllocation{EdgeAbilities: 60}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, b.ID, []string{"patient"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePilot(ctx, id, camp, b.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), b.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a deleted pilot's abilities: %d %v", n, err)
	}
	c, err := s.AddPilot(ctx, id, camp, "Jarek", "", pilot.SPAllocation{EdgeAbilities: 60}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LearnAbilities(ctx, id, camp, c.ID, []string{"cautious"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRun(ctx, id, camp); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), c.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a deleted campaign's abilities: %d %v", n, err)
	}
}

// pilotStore exercises the named pilot queries; it is run for both databases from storeContract.
func pilotStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	if ps, err := s.Pilots(ctx, id, "refusalwar"); err != nil || len(ps) != 0 {
		t.Fatalf("no pilots yet: %v %v", ps, err)
	}
	a, err := s.AddPilot(ctx, id, "refusalwar", "Alrek", "", pilot.SPAllocation{Gunnery: 150}, 2)
	if err != nil || a.ID == "" || a.Name != "Alrek" {
		t.Fatalf("add pilot: %+v, %v", a, err)
	}
	b, err := s.AddPilot(ctx, id, "refusalwar", "Katarina", "", pilot.SPAllocation{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPilot(ctx, id, "refusalwar", "Third", "", pilot.SPAllocation{}, 2); !errors.Is(err, pilot.ErrPilotLimit) {
		t.Fatalf("the limit should refuse a third pilot, got %v", err)
	}
	// Another campaign has its own pilots.
	if _, err := s.AddPilot(ctx, id, "another", "Elsewhere", "", pilot.SPAllocation{}, 2); err != nil {
		t.Fatalf("pilots belong to a campaign: %v", err)
	}
	ps, err := s.Pilots(ctx, id, "refusalwar")
	if err != nil || len(ps) != 2 || ps[0].ID != a.ID || ps[1].ID != b.ID || ps[0].SP.Gunnery != 150 || ps[0].Fallen {
		t.Fatalf("pilots = %+v, %v", ps, err)
	}

	// SP adds to the columns.
	if err := s.AwardSP(ctx, id, "refusalwar", a.ID, pilot.SPAllocation{Gunnery: 150, EdgeTokens: 60}); err != nil {
		t.Fatal(err)
	}
	ps, _ = s.Pilots(ctx, id, "refusalwar")
	if ps[0].SP != (pilot.SPAllocation{Gunnery: 300, EdgeTokens: 60}) || ps[0].Gunnery() != 3 || ps[0].EdgeTokens() != 2 {
		t.Fatalf("after award: %+v", ps[0])
	}
	if err := s.AwardSP(ctx, id, "refusalwar", "no-such-pilot", pilot.SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	if err := s.AwardSP(ctx, id, "another", a.ID, pilot.SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}

	// A fallen pilot frees their place, keeps their record, and can no longer earn SP.
	if err := s.MarkFallen(ctx, id, "refusalwar", b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFallen(ctx, id, "refusalwar", b.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("already fallen: %v", err)
	}
	if err := s.AwardSP(ctx, id, "refusalwar", b.ID, pilot.SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the fallen earn nothing: %v", err)
	}
	if _, err := s.AddPilot(ctx, id, "refusalwar", "Replacement", "", pilot.SPAllocation{}, 2); err != nil {
		t.Fatalf("a fallen pilot should free a place: %v", err)
	}
	if ps, _ := s.Pilots(ctx, id, "refusalwar"); len(ps) != 3 || !ps[1].Fallen {
		t.Fatalf("the fallen pilot should stay on record: %+v", ps)
	}

	// Starting a campaign creates its run and pilots together, and only once.
	start := []pilot.NewPilot{{Name: "Alrek", SP: pilot.SPAllocation{Gunnery: 90, EdgeTokens: 60}}, {Name: "Katarina", SP: pilot.SPAllocation{Piloting: 100, EdgeTokens: 50}}}
	hangar := mekbay.Hangar{URL: goodHangarURL, Instance: goodInstance}
	if err := s.StartRunWithPilots(ctx, id, "wizard", hangar, start, 4500, 1, "Test Force"); err != nil {
		t.Fatal(err)
	}
	if r, ok, _ := s.Run(ctx, id, "wizard"); !ok || r.HangarInstance != goodInstance || r.Warchest != 4500 || r.WarchestText() != "4,500" {
		t.Fatalf("run not started: %+v", r)
	}
	if ps, _ := s.Pilots(ctx, id, "wizard"); len(ps) != 2 || ps[0].Name != "Alrek" || ps[0].SP != start[0].SP || ps[1].SP != start[1].SP || ps[1].Piloting() != 4 {
		t.Fatalf("starting pilots = %+v", ps)
	}
	if err := s.StartRunWithPilots(ctx, id, "wizard", hangar, start, 4500, 1, "Test Force"); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("starting twice should be refused, got %v", err)
	}
	if ps, _ := s.Pilots(ctx, id, "wizard"); len(ps) != 2 {
		t.Fatalf("a refused start must not add pilots: %+v", ps)
	}
	if err := s.StartRunWithPilots(ctx, "no-such-playthrough", "wizard", hangar, start, 4500, 1, "Test Force"); err == nil {
		t.Fatal("a run needs a playthrough")
	}
}

// portraitStore checks the portrait rules on a store (run for both databases from storeContract, in a campaign of its own).
func portraitStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	pa, pb := pilot.Portraits()[0].File, pilot.Portraits()[1].File
	a, err := s.AddPilot(ctx, id, "portraitcamp", "Alrek", pa, pilot.SPAllocation{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPilot(ctx, id, "portraitcamp", "Katarina", pa, pilot.SPAllocation{}, 4); !errors.Is(err, pilot.ErrPortraitTaken) {
		t.Fatalf("same portrait: %v", err)
	}
	b, err := s.AddPilot(ctx, id, "portraitcamp", "Katarina", pb, pilot.SPAllocation{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotPortrait(ctx, id, "portraitcamp", b.ID, pa); !errors.Is(err, pilot.ErrPortraitTaken) {
		t.Fatalf("change to a taken one: %v", err)
	}
	pc := pilot.Portraits()[2].File
	if err := s.SetPilotPortrait(ctx, id, "portraitcamp", b.ID, pc); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPilotPortrait(ctx, id, "portraitcamp", "nobody", pilot.Portraits()[3].File); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	// A fallen pilot frees their portrait.
	if err := s.MarkFallen(ctx, id, "portraitcamp", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPilot(ctx, id, "portraitcamp", "Heir", pa, pilot.SPAllocation{}, 4); err != nil {
		t.Fatalf("fallen pilot's portrait should be free: %v", err)
	}
}

// spStore checks earning and allocating SP, in a campaign of its own (run for both databases from storeContract).
func spStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp = "spcamp"
	a, err := s.AddPilot(ctx, id, camp, "Alrek", "", pilot.SPAllocation{Gunnery: 90, EdgeTokens: 60}, 4)
	if err != nil {
		t.Fatal(err)
	}
	get := func() pilot.NamedPilot {
		ps, _ := s.Pilots(ctx, id, camp)
		for _, p := range ps {
			if p.ID == a.ID {
				return p
			}
		}
		t.Fatal("pilot vanished")
		return pilot.NamedPilot{}
	}
	if p := get(); p.TotalSP != 150 || p.Unallocated() != 0 {
		t.Fatalf("a new pilot has allocated every starting SP: %+v", p)
	}
	if err := s.EarnSP(ctx, id, camp, a.ID, 300); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.TotalSP != 450 || p.Unallocated() != 300 || p.SP.Total() != 150 {
		t.Fatalf("after earning 300: %+v", p)
	}
	// Allocating spends only what is unallocated.
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 200, Piloting: 101}); !errors.Is(err, ErrNotEnoughSP) {
		t.Fatalf("301 of 300: %v", err)
	}
	if p := get(); p.Unallocated() != 300 {
		t.Fatalf("a refused allocation changed the pilot: %+v", p)
	}
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 200, Piloting: 50}); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.SP != (pilot.SPAllocation{Gunnery: 290, Piloting: 50, EdgeTokens: 60}) || p.TotalSP != 450 || p.Unallocated() != 50 {
		t.Fatalf("after allocating 250: %+v", p)
	}
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{EdgeAbilities: 50}); err != nil {
		t.Fatalf("the last 50: %v", err)
	}
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 1}); !errors.Is(err, ErrNotEnoughSP) {
		t.Fatalf("nothing left: %v", err)
	}
	// Earned SP and an allocation commit together, or not at all.
	if err := s.ApplySP(ctx, id, camp, a.ID, 100, pilot.SPAllocation{Gunnery: 101}); !errors.Is(err, ErrNotEnoughSP) {
		t.Fatalf("101 of 100 earned: %v", err)
	}
	if p := get(); p.TotalSP != 450 {
		t.Fatalf("a refused apply changed the total: %+v", p)
	}
	if err := s.ApplySP(ctx, id, camp, a.ID, 100, pilot.SPAllocation{Gunnery: 60, Piloting: 40}); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.TotalSP != 550 || p.Unallocated() != 0 || p.SP.Gunnery != 350 || p.SP.Piloting != 90 {
		t.Fatalf("after earning 100 and allocating it: %+v", p)
	}
	if err := s.ApplySP(ctx, id, camp, a.ID, 25, pilot.SPAllocation{}); err != nil {
		t.Fatalf("earning without allocating: %v", err)
	}
	if p := get(); p.TotalSP != 575 || p.Unallocated() != 25 {
		t.Fatalf("after earning 25: %+v", p)
	}
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 25}); err != nil {
		t.Fatal(err)
	}
	if err := s.AllocateSP(ctx, id, camp, "nobody", pilot.SPAllocation{Gunnery: 1}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	if err := s.EarnSP(ctx, id, "another", a.ID, 5); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}
	// The direct award (used by /api/pilots/sp) raises the total along with the columns, leaving nothing unallocated.
	if err := s.AwardSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 10}); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.TotalSP != 585 || p.Unallocated() != 0 {
		t.Fatalf("after a direct award: %+v", p)
	}
	if err := s.MarkFallen(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.EarnSP(ctx, id, camp, a.ID, 5); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("the fallen earn nothing: %v", err)
	}
}

// repairStore checks the factory reset and the total override, in a campaign of its own (run for both databases).
func repairStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp = "repaircamp"
	a, err := s.AddPilot(ctx, id, camp, "Alrek", "", pilot.SPAllocation{Gunnery: 90, EdgeTokens: 60}, 4)
	if err != nil {
		t.Fatal(err)
	}
	get := func() pilot.NamedPilot {
		ps, _ := s.Pilots(ctx, id, camp)
		for _, p := range ps {
			if p.ID == a.ID {
				return p
			}
		}
		t.Fatal("pilot vanished")
		return pilot.NamedPilot{}
	}
	// Raising the total leaves the allocation alone.
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 500, false); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.TotalSP != 500 || p.SP.Total() != 150 || p.Unallocated() != 350 {
		t.Fatalf("raised total: %+v", p)
	}
	// Down to exactly what is allocated is fine; below it needs to reset the allocation.
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 150, false); err != nil {
		t.Fatalf("down to the allocated amount: %v", err)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 149, false); !errors.Is(err, ErrTotalBelowAllocated) {
		t.Fatalf("below the allocation without agreeing: %v", err)
	}
	if p := get(); p.TotalSP != 150 || p.SP.Total() != 150 {
		t.Fatalf("a refused override changed the pilot: %+v", p)
	}
	if err := s.SetPilotTotal(ctx, id, camp, a.ID, 100, true); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.TotalSP != 100 || p.SP.Total() != 0 || p.Unallocated() != 100 || p.Gunnery() != 4 {
		t.Fatalf("total below the allocation, reset: %+v", p)
	}
	if err := s.SetPilotTotal(ctx, id, camp, "nobody", 10, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	// The factory reset keeps the total and frees everything, for living and killed pilots alike.
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 70, Piloting: 30}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPilotSP(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.TotalSP != 100 || p.SP.Total() != 0 || p.Unallocated() != 100 {
		t.Fatalf("after the reset: %+v", p)
	}
	if err := s.AllocateSP(ctx, id, camp, a.ID, pilot.SPAllocation{Gunnery: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFallen(ctx, id, camp, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPilotSP(ctx, id, camp, a.ID); err != nil {
		t.Fatalf("resetting a killed pilot: %v", err)
	}
	if p := get(); p.SP.Total() != 0 || p.TotalSP != 100 {
		t.Fatalf("a killed pilot after the reset: %+v", p)
	}
	if err := s.ResetPilotSP(ctx, id, "another", a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}
}

// rankStore checks the rank column on both databases, in a campaign of its own.
func rankStore(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	const camp = "rankcamp"
	a, err := s.AddPilot(ctx, id, camp, "Alrek", "", pilot.SPAllocation{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	get := func() pilot.NamedPilot {
		ps, _ := s.Pilots(ctx, id, camp)
		return ps[0]
	}
	if got := get().Rank; got != "" {
		t.Fatalf("a new pilot has no stored rank (the lowest is shown): %q", got)
	}
	if err := s.SetPilotRank(ctx, id, camp, a.ID, "Star Captain"); err != nil {
		t.Fatal(err)
	}
	if got := get().Rank; got != "Star Captain" {
		t.Fatalf("rank = %q", got)
	}
	if err := s.SetPilotRank(ctx, id, camp, "nobody", "Khan"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown pilot: %v", err)
	}
	if err := s.SetPilotRank(ctx, id, "another", a.ID, "Khan"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another campaign's pilot: %v", err)
	}
}
