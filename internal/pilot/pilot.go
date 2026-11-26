package pilot

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// Named pilots follow the ruleset in BattleTech: Hot Spots - Draconis Reach, "Named Pilots" (pp. 34-35). Only named
// pilots improve their skills, use Edge and earn Edge abilities. Each earned SP is allocated to one of the pilot's
// columns (Gunnery, Piloting, Edge tokens, Edge abilities), and a column's level is however far its cumulative SP
// reaches along that column's ladder in the book's Named Pilot Improvement Table.

// MaxNamedPilots is how many named pilots a command may have at once (a fallen pilot doesn't count).
const MaxNamedPilots = 4

// ladderRow is one step of an improvement ladder: reaching Level costs SP in total (cumulative, not per step).
// (The book also gives each step a handicap, used to balance opposing forces; this campaign doesn't track it.)
type ladderRow struct {
	Level int `json:"level"`
	SP    int `json:"sp"`
}

// ladder is one column of the improvement table: the level a pilot starts at, and the steps above it.
type ladder struct {
	Key  string      `json:"key"` // the column's form field and JSON name
	Name string      `json:"name"`
	Base int         `json:"base"` // the level with no SP allocated
	Rows []ladderRow `json:"rows"`
}

// The Named Pilot Improvement Table (Classic BattleTech columns). Gunnery and Piloting get better as the number
// falls; Edge tokens and Edge abilities as it rises. The Alpha Strike "Skill" column isn't used by this site.
var (
	gunneryLadder    = ladder{"gunnery", "Gunnery", 4, []ladderRow{{3, 300}, {2, 700}, {1, 1200}, {0, 2200}}}
	pilotingLadder   = ladder{"piloting", "Piloting", 5, []ladderRow{{4, 100}, {3, 200}, {2, 700}, {1, 1200}}}
	edgeTokensLadder = ladder{"edge_tokens", "Edge tokens", 1, []ladderRow{
		{2, 60}, {3, 120}, {4, 200}, {5, 300}, {6, 420}, {7, 560}, {8, 720}, {9, 900}, {10, 1100},
	}}
	EdgeAbilitiesLadder = ladder{"edge_abilities", "Edge abilities", 0, []ladderRow{{1, 60}, {2, 180}, {3, 360}, {4, 600}, {5, 900}}}
)

// at is the step a column has reached with sp allocated to it: the highest row it can afford, or the base level
// if none.
func (l ladder) At(sp int) ladderRow {
	reached := ladderRow{Level: l.Base}
	for _, r := range l.Rows {
		if sp >= r.SP {
			reached = r
		}
	}
	return reached
}

// next is the step after the one sp has reached; ok is false at the top of the ladder.
func (l ladder) next(sp int) (r ladderRow, ok bool) {
	for _, r := range l.Rows {
		if sp < r.SP {
			return r, true
		}
	}
	return ladderRow{}, false
}

// boxLabels are shorter names for the stat boxes, where "Edge abilities" would wrap onto two lines. Everywhere else (the slider
// rows, for instance) a column keeps its full name.
var boxLabels = map[string]string{"edge_abilities": "Abilities"}

// boxLabel is the name a column shows in its stat box.
func boxLabel(l ladder) string {
	if s, ok := boxLabels[l.Key]; ok {
		return s
	}
	return l.Name
}

// ladderView is a ladder as the pages receive it: the ladder plus the label for its stat box.
type ladderView struct {
	ladder
	Short string `json:"short"`
}

// LaddersJSON is the four ladders as JSON, handed to the campaign page so the pilot builder can preview levels with
// exactly the rules the server uses.
var LaddersJSON = func() string {
	var views []ladderView
	for _, l := range []ladder{gunneryLadder, pilotingLadder, edgeTokensLadder, EdgeAbilitiesLadder} {
		views = append(views, ladderView{l, boxLabel(l)})
	}
	b, err := json.Marshal(views)
	if err != nil {
		panic(err)
	}
	return string(b)
}()

// SPAllocation is SP spread over a pilot's four columns.
type SPAllocation struct {
	Gunnery       int
	Piloting      int
	EdgeTokens    int
	EdgeAbilities int
}

// Total is all the SP in the allocation.
func (a SPAllocation) Total() int { return a.Gunnery + a.Piloting + a.EdgeTokens + a.EdgeAbilities }

// NamedPilot is one of a command's named pilots: the SP they have earned in all (TotalSP) and how much of it is
// allocated to each of the four columns (SP); the rest is unallocated.
type NamedPilot struct {
	ID   string
	Name string
	// Portrait is the portrait the pilot picked (a file in portraits/); "" for a pilot from before portraits existed,
	// who is shown the one portraitOf works out for them.
	Portrait string
	// Rank is the pilot's cosmetic rank (a name from the campaign's ranks); "" until one is chosen, which reads as the lowest.
	Rank string
	// TotalSP is every SP the pilot has earned. The four columns in SP hold what has been allocated so far, so what is
	// still unallocated is TotalSP minus their sum (see Unallocated).
	TotalSP int
	Fallen  bool // died, and is permanently removed from the campaign (kept so the record isn't lost)
	SP      SPAllocation
	// Learned are the IDs of the Edge abilities the pilot has chosen (see abilities.go), in the order they were chosen. A pilot may hold as
	// many as their Edge abilities level (EdgeAbilities); what they haven't chosen yet is PendingAbilities.
	Learned []string
}

// PendingAbilities is how many Edge abilities the pilot has earned but not yet chosen: their Edge abilities level minus the abilities
// they have learned (never negative; resetting their SP allocation forgets their learned abilities too, see Store.ResetPilotSP).
func (p NamedPilot) PendingAbilities() int { return max(0, p.EdgeAbilities()-len(p.Learned)) }

