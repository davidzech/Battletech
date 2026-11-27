package mekbay

// RemoveUnitFromForce takes the unit with this ID out of a saved force (the decoded JSON of its `data`); false if it isn't there.
func RemoveUnitFromForce(force map[string]any, unitID string) bool {
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		units, _ := gm["units"].([]any)
		for i, u := range units {
			if um, _ := u.(map[string]any); um != nil && um["id"] == unitID {
				gm["units"] = append(units[:i:i], units[i+1:]...)
				return true
			}
		}
	}
	return false
}
