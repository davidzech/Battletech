package site

import (
	"fmt"
	"sort"
	"strings"
)

// The state a hangar unit is in, for the pills on its row. MekBay stores a unit's raw damage (armor and internal structure points lost
// per location, critical hit slots, ammo consumed, the pilot's hits, and a destroyed flag); there is no stored "crippled", so that is
// worked out here from the book's own crippling rules. The unit shows the worst state it is in: destroyed beats crippled beats internal
// structure damage beats armor damage. Ammo is tracked beside it, since rearming is a different need from repair.

// Condition is the worst state a unit is in.
type Condition string

const (
	CondOK        Condition = "ok"
	CondArmor     Condition = "armor"     // armor lost, no internal structure lost
	CondStructure Condition = "structure" // internal structure lost somewhere
	CondCrippled  Condition = "crippled"  // meets one of the crippling rules below
	CondDestroyed Condition = "destroyed" // MekBay's destroyed flag
)

// LocationLoss is what a location has lost: armor and internal structure points.
type LocationLoss struct {
	Armor    int
	Internal int
}

// structureByTons is the standard BattleMech internal structure by tonnage: head, center torso, each side torso, each arm, each leg.
// (Endo Steel and the like change weight, not points.)
var structureByTons = map[int][5]int{
	10: {3, 4, 3, 1, 2}, 15: {3, 5, 4, 2, 3}, 20: {3, 6, 5, 3, 4}, 25: {3, 8, 6, 4, 6}, 30: {3, 10, 7, 5, 7},
	35: {3, 11, 8, 6, 8}, 40: {3, 12, 10, 6, 10}, 45: {3, 14, 11, 7, 11}, 50: {3, 16, 12, 8, 12}, 55: {3, 18, 13, 9, 13},
	60: {3, 20, 14, 10, 14}, 65: {3, 21, 15, 10, 15}, 70: {3, 22, 15, 11, 15}, 75: {3, 23, 16, 12, 16}, 80: {3, 25, 17, 13, 17},
	85: {3, 27, 18, 14, 18}, 90: {3, 29, 19, 15, 19}, 95: {3, 30, 20, 16, 20}, 100: {3, 31, 21, 17, 21},
}

var (
	armKeys   = []string{"LA", "RA"}
	legKeys   = []string{"LL", "RL", "FLL", "FRL", "RLL", "RRL"}
	torsoKeys = []string{"LT", "RT", "CT"}
)

// maxStructure is the internal structure a location starts with at a tonnage, 0 if unknown.
func maxStructure(loc string, tons float64) int {
	t, ok := structureByTons[int(tons)]
	if !ok {
		return 0
	}
	switch loc {
	case "HD":
		return t[0]
	case "CT":
		return t[1]
	case "LT", "RT":
		return t[2]
	case "LA", "RA":
		return t[3]
	}
	return t[4] // legs
}

// internalGone reports whether a location has lost all its internal structure.
func internalGone(hu HangarUnit, loc string, tons float64) bool {
	max := maxStructure(loc, tons)
	return max > 0 && hu.Locations[loc].Internal >= max
}

// structureKeys are the locations whose internal structure counts as structure damage (rear armor locations have none).
var structureKeys = []string{"HD", "CT", "LT", "RT", "LA", "RA", "LL", "RL", "FLL", "FRL", "RLL", "RRL"}

// conditionOf works out a unit's worst state and a short note saying why (for the pill's tooltip). tons and engine come from the
// catalog (0 and "" if unknown).
func conditionOf(hu HangarUnit, tons float64, engine string) (Condition, string) {
	if hu.Destroyed {
		return CondDestroyed, "Destroyed"
	}
	if why := crippledBecause(hu, tons, engine); why != "" {
		return CondCrippled, "Crippled: " + why
	}
	var hurt []string
	for _, k := range structureKeys {
		if hu.Locations[k].Internal > 0 {
			hurt = append(hurt, k)
		}
	}
	if len(hurt) > 0 {
		return CondStructure, "Internal structure damaged: " + strings.Join(hurt, ", ")
	}
	var scuffed []string
	for k, l := range hu.Locations {
		if l.Armor > 0 {
			scuffed = append(scuffed, k)
		}
	}
	if len(scuffed) > 0 {
		sort.Strings(scuffed)
		return CondArmor, "Armor damaged: " + strings.Join(scuffed, ", ")
	}
	return CondOK, "No damage"
}

// crippledBecause mirrors MekBay's own Mek crippling rule (mek-rules.ts, with forced withdrawal on, its default): two engine hits; two
// destroyed limbs, at least one a leg; two destroyed torsos; or one destroyed torso when the engine needs no crippling check (anything but
// Fusion or Compact) or MekBay's stored torso check failed. Returns the reason, or "".
func crippledBecause(hu HangarUnit, tons float64, engine string) string {
	if hu.EngineHits >= 2 {
		return "two engine hits"
	}
	limbs, legs := 0, 0
	for _, k := range armKeys {
		if internalGone(hu, k, tons) {
			limbs++
		}
	}
	for _, k := range legKeys {
		if internalGone(hu, k, tons) {
			limbs++
			legs++
		}
	}
	if limbs >= 2 && legs >= 1 {
		return fmt.Sprintf("%d limbs destroyed, including a leg", limbs)
	}
	torsos := 0
	for _, k := range torsoKeys {
		if internalGone(hu, k, tons) {
			torsos++
		}
	}
	switch {
	case torsos >= 2:
		return "two torsos destroyed"
	case torsos == 1:
		e := strings.ToLower(strings.TrimSpace(engine))
		if (e != "fusion" && e != "compact") || hu.TorsoCheckFailed {
			return "torso destroyed"
		}
	}
	return ""
}