// Earned is the pilot's total SP earned, allocated or not.
func (p NamedPilot) Earned() int { return p.TotalSP }

// Unallocated is the earned SP not yet allocated to a column.
func (p NamedPilot) Unallocated() int { return max(0, p.TotalSP-p.SP.Total()) }

// Gunnery is the pilot's current Gunnery skill (lower is better).
func (p NamedPilot) Gunnery() int { return gunneryLadder.At(p.SP.Gunnery).Level }

// Piloting is the pilot's current Piloting skill (lower is better).
func (p NamedPilot) Piloting() int { return pilotingLadder.At(p.SP.Piloting).Level }

// EdgeTokens is how many Edge tokens the pilot starts each scenario with.
func (p NamedPilot) EdgeTokens() int { return edgeTokensLadder.At(p.SP.EdgeTokens).Level }

// EdgeAbilities is how many Edge abilities the pilot has earned.
func (p NamedPilot) EdgeAbilities() int { return EdgeAbilitiesLadder.At(p.SP.EdgeAbilities).Level }

// stepPercent is how far sp has come from the breakpoint it reached (from) towards the next one (to), 0 to 100, rounded like
// the browser's (allocator.js): Edge abilities at 180 SP is 0% of the way to 360, and at 270 SP 50%.
func stepPercent(sp, from, to int) int {
	if to <= from {
		return 100
	}
	gap := to - from
	return max(0, min(100, ((sp-from)*100+gap/2)/gap))
}

// pilotColumn is one column of a pilot as the campaign page shows it.
type pilotColumn struct {
	Name    string
	Label   string // the (possibly shorter) name for the column's stat box
	SP      int    // SP allocated to this column
	Level   int    // level reached
	HasNext bool   // false at the top of the ladder
	NextAt  int    // SP the column needs in total to reach the next level
	NextLvl int
	Percent int // how far from the last level reached to the next, 0 to 100 (100 at the top of the ladder)
}

// PilotView is a named pilot with everything the campaign page shows already worked out.
type PilotView struct {
	ID, Name                                 string
	Portrait, PortraitURL                    string // the file picked (or worked out) and where it is served
	Rank                                     string // the pilot's rank (the lowest if none was chosen); "" when the campaign has no ranks
	Fallen                                   bool
	Gunnery, Piloting, EdgeTokens, Abilities int
	Earned, Unallocated, Allocated           int           // total SP earned, the part not yet in a column, and the part that is
	EdgeLearned                              []EdgeAbility // the Edge abilities the pilot has chosen, in order
	EdgePending                              int           // how many more they have earned and still have to choose
	Columns                                  []pilotColumn
}

func (p NamedPilot) View(ranks []string) PilotView {
	col := func(l ladder, sp int) pilotColumn {
		at := l.At(sp)
		c := pilotColumn{Name: l.Name, Label: boxLabel(l), SP: sp, Level: at.Level}
		c.Percent = 100
		if n, ok := l.next(sp); ok {
			c.HasNext, c.NextAt, c.NextLvl = true, n.SP, n.Level
			c.Percent = stepPercent(sp, at.SP, n.SP)
		}
		return c
	}
	return PilotView{
		ID: p.ID, Name: p.Name, Rank: rankFor(ranks, p.Rank), Fallen: p.Fallen, Portrait: portraitOf(p.ID, p.Portrait), PortraitURL: portraitURL(portraitOf(p.ID, p.Portrait)),
		Gunnery: p.Gunnery(), Piloting: p.Piloting(), EdgeTokens: p.EdgeTokens(), Abilities: p.EdgeAbilities(),
		Earned: p.Earned(), Unallocated: p.Unallocated(), Allocated: p.SP.Total(),
		EdgeLearned: edgeAbilityList(p.Learned), EdgePending: p.PendingAbilities(),
		Columns: []pilotColumn{
			col(gunneryLadder, p.SP.Gunnery), col(pilotingLadder, p.SP.Piloting),
			col(edgeTokensLadder, p.SP.EdgeTokens), col(EdgeAbilitiesLadder, p.SP.EdgeAbilities),
		},
	}
}

// Views prepares a campaign's pilots for display and counts how many are still alive.
func Views(pilots []NamedPilot, ranks []string) (views []PilotView, alive int) {
	for _, p := range pilots {
		views = append(views, p.View(ranks))
		if !p.Fallen {
			alive++
		}
	}
	return views, alive
}

// NewPilot is a pilot to create along with a campaign run: a name, their portrait and the SP allocated at the start.
type NewPilot struct {
	Name     string
	Portrait string
	SP       SPAllocation
	// Abilities are the Edge abilities the pilot starts with: exactly as many as their Edge abilities level (see edgeAbilitiesLadder).
	Abilities []string
}

// ErrPortraitTaken is returned when another living pilot of the campaign already has the portrait.
var ErrPortraitTaken = errors.New("another pilot already has that portrait")

// ErrPilotLimit is returned when a command already has the most named pilots it may have.
var ErrPilotLimit = errors.New("you already have as many named pilots as you can have at a time")

// ParsePilotName validates a named pilot's name: 1 to 30 characters, in the same style as an account or force name.
func ParsePilotName(raw string) (string, error) {
	name, err := ParseName(raw, "pilot")
	if err != nil || utf8.RuneCountInString(name) > 30 {
		return "", errors.New("Give your pilot a name of 1 to 30 letters, numbers and spaces (apostrophes, hyphens and a few marks are fine).")
	}
	return name, nil
}
