package site

import (
	"fmt"
	"strings"
)

// Names of the two groups a deployed hangar is sorted into. MekBay can't load part of a force, so the units
// going on the mission sit in "Deployed" and everything else waits in "Hangar".
const (
	deployedGroup = "Deployed"
	hangarGroup   = "Hangar"
)

// sameGroupName compares group names the way the site reads them: ignoring case and surrounding space.
func sameGroupName(name, want string) bool {
	return strings.EqualFold(strings.TrimSpace(name), want)
}

// deployGroups rearranges a saved MekBay force (the decoded JSON of its `data`) so the units whose IDs are in
// deployed form the "Deployed" group and every other unit the "Hangar" group. Existing groups of those names are
// reused, keeping their ID and color; any other groups are dissolved. Everything else in the force (units'
// damage, crews, networks, ...) is left exactly as it was. Empty groups are left out, as MekBay does.
func deployGroups(force map[string]any, deployed map[string]bool, newID func() string) error {
	groups, _ := force["groups"].([]any)
	var deployedG, hangarG map[string]any
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := gm["name"].(string)
		switch {
		case deployedG == nil && sameGroupName(name, deployedGroup):
			deployedG = gm
		case hangarG == nil && sameGroupName(name, hangarGroup):
			hangarG = gm
		}
	}
	fresh := func(name string) map[string]any {
		return map[string]any{"id": newID(), "name": name, "color": ""}
	}
	if deployedG == nil {
		deployedG = fresh(deployedGroup)
	}
	if hangarG == nil {
		hangarG = fresh(hangarGroup)
	}

	var inDeployed, inHangar []any
	seen := map[string]bool{}
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		units, _ := gm["units"].([]any)
		for _, u := range units {
			um, ok := u.(map[string]any)
			if !ok {
				continue
			}
			id, _ := um["id"].(string)
			seen[id] = true
			if deployed[id] {
				inDeployed = append(inDeployed, um)
			} else {
				inHangar = append(inHangar, um)
			}
		}
	}
	for id := range deployed {
		if !seen[id] {
			return fmt.Errorf("unit %s is not in the hangar force", id)
		}
	}

	deployedG["units"], hangarG["units"] = inDeployed, inHangar
	var out []any
	kept := map[string]bool{}
	for _, g := range []map[string]any{deployedG, hangarG} {
		if len(g["units"].([]any)) > 0 {
			out = append(out, g)
			id, _ := g["id"].(string)
			kept[id] = true
		}
	}
	// A kept group must not point its formation at a group that no longer exists.
	for _, g := range out {
		gm := g.(map[string]any)
		if target, ok := gm["formationTargetGroupId"].(string); ok && !kept[target] {
			delete(gm, "formationTargetGroupId")
		}
	}
	if out == nil {
		out = []any{}
	}
	force["groups"] = out
	return nil
}
