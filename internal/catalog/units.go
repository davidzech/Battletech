package catalog

import (
	_ "embed"
	"encoding/json"
	"math"
	"sort"
	"sync"
)

//go:generate go run gen_units.go gen_common.go

// unitsIndexJSON is the slice of MekBay's unit catalog the site needs (see gen_units.go). The full
// data/units.json is not shipped in the Docker image, so this is embedded instead.
//
//go:embed units_index.json
var unitsIndexJSON []byte

// AmmoBin is one ammo entry of a unit: the bins of one ammo type in one location (Slots tons, Shots shots in all of them).
type AmmoBin struct {
	ID    string `json:"id"` // MekBay's name for the ammo, as it appears on a critical slot
	Name  string `json:"n"`  // e.g. "LRM 5 Ammo"
	Loc   string `json:"l"`
	Slots int    `json:"q"`
	Shots int    `json:"s"`
}

// Unit is one catalog entry: Name is MekBay's unit ID (e.g. BMMadCat_H), which is what a saved force
// stores in each unit's `unit` field.
type Unit struct {
	Name     string    `json:"name"`
	Chassis  string    `json:"chassis"`
	Model    string    `json:"model"`
	BV       int       `json:"bv"` // base battle value, before pilot skills
	Tons     float64   `json:"tons"`
	Class    string    `json:"weightClass"` // Light, Medium, Heavy, Assault...
	Role     string    `json:"role"`        // Master Unit List role
	Type     string    `json:"type"`        // Mek, Tank, ProtoMek...
	TechBase string    `json:"techBase"`    // Clan, Inner Sphere, Mixed
	Omni     int       `json:"omni"`        // 1 for an omnimech: its variants can be swapped (see swap.go)
	Engine   string    `json:"engine"`      // Fusion, XL (Clan), Light, ICE...
	Ammo     []AmmoBin `json:"ammo"`        // the unit's ammunition, for the rearm breakdown
	Icon     string    `json:"icon"`        // file name in icons/ (mechs, vehicles and battle armor; "" for anything else)
	// A mech's construction (mechs only, 0 or "" for anything else): armor and internal structure points in all and their types, the heat generated
	// firing everything and the heat sunk per turn, and Walk/Run/Jump movement points.
	Armor         int    `json:"armor"`
	ArmorType     string `json:"armorType"`
	Internal      int    `json:"internal"`
	StructureType string `json:"structureType"`
	Heat          int    `json:"heat"`
	Dissipation   int    `json:"dissipation"`
	Walk          int    `json:"walk"`
	Run           int    `json:"run"`
	Jump          int    `json:"jump"`
	// Firepower (every weapon fired at once, physical attacks left out), MaxRange (the longest weapon range in hexes) and Range (weighted by
	// damage), worked out by gen_units.go the way MekBay's stat bars are.
	Firepower int `json:"firepower"`
	MaxRange  int `json:"maxRange"`
	Range     int `json:"range"`
	// Ballistic, Energy and Missile count the mech's weapons of each type (gen_units.go).
	Ballistic int `json:"ballistic"`
	Energy    int `json:"energy"`
	Missile   int `json:"missile"`
	// CockpitLoc is where a mech's cockpit is when it is not in the head ("CT" for a torso-mounted cockpit); "" for a head cockpit (gen_units.go).
	CockpitLoc string `json:"cockpitLoc"`
}

var (
	unitsOnce   sync.Once
	unitsByCM   map[[2]string]Unit
	unitsByName map[string]Unit
)

func loadUnits() {
	unitsOnce.Do(func() {
		var all []Unit
		if err := json.Unmarshal(unitsIndexJSON, &all); err != nil {
			panic("units_index.json: " + err.Error())
		}
		unitsByCM = make(map[[2]string]Unit, len(all))
		unitsByName = make(map[string]Unit, len(all))
		for _, u := range all {
			k := [2]string{u.Chassis, u.Model}
			if _, dup := unitsByCM[k]; !dup {
				unitsByCM[k] = u
			}
			unitsByName[u.Name] = u
		}
	})
}

// Lookup finds a unit by the chassis and model strings a MegaMek/MekBay .mul file uses,
// e.g. ("Mad Cat (Timber Wolf)", "H") or ("Grizzly", "").
func Lookup(chassis, model string) (Unit, bool) {
	loadUnits()
	u, ok := unitsByCM[[2]string{chassis, model}]
	return u, ok
}

// ByName finds a unit by its MekBay unit ID, e.g. "BMMadCat_H" (what a saved force stores).
func ByName(name string) (Unit, bool) {
	loadUnits()
	u, ok := unitsByName[name]
	return u, ok
}

// bvSkillMatrix is MekBay's BV 2.0 skill multiplier table (bv-calculator.util.ts), indexed [gunnery][piloting].
var bvSkillMatrix = [9][9]float64{
	{2.42, 2.31, 2.21, 2.10, 1.93, 1.75, 1.68, 1.59, 1.50},
	{2.21, 2.11, 2.02, 1.92, 1.76, 1.60, 1.54, 1.46, 1.38},
	{1.93, 1.85, 1.76, 1.68, 1.54, 1.40, 1.35, 1.28, 1.21},
	{1.66, 1.58, 1.51, 1.44, 1.32, 1.20, 1.16, 1.10, 1.04},
	{1.38, 1.32, 1.26, 1.20, 1.10, 1.00, 0.95, 0.90, 0.85},
	{1.31, 1.19, 1.13, 1.08, 0.99, 0.90, 0.86, 0.81, 0.77},
	{1.24, 1.12, 1.07, 1.02, 0.94, 0.85, 0.81, 0.77, 0.72},
	{1.17, 1.06, 1.01, 0.96, 0.88, 0.80, 0.76, 0.72, 0.68},
	{1.10, 0.99, 0.95, 0.90, 0.83, 0.75, 0.71, 0.68, 0.64},
}

// AdjustedBV is a unit's battle value with its pilot's skills applied, rounded the way MekBay does.
func AdjustedBV(base, gunnery, piloting int) int {
	clamp := func(n int) int { return max(0, min(8, n)) }
	return int(math.Round(float64(base) * bvSkillMatrix[clamp(gunnery)][clamp(piloting)]))
}

// All lists every unit in the catalog, sorted by MekBay unit ID.
func All() []Unit {
	loadUnits()
	all := make([]Unit, 0, len(unitsByName))
	for _, u := range unitsByName {
		all = append(all, u)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}
