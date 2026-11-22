package pilot

import (
	"strings"
	"testing"
)

func TestLadders(t *testing.T) {
	for _, c := range []struct {
		l         ladder
		sp, level int
	}{
		// Gunnery starts at 4 and improves one step at a time as cumulative SP is reached.
		{gunneryLadder, 0, 4},
		{gunneryLadder, 299, 4},
		{gunneryLadder, 300, 3},
		{gunneryLadder, 699, 3},
		{gunneryLadder, 700, 2},
		{gunneryLadder, 1200, 1},
		{gunneryLadder, 2200, 0},
		{gunneryLadder, 99999, 0}, // the top of the ladder holds
		// Piloting starts at 5 and stops at 1.
		{pilotingLadder, 99, 5},
		{pilotingLadder, 100, 4},
		{pilotingLadder, 200, 3},
		{pilotingLadder, 700, 2},
		{pilotingLadder, 1200, 1},
		{pilotingLadder, 5000, 1},
		// Edge tokens start at 1.
		{edgeTokensLadder, 59, 1},
		{edgeTokensLadder, 60, 2},
		{edgeTokensLadder, 120, 3},
		{edgeTokensLadder, 560, 7},
		{edgeTokensLadder, 1100, 10},
		// Edge abilities start at none.
		{EdgeAbilitiesLadder, 59, 0},
		{EdgeAbilitiesLadder, 60, 1},
		{EdgeAbilitiesLadder, 180, 2},
		{EdgeAbilitiesLadder, 900, 5},
		{EdgeAbilitiesLadder, 5000, 5},
	} {
		if got := c.l.At(c.sp); got.Level != c.level {
			t.Errorf("%s at %d SP = level %d, want level %d", c.l.Name, c.sp, got.Level, c.level)
		}
	}
	if n, ok := gunneryLadder.next(0); !ok || n.SP != 300 || n.Level != 3 {
		t.Errorf("next gunnery step from 0 = %+v, %v", n, ok)
	}
	if n, ok := gunneryLadder.next(300); !ok || n.SP != 700 {
		t.Errorf("next gunnery step from 300 = %+v, %v", n, ok)
	}
	if _, ok := gunneryLadder.next(2200); ok {
		t.Error("there is nothing above Gunnery 0")
	}
	// Every ladder's SP costs must rise, or a cheaper step could be skipped.
	for _, l := range []ladder{gunneryLadder, pilotingLadder, edgeTokensLadder, EdgeAbilitiesLadder} {
		for i := 1; i < len(l.Rows); i++ {
			if l.Rows[i].SP <= l.Rows[i-1].SP {
				t.Errorf("%s: row %d (%+v) must cost more than the one before", l.Name, i, l.Rows[i])
			}
		}
	}
}

// TestKirbyExample replays the worked example in the book (Hot Spots: Draconis Reach, p. 34): Kirby puts 120 SP into
// Edge tokens and 30 into Edge abilities, then later 60 more into abilities.
func TestKirbyExample(t *testing.T) {
	kirby := NamedPilot{Name: "Kirby", SP: SPAllocation{EdgeTokens: 120, EdgeAbilities: 30}, TotalSP: 150}
	if kirby.EdgeTokens() != 3 || kirby.EdgeAbilities() != 0 || kirby.Earned() != 150 {
		t.Fatalf("Kirby after 150 SP: tokens %d, abilities %d, earned %d (the book says 3, 0, 150)", kirby.EdgeTokens(), kirby.EdgeAbilities(), kirby.Earned())
	}
	kirby.SP.EdgeAbilities += 60
	if kirby.SP.EdgeAbilities != 90 || kirby.EdgeAbilities() != 1 {
		t.Fatalf("Kirby after 60 more: abilities SP %d, abilities %d (the book says 90, 1)", kirby.SP.EdgeAbilities, kirby.EdgeAbilities())
	}
	// A brand new pilot is a standard 4/5 pilot with one Edge token and no abilities.
	if p := (NamedPilot{}); p.Gunnery() != 4 || p.Piloting() != 5 || p.EdgeTokens() != 1 || p.EdgeAbilities() != 0 {
		t.Errorf("new pilot = %d/%d, edge %d, abilities %d", p.Gunnery(), p.Piloting(), p.EdgeTokens(), p.EdgeAbilities())
	}
}

