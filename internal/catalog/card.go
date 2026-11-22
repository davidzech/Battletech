package catalog

import (
	"strings"
)

// UnitCard is what a unit's hover card shows (static/unitcard.js), on a scenario's unit links and on the overview's unit icons (hangarUnitView.Card).
type UnitCard struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`  // the Clan name when the design has one
	Model  string  `json:"model"` // e.g. "Prime"
	Icon   string  `json:"icon"`
	Class  string  `json:"class"`
	Role   string  `json:"role"`
	Tons   float64 `json:"tons"`
	Tech   string  `json:"tech"`
	BV     int     `json:"bv"` // base BV
	Engine string  `json:"engine,omitempty"`
	Omni   bool    `json:"omni,omitempty"`
	// The mech's construction, the card's second column: armor and internal structure points and their types, heat generated and dissipated, and
	// Walk/Run/Jump movement points.
	Armor         int           `json:"armor,omitempty"`
	ArmorType     string        `json:"armorType,omitempty"`
	Structure     int           `json:"structure,omitempty"`
	StructureType string        `json:"structureType,omitempty"`
	Heat          int           `json:"heat,omitempty"`
	Dissipation   int           `json:"dissipation,omitempty"`
	Walk          int           `json:"walk,omitempty"`
	Run           int           `json:"run,omitempty"`
	Jump          int           `json:"jump"`
	Firepower     int           `json:"firepower,omitempty"` // every weapon fired at once
	MaxRange      int           `json:"maxRange,omitempty"`  // the longest weapon range, in hexes
	Range         int           `json:"range,omitempty"`     // the range weighted by each weapon's damage
	Weapons       *WeaponCounts `json:"weapons,omitempty"`   // how many ballistic, energy and missile weapons it mounts (mechs only)
	Gunnery       int           `json:"gunnery,omitempty"`   // from "(3/4)" after the link; 0 when none was written
	Piloting      int           `json:"piloting,omitempty"`
	SkillBV       int           `json:"skillBV,omitempty"` // the BV at those skills
}

// WeaponCounts is how many weapons of each type a mech mounts, for the card's weapon type bar.
type WeaponCounts struct {
	Ballistic int `json:"ballistic"`
	Energy    int `json:"energy"`
	Missile   int `json:"missile"`
}

// CardFor is the card of a catalog unit, without any pilot's skills.
func CardFor(u Unit) *UnitCard {
	name, model := Names(u)
	card := &UnitCard{ID: u.Name, Name: name, Model: model, Icon: IconURL(u), Class: u.Class, Role: u.Role, Tons: u.Tons, Tech: u.TechBase,
		BV: u.BV, Engine: u.Engine, Omni: u.Omni == 1,
		Armor: u.Armor, ArmorType: strings.TrimSuffix(u.ArmorType, " Armor"), Structure: u.Internal, StructureType: u.StructureType,
		Heat: u.Heat, Dissipation: u.Dissipation, Walk: u.Walk, Run: u.Run, Jump: u.Jump,
		Firepower: u.Firepower, MaxRange: u.MaxRange, Range: u.Range}
	if u.Type == "Mek" {
		card.Weapons = &WeaponCounts{u.Ballistic, u.Energy, u.Missile}
	}
	return card
}
