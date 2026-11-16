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

// ladderRow is one step of an improvement ladder: reaching Level costs SP in total (cumulative, not per step) and
// adds Handicap (that level's handicap alone, not added to earlier steps').
type ladderRow struct {
	Level    int `json:"level"`
	Handicap int `json:"handicap"`
	SP       int `json:"sp"`
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
	gunneryLadder    = ladder{"gunnery", "Gunnery", 4, []ladderRow{{3, 12, 300}, {2, 28, 700}, {1, 48, 1200}, {0, 88, 2200}}}
	pilotingLadder   = ladder{"piloting", "Piloting", 5, []ladderRow{{4, 4, 100}, {3, 8, 200}, {2, 28, 700}, {1, 48, 1200}}}
	edgeTokensLadder = ladder{"edge_tokens", "Edge tokens", 1, []ladderRow{
		{2, 2, 60}, {3, 5, 120}, {4, 8, 200}, {5, 12, 300}, {6, 17, 420}, {7, 22, 560}, {8, 29, 720}, {9, 36, 900}, {10, 44, 1100},
	}}
	edgeAbilitiesLadder = ladder{"edge_abilities", "Edge abilities", 0, []ladderRow{{1, 2, 60}, {2, 8, 180}, {3, 14, 360}, {4, 24, 600}, {5, 36, 900}}}
)

// at is the step a column has reached with sp allocated to it: the highest row it can afford, or the base level
// (with no handicap) if none.
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
var laddersJSON = func() string {
	b, err := json.Marshal([]ladder{gunneryLadder, pilotingLadder, edgeTokensLadder, edgeAbilitiesLadder})
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
	ID     string
	Name   string
	Fallen bool // died, and is permanently removed from the campaign (kept so the record isn't lost)
	SP     SPAllocation
}

// Earned is the pilot's total SP earned.
func (p NamedPilot) Earned() int { return p.SP.Total() }

// Gunnery is the pilot's current Gunnery skill (lower is better).
func (p NamedPilot) Gunnery() int { return gunneryLadder.at(p.SP.Gunnery).Level }

// Piloting is the pilot's current Piloting skill (lower is better).
func (p NamedPilot) Piloting() int { return pilotingLadder.at(p.SP.Piloting).Level }

// EdgeTokens is how many Edge tokens the pilot starts each sortie with.
func (p NamedPilot) EdgeTokens() int { return edgeTokensLadder.at(p.SP.EdgeTokens).Level }

// EdgeAbilities is how many Edge abilities the pilot has earned.
func (p NamedPilot) EdgeAbilities() int { return edgeAbilitiesLadder.at(p.SP.EdgeAbilities).Level }

// Handicap is the pilot's handicap: the sum of the handicaps of the step each column has reached. Commands compare
// their pilots' average handicap with their opponent's to hand the weaker side extra points.
func (p NamedPilot) Handicap() int {
	return gunneryLadder.at(p.SP.Gunnery).Handicap + pilotingLadder.at(p.SP.Piloting).Handicap +
		edgeTokensLadder.at(p.SP.EdgeTokens).Handicap + edgeAbilitiesLadder.at(p.SP.EdgeAbilities).Handicap
}

// pilotColumn is one column of a pilot as the campaign page shows it.
type pilotColumn struct {
	Name    string
	SP      int  // SP allocated to this column
	Level   int  // level reached
	HasNext bool // false at the top of the ladder
	NextAt  int  // SP the column needs in total to reach the next level
	NextLvl int
}

// PilotView is a named pilot with everything the campaign page shows already worked out.
type PilotView struct {
	ID, Name                                 string
	Fallen                                   bool
	Gunnery, Piloting, EdgeTokens, Abilities int
	Handicap, Earned                         int
	Columns                                  []pilotColumn
}

func (p NamedPilot) view() PilotView {
	col := func(l ladder, sp int) pilotColumn {
		c := pilotColumn{Name: l.Name, SP: sp, Level: l.at(sp).Level}
		if n, ok := l.next(sp); ok {
			c.HasNext, c.NextAt, c.NextLvl = true, n.SP, n.Level
		}
		return c
	}
	return PilotView{
		ID: p.ID, Name: p.Name, Fallen: p.Fallen,
		Gunnery: p.Gunnery(), Piloting: p.Piloting(), EdgeTokens: p.EdgeTokens(), Abilities: p.EdgeAbilities(),
		Handicap: p.Handicap(), Earned: p.Earned(),
		Columns: []pilotColumn{
			col(gunneryLadder, p.SP.Gunnery), col(pilotingLadder, p.SP.Piloting),
			col(edgeTokensLadder, p.SP.EdgeTokens), col(edgeAbilitiesLadder, p.SP.EdgeAbilities),
		},
	}
}

// pilotViews prepares a campaign's pilots for display and counts how many are still alive.
func pilotViews(pilots []NamedPilot) (views []PilotView, alive int) {
	for _, p := range pilots {
		views = append(views, p.view())
		if !p.Fallen {
			alive++
		}
	}
	return views, alive
}

// NewPilot is a pilot to create along with a campaign run: a name and the SP allocated at the start.
type NewPilot struct {
	Name string
	SP   SPAllocation
}

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
