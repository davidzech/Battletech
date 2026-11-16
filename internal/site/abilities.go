package site

import "encoding/json"

// EdgeAbility is one of the Edge abilities a named pilot can learn: BattleTech: Hot Spots - Draconis Reach, "Edge Abilities",
// printed p. 166. The book says that when a named pilot reaches a new level of Edge abilities they choose one of these, and that the
// choice is permanent. The text is the Alpha Strike rule, with the Total Warfare rule in parentheses, as the book prints it. This
// site only records which abilities a pilot has; it doesn't play them.
type EdgeAbility struct {
	ID       string `json:"id"`       // stable identifier stored with a pilot
	Name     string `json:"name"`     // e.g. "Marksman"
	Category string `json:"category"` // the book's heading: Attack, Indirect Fire, Defense, Heat, Melee or Movement
	Text     string `json:"text"`
}

// edgeAbilities are all of them, in the order the book lists them.
var edgeAbilities = []EdgeAbility{
	{"assassin", "Assassin", "Attack", `Before rolling to attack a target in its rear arc: Spend 1 Edge to increase your damage rating by 1 (in addition to the bonus 1 damage for attacking the target in the rear). (TW: You may add or subtract 1 from your hit location roll if the result would be rear torso armor.)`},
	{"forward_observer", "Forward Observer", "Indirect Fire", `When spotting: Spend 1 Edge to also attack this Combat Phase without incurring the +1 penalty to the Target Number for you or any ally benefitting from your spotting.`},
	{"marksman", "Marksman", "Attack", `After rolling for an attack: spend 1 Edge to cause one attack roll of 11 that hits to also inflict a critical hit. (TW: Add 1 to your roll on the Determining Critical Hits Table.)`},
	{"jumping_jack", "Jumping Jack", "Attack", `When attacking a target at Short range, if you used jumping movement this turn: Spend 1 Edge to reduce your Target Number penalty for jumping to +1 (not +2). (TW: to +2 instead of +3).`},
	{"patient", "Patient", "Attack", `After rolling for an attack, if you used Standstill movement (TW: did not move) this turn: spend 1 Edge to reroll all of your attacks.`},
	{"cautious", "Cautious", "Defense", `At the start of the Combat Phase: spend 1 Edge to change your facing to any direction.`},
	{"protector", "Protector", "Defense", `When a friendly unit within 2" (TW: 1 hex) takes damage, if it is a smaller size/tonnage than you: Spend 1 Edge to prevent 1 damage (TW: the damage from that weapon) to that unit.`},
	{"coolant_flush", "Coolant Flush", "Heat", `During the End Phase: Spend up to 2 Edge to reduce your heat by the same amount (TW: reduce heat by 4 per Edge).`},
	{"bulwark", "Bulwark", "Melee", `When you take damage from a Physical Attack (including self-inflicted damage from a DFA or Charge): Spend 1 Edge to reduce the damage you receive by 1 (TW: 5).`},
	{"melee_specialist", "Melee Specialist", "Melee", `After rolling for a Physical or MEL attack: Spend 1 Edge to reroll.`},
	{"nimble", "Nimble", "Movement", `When using ground or sprint movement: Spend 1 Edge to gain +1 TMM, and you may move through enemy units this Movement Phase. If your sprinted TMM is 3 or more (TW: Walking MP 7+), this ability costs 2 Edge.`},
	{"speed_demon", "Speed Demon", "Movement", `When using ground (or sprint) movement: Spend 1 Edge to gain +2" (or +4") movement this turn (TW: 1 MP, 2 MP if Sprinting).`},
}

var edgeAbilityByIDMap = func() map[string]EdgeAbility {
	m := map[string]EdgeAbility{}
	for _, a := range edgeAbilities {
		m[a.ID] = a
	}
	return m
}()

// edgeAbilityByID looks an ability up by its stable ID.
func edgeAbilityByID(id string) (EdgeAbility, bool) {
	a, ok := edgeAbilityByIDMap[id]
	return a, ok
}

// edgeAbilityList turns learned IDs into abilities, in the order given, skipping any the book no longer lists.
func edgeAbilityList(ids []string) []EdgeAbility {
	var out []EdgeAbility
	for _, id := range ids {
		if a, ok := edgeAbilityByID(id); ok {
			out = append(out, a)
		}
	}
	return out
}

// edgeAbilitiesJSON is the catalog as JSON, for the picker on the Manage page.
var edgeAbilitiesJSON = func() string {
	b, err := json.Marshal(edgeAbilities)
	if err != nil {
		panic(err)
	}
	return string(b)
}()