func TestPilotView(t *testing.T) {
	views, alive := Views([]NamedPilot{
		{ID: "a", Name: "Alrek", SP: SPAllocation{Gunnery: 300, Piloting: 50}, TotalSP: 400},
		{ID: "b", Name: "Katarina", Fallen: true, Rank: "High"},
	}, []string{"Low", "High"})
	if alive != 1 || len(views) != 2 || views[0].Rank != "Low" || views[1].Rank != "High" {
		t.Fatalf("alive %d of %d", alive, len(views))
	}
	a := views[0]
	if a.Gunnery != 3 || a.Piloting != 5 || a.Earned != 400 || a.Unallocated != 50 || !views[1].Fallen {
		t.Errorf("view = %+v", a)
	}
	g, p := a.Columns[0], a.Columns[1]
	if g.Name != "Gunnery" || g.SP != 300 || !g.HasNext || g.NextAt != 700 || g.NextLvl != 2 {
		t.Errorf("gunnery column = %+v", g)
	}
	if p.SP != 50 || p.NextAt != 100 || p.NextLvl != 4 {
		t.Errorf("piloting column = %+v", p)
	}
}

func TestParsePilotName(t *testing.T) {
	for _, good := range []string{"Alrek", "Star Captain Alrek", "Ulric d'Aleshire"} {
		if got, err := ParsePilotName(good); err != nil || got != good {
			t.Errorf("parsePilotName(%q) = %q, %v", good, got, err)
		}
	}
	for _, bad := range []string{"", "  ", "<b>", strings.Repeat("a", 31), "a\x00b"} {
		if _, err := ParsePilotName(bad); err == nil {
			t.Errorf("parsePilotName(%q) should fail", bad)
		}
	}
}

func TestPortraits(t *testing.T) {
	all := Portraits()
	if len(all) < 70 {
		t.Fatalf("only %d portraits embedded", len(all))
	}
	for _, p := range all {
		if !ValidPortrait(p.File) || p.Group == "" || p.Label == "" {
			t.Errorf("bad portrait %+v", p)
		}
	}
	if ValidPortrait("../x.png") || ValidPortrait("") || ValidPortrait("nope.png") {
		t.Error("invalid names accepted")
	}
	// Legacy pilots (no stored portrait) get a stable one; a picked one wins.
	if first := portraitOf("abc", ""); first != portraitOf("abc", "") || !ValidPortrait(first) {
		t.Error("fallback portrait not stable or invalid")
	}
	if portraitOf("abc", all[3].File) != all[3].File {
		t.Error("picked portrait ignored")
	}
	// A random pick avoids the taken ones.
	taken := map[string]bool{}
	for _, p := range all[1:] {
		taken[p.File] = true
	}
	if RandomPortrait(taken) != all[0].File {
		t.Error("random pick used a taken portrait")
	}
}

func TestRankHelpers(t *testing.T) {
	ranks := []string{"MechWarrior", "Star Commander", "Khan"}
	for stored, want := range map[string]string{"": "MechWarrior", "Khan": "Khan", "Star Commander": "Star Commander", "General": "MechWarrior", "khan": "MechWarrior"} {
		if got := rankFor(ranks, stored); got != want {
			t.Errorf("rankFor(%q) = %q, want %q", stored, got, want)
		}
	}
	if rankFor(nil, "Khan") != "" {
		t.Error("a campaign without ranks shows none")
	}
	if !ValidRank(ranks, "Khan") || ValidRank(ranks, "khan") || ValidRank(ranks, "") || ValidRank(nil, "Khan") {
		t.Error("validRank")
	}
}
