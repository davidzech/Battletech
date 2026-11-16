package site

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// Named pilots follow the ruleset in BattleTech: Hot Spots - Draconis Reach, "Named Pilots" (pp. 34-35). Only named
// pilots improve their skills, use Edge and earn Edge abilities. Each earned SP is allocated to one of the pilot's
// columns (Gunnery, Piloting, Edge tokens, Edge abilities), and a column's level is however far its cumulative SP
// reaches along that column's ladder in the book's Named Pilot Improvement Table.

// maxNamedPilots is how many named pilots a command may have at once (a fallen pilot doesn't count).
const maxNamedPilots = 4

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
	edgeAbilitiesLadder = ladder{"edge_abilities", "Edge abilities", 0, []ladderRow{{1, 60}, {2, 180}, {3, 360}, {4, 600}, {5, 900}}}
)

// at is the step a column has reached with sp allocated to it: the highest row it can afford, or the base level
// if none.
func (l ladder) at(sp int) ladderRow {
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

// laddersJSON is the four ladders as JSON, handed to the campaign page so the pilot builder can preview levels with
// exactly the rules the server uses.
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

var laddersJSON = func() string {
	var views []ladderView
	for _, l := range []ladder{gunneryLadder, pilotingLadder, edgeTokensLadder, edgeAbilitiesLadder} {
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

// NamedPilot is one of a command's named pilots. Their SP lives only as allocations, because the book has a pilot
// allocate every SP the moment they earn it, so the "total SP earned" is just the sum.
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
}

// Earned is the pilot's total SP earned, allocated or not.
func (p NamedPilot) Earned() int { return p.TotalSP }

// Unallocated is the earned SP not yet allocated to a column.
func (p NamedPilot) Unallocated() int { return max(0, p.TotalSP-p.SP.Total()) }

// Gunnery is the pilot's current Gunnery skill (lower is better).
func (p NamedPilot) Gunnery() int { return gunneryLadder.at(p.SP.Gunnery).Level }

// Piloting is the pilot's current Piloting skill (lower is better).
func (p NamedPilot) Piloting() int { return pilotingLadder.at(p.SP.Piloting).Level }

// EdgeTokens is how many Edge tokens the pilot starts each sortie with.
func (p NamedPilot) EdgeTokens() int { return edgeTokensLadder.at(p.SP.EdgeTokens).Level }

// EdgeAbilities is how many Edge abilities the pilot has earned.
func (p NamedPilot) EdgeAbilities() int { return edgeAbilitiesLadder.at(p.SP.EdgeAbilities).Level }

// pilotColumn is one column of a pilot as the campaign page shows it.
type pilotColumn struct {
	Name    string
	Label   string // the (possibly shorter) name for the column's stat box
	SP      int    // SP allocated to this column
	Level   int    // level reached
	HasNext bool   // false at the top of the ladder
	NextAt  int    // SP the column needs in total to reach the next level
	NextLvl int
	Percent int // how far along to the next level, 0 to 100 (100 at the top of the ladder)
}

// PilotView is a named pilot with everything the campaign page shows already worked out.
type PilotView struct {
	ID, Name                                 string
	Portrait, PortraitURL                    string // the file picked (or worked out) and where it is served
	Rank                                     string // the pilot's rank (the lowest if none was chosen); "" when the campaign has no ranks
	Fallen                                   bool
	Gunnery, Piloting, EdgeTokens, Abilities int
	Earned, Unallocated, Allocated           int // total SP earned, the part not yet in a column, and the part that is
	Columns                                  []pilotColumn
}

func (p NamedPilot) view(ranks []string) PilotView {
	col := func(l ladder, sp int) pilotColumn {
		c := pilotColumn{Name: l.Name, Label: boxLabel(l), SP: sp, Level: l.at(sp).Level}
		c.Percent = 100
		if n, ok := l.next(sp); ok {
			c.HasNext, c.NextAt, c.NextLvl = true, n.SP, n.Level
			c.Percent = min(100, (sp*100+n.SP/2)/n.SP) // rounded, like the browser's
		}
		return c
	}
	return PilotView{
		ID: p.ID, Name: p.Name, Rank: rankFor(ranks, p.Rank), Fallen: p.Fallen, Portrait: portraitOf(p.ID, p.Portrait), PortraitURL: portraitURL(portraitOf(p.ID, p.Portrait)),
		Gunnery: p.Gunnery(), Piloting: p.Piloting(), EdgeTokens: p.EdgeTokens(), Abilities: p.EdgeAbilities(),
		Earned: p.Earned(), Unallocated: p.Unallocated(), Allocated: p.SP.Total(),
		Columns: []pilotColumn{
			col(gunneryLadder, p.SP.Gunnery), col(pilotingLadder, p.SP.Piloting),
			col(edgeTokensLadder, p.SP.EdgeTokens), col(edgeAbilitiesLadder, p.SP.EdgeAbilities),
		},
	}
}

// pilotViews prepares a campaign's pilots for display and counts how many are still alive.
func pilotViews(pilots []NamedPilot, ranks []string) (views []PilotView, alive int) {
	for _, p := range pilots {
		views = append(views, p.view(ranks))
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
}

// ErrPortraitTaken is returned when another living pilot of the campaign already has the portrait.
var ErrPortraitTaken = errors.New("another pilot already has that portrait")

// ErrPilotLimit is returned when a command already has the most named pilots it may have.
var ErrPilotLimit = errors.New("you can have at most four named pilots at a time")

// parsePilotName validates a named pilot's name: 1 to 30 characters, in the same style as a company name.
func parsePilotName(raw string) (string, error) {
	name, err := parseCompanyName(raw)
	if err != nil || utf8.RuneCountInString(name) > 30 {
		return "", errors.New("Give your pilot a name of 1 to 30 letters, numbers and spaces (apostrophes, hyphens and a few marks are fine).")
	}
	return name, nil
}
