package mekbay

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/davidzech/Battletech/internal/catalog"
)

// Editing a force saved in MekBay's version 2 format (see v2.go): the same edits the version 1 functions make, on version 2 rows. Each exported edit picks the
// version 1 or version 2 way by the force's own version, so callers need not know which a hangar is.
//
// A unit's ID inside the site is its unpacked instance ID (unpackInstanceID): a version 1 unit keeps its ID through the conversion, and a new unit's is a plain
// UUID, packed as MekBay packs one.

// v2Units is a version 2 force's unit rows.
func v2Units(force map[string]any) []map[string]any {
	raw, _ := force["units"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, u := range raw {
		if um, ok := u.(map[string]any); ok {
			out = append(out, um)
		}
	}
	return out
}

// v2Find finds the unit row with this site ID and its index.
func v2Find(force map[string]any, unitID string) (int, map[string]any) {
	raw, _ := force["units"].([]any)
	for i, u := range raw {
		if um, ok := u.(map[string]any); ok {
			if id, _ := um["id"].(string); unpackInstanceID(id) == unitID {
				return i, um
			}
		}
	}
	return -1, nil
}

func v2UnitID(um map[string]any) string {
	id, _ := um["id"].(string)
	return unpackInstanceID(id)
}

func v2Design(um map[string]any) string {
	p, _ := um["uuid"].(string)
	u, _ := unpackUUID(p)
	return u
}

// v2Person is a crew member in version 2 form (packPerson): skills only when they aren't the default 4/5.
func v2Person(name string, gunnery, piloting int, commander bool) map[string]any {
	p := map[string]any{"id": packOpaqueID(NewID())}
	setPerson(p, name, gunnery, piloting, commander)
	return p
}

func setPerson(p map[string]any, name string, gunnery, piloting int, commander bool) {
	for _, k := range []string{"name", "g", "p", "commander", "edge", "health"} {
		delete(p, k)
	}
	if name != "" {
		p["name"] = name
	}
	if gunnery != DefaultGunnery {
		p["g"] = gunnery
	}
	if piloting != DefaultPiloting {
		p["p"] = piloting
	}
	if commander {
		p["commander"] = true
	}
}

// maxEdge is the most Edge MekBay accepts on a person (FORCE_PERSON_EDGE_MAX).
const maxEdge = 99

// setEdge gives a person Edge tokens; MekBay leaves the field out for none. setPerson clears it.
func setEdge(p map[string]any, edge int) {
	if edge > 0 {
		p["edge"] = min(edge, maxEdge)
	}
}

// v2Pilot is a unit row's pilot (its first crew station), made if it has none.
func v2Pilot(um map[string]any) map[string]any {
	crew, _ := um["crew"].([]any)
	if len(crew) > 0 {
		if p, ok := crew[0].(map[string]any); ok {
			return p
		}
	}
	p := v2Person("", DefaultGunnery, DefaultPiloting, false)
	um["crew"] = append([]any{p}, crew...)
	return p
}

// v2UnitRow is a pristine unit of a design (a catalog unit's MegaMek UUID), with this site ID and pilot.
func v2UnitRow(id, design string, pilot map[string]any) (map[string]any, error) {
	p, err := packUUID(design)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": packInstanceID(id), "uuid": p, "crew": []any{pilot}}, nil
}

// designUUID is a catalog unit's MegaMek UUID.
func designUUID(unit string) (string, error) {
	cu, ok := catalog.ByName(unit)
	if !ok || cu.UUID == "" {
		return "", fmt.Errorf("unit %s is not in the catalog", unit)
	}
	return cu.UUID, nil
}

// v2Group is a group of a version 2 force (packGroup).
func v2Group(name string, indices []int) map[string]any {
	g := map[string]any{"id": packOpaqueID(NewID()), "unitIndices": indices}
	if name != "" {
		g["name"] = name
	}
	return g
}

// newV2Force is an empty version 2 force record.
func newV2Force(instance, name string, now time.Time) map[string]any {
	return map[string]any{"version": 2, "type": "cbt", "instanceId": instance, "name": name, "timestamp": now.UnixMilli(),
		"units": []any{}, "groups": []any{}, "cbt": map[string]any{"r": 0}}
}

// BuildForceV2 is BuildForce in version 2: a pristine force from a draft.
func BuildForceV2(instanceID string, now time.Time, d ForceDraft) (map[string]any, error) {
	f := newV2Force(instanceID, d.Name, now)
	var units, groups []any
	bv := 0
	for _, g := range d.Groups {
		var idx []int
		for _, u := range g.Units {
			design, err := designUUID(u.Unit)
			if err != nil {
				return nil, err
			}
			pilot := v2Person(u.Pilot, u.Gunnery, u.Piloting, u.Commander)
			if u.Pilot != "" {
				setEdge(pilot, u.Edge)
			}
			row, err := v2UnitRow(NewID(), design, pilot)
			if err != nil {
				return nil, err
			}
			idx = append(idx, len(units))
			units = append(units, row)
			bv += u.BV
		}
		groups = append(groups, v2Group(g.Name, idx))
	}
	f["units"], f["groups"], f["bv"] = orEmpty(units), orEmpty(groups), bv
	return f, nil
}

func orEmpty(a []any) []any {
	if a == nil {
		return []any{}
	}
	return a
}

// v2AddUnits adds a pristine unit of each catalog unit to the "Hangar" group, making the group if it isn't there.
func v2AddUnits(force map[string]any, units []string) error {
	rows, _ := force["units"].([]any)
	groups, _ := force["groups"].([]any)
	var hangar map[string]any
	for _, g := range groups {
		if gm, ok := g.(map[string]any); ok {
			if name, _ := gm["name"].(string); hangar == nil && SameGroupName(name, HangarGroup) {
				hangar = gm
			}
		}
	}
	if hangar == nil {
		hangar = v2Group(HangarGroup, nil)
		groups = append(groups, hangar)
	}
	idx := ints(hangar["unitIndices"])
	for _, u := range units {
		design, err := designUUID(u)
		if err != nil {
			return err
		}
		row, err := v2UnitRow(NewID(), design, v2Person("", DefaultGunnery, DefaultPiloting, false))
		if err != nil {
			return err
		}
		idx = append(idx, len(rows))
		rows = append(rows, row)
	}
	hangar["unitIndices"] = idx
	force["units"], force["groups"] = rows, groups
	return nil
}

func ints(v any) []int {
	switch a := v.(type) {
	case []int:
		return append([]int(nil), a...)
	case []any:
		out := make([]int, 0, len(a))
		for _, x := range a {
			out = append(out, numberOf(x))
		}
		return out
	}
	return nil
}

// v2RemoveUnit takes a unit out, renumbering the groups' unit indices; false if it isn't there.
func v2RemoveUnit(force map[string]any, unitID string) bool {
	i, _ := v2Find(force, unitID)
	if i < 0 {
		return false
	}
	rows, _ := force["units"].([]any)
	force["units"] = append(rows[:i:i], rows[i+1:]...)
	groups, _ := force["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		if gm == nil {
			continue
		}
		var idx []int
		for _, n := range ints(gm["unitIndices"]) {
			switch {
			case n < i:
				idx = append(idx, n)
			case n > i:
				idx = append(idx, n-1)
			}
		}
		gm["unitIndices"] = orInts(idx)
	}
	dropForceHistory(force)
	return true
}

func orInts(a []int) []int {
	if a == nil {
		return []int{}
	}
	return a
}

// dropForceHistory clears what of a force's own record points at units by their place in it (the turn history and C3 networks): an edit that moves units would
// leave it pointing at the wrong ones, and a hangar has no battle to remember.
func dropForceHistory(force map[string]any) {
	cbt, _ := force["cbt"].(map[string]any)
	r := 0
	if cbt != nil {
		r = numberOf(cbt["r"])
	}
	force["cbt"] = map[string]any{"r": r}
}

// v2Pristine makes a unit row as new: no state, not destroyed, an unnamed 4/5 pilot (keeping the pilot's ID).
func v2Pristine(um map[string]any) {
	delete(um, "state")
	delete(um, "destroyed")
	p := v2Pilot(um)
	setPerson(p, "", DefaultGunnery, DefaultPiloting, false)
	um["crew"] = []any{p}
}

// v2Swap reconfigures a unit as another design: a fresh unit of it, keeping its ID.
func v2Swap(force map[string]any, unitID, newName string) error {
	_, um := v2Find(force, unitID)
	if um == nil {
		return errors.New("that unit is no longer in your hangar force")
	}
	design, err := designUUID(newName)
	if err != nil {
		return err
	}
	if v2Design(um) == design {
		return ErrNoChange
	}
	p, _ := packUUID(design)
	v2Pristine(um)
	um["uuid"] = p
	delete(um, "sourceHash")
	delete(um, "customDesign")
	return nil
}

// v2State is a unit row's state object, made if it has none.
func v2State(um map[string]any) map[string]any {
	st, _ := um["state"].(map[string]any)
	if st == nil {
		st = map[string]any{}
		um["state"] = st
	}
	return st
}

// keepRows filters a state's rows (a JSON array of rows) in place, dropping the field when none are left.
func keepRows(state map[string]any, field string, keep func(row []any) bool) {
	raw, _ := state[field].([]any)
	if raw == nil {
		return
	}
	var out []any
	for _, r := range raw {
		row, ok := r.([]any)
		if !ok || keep(row) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		delete(state, field)
	} else {
		state[field] = out
	}
}

// tidyState drops a unit's state when nothing is left in it.
func tidyState(um map[string]any) {
	if st, _ := um["state"].(map[string]any); st != nil {
		if p, _ := st[v2Pending].(map[string]any); p != nil && len(p) == 0 {
			delete(st, v2Pending)
		}
		if len(st) == 0 {
			delete(um, "state")
		}
	}
}

// v2Service is ServiceUnitState for a version 2 unit (see there for what each repair level fixes).
func v2Service(force map[string]any, unitID, kind, level string, tons float64, crippled bool) error {
	_, um := v2Find(force, unitID)
	if um == nil {
		return errors.New("that unit is no longer in your hangar force")
	}
	before, _ := json.Marshal(um)
	state := v2State(um)
	switch kind {
	case "rearm":
		// Spent shots go back to 0; a bin keeps a munition or hot-loading choice, which is the unit's configuration.
		keepRows(state, v2AmmoState, func(row []any) bool {
			if len(row) > 1 {
				row[1] = 0
			}
			return len(row) > 2
		})
	case "repair":
		if err := v2Repair(um, state, level, tons, crippled); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown service %q", kind)
	}
	tidyState(um)
	if after, _ := json.Marshal(um); string(before) == string(after) {
		return ErrNoChange
	}
	return nil
}

func v2Repair(um, state map[string]any, level string, tons float64, crippled bool) error {
	switch level {
	case RepairArmor, RepairStructure, RepairCrippled, RepairDestroyed:
	default:
		return fmt.Errorf("unknown repair level %q", level)
	}
	view, err := v2UnitView(um, "")
	if err != nil {
		return err
	}
	// As in version 1: a unit destroyed without MekBay's flag (its cockpit lost) gets the flag first, so only the destroyed level makes it whole.
	if level != RepairDestroyed && view.IsDestroyed() {
		um["destroyed"] = true
	}
	if level == RepairCrippled || level == RepairDestroyed || (level == RepairStructure && !crippled) {
		v2ClearAllDamage(state)
		if level == RepairDestroyed {
			delete(um, "destroyed")
		}
		return nil
	}
	gone := map[string]bool{}
	for loc := range view.Locations {
		gone[loc] = InternalGone(view, loc, tons)
	}
	pending, _ := state[v2Pending].(map[string]any)
	for _, src := range []map[string]any{state, pending} {
		if src == nil {
			continue
		}
		keepRows(src, v2LocationState, func(row []any) bool {
			kind, loc, _ := v2Target(rowText(row, 0))
			switch kind {
			case 'f', 'r':
				return false // armor: every level restores it
			case 'i':
				return level == RepairArmor || gone[loc]
			}
			return true
		})
	}
	if level == RepairArmor {
		return nil
	}
	// The structure level on a crippled unit: critical slots and destroyed equipment too, but not in destroyed locations, and not the engine's.
	canary, _ := um["sourceHash"].(string)
	layout, _ := layoutOf(v2Design(um), canary)
	engine := func(loc string, index int) bool {
		if layout == nil {
			return false
		}
		s, _ := layout.SlotAt(loc, index)
		return s.System == "Engine"
	}
	for _, src := range []map[string]any{state, pending} {
		if src == nil {
			continue
		}
		keepRows(src, v2SlotState, func(row []any) bool {
			_, loc, index := v2Target(rowText(row, 0))
			return gone[loc] || engine(loc, index)
		})
	}
	componentLoc := func(target string) string {
		if layout != nil {
			if m, ok := layout.MountByID(strings.TrimPrefix(target, "c:")); ok {
				return m.Loc
			}
		}
		return ""
	}
	keepRows(state, v2ComponentState, func(row []any) bool {
		if gone[componentLoc(rowText(row, 0))] {
			return true
		}
		return !clearStatus(row)
	})
	if pending != nil {
		keepRows(pending, v2ComponentState, func(row []any) bool { return gone[componentLoc(rowText(row, 0))] })
	}
	return nil
}

// clearStatus takes a component row's destroyed or disabled status away; true when nothing else is left on the row.
func clearStatus(row []any) bool {
	st, _ := row[1].(map[string]any)
	if st == nil {
		return true
	}
	delete(st, "s")
	return len(st) == 0
}

// v2ClearAllDamage restores every location (conditions too), slot and component, and forgets the rule checks and pending damage; spent ammunition stays.
func v2ClearAllDamage(state map[string]any) {
	delete(state, v2LocationState)
	delete(state, v2LocationConditions)
	delete(state, v2SlotState)
	delete(state, v2RuleChecks)
	delete(state, v2Pending)
	keepRows(state, v2ComponentState, func(row []any) bool { return !clearStatus(row) })
}

// v2ApplyCrew is applyCrew for a version 2 force.
func v2ApplyCrew(force map[string]any, crew []CrewAssignment) error {
	if len(crew) == 0 {
		return nil
	}
	target := map[string]CrewAssignment{}
	named := map[string]bool{}
	for _, a := range crew {
		target[a.UnitID] = a
		if a.Name != "" {
			named[strings.ToLower(a.Name)] = true
		}
	}
	found := map[string]bool{}
	for _, um := range v2Units(force) {
		id := v2UnitID(um)
		p := v2Pilot(um)
		commander, _ := p["commander"].(bool)
		if a, ok := target[id]; ok {
			found[id] = true
			if a.Name == "" {
				setPerson(p, "", DefaultGunnery, DefaultPiloting, commander)
			} else {
				setPerson(p, a.Name, a.Gunnery, a.Piloting, commander)
				setEdge(p, a.Edge)
			}
			continue
		}
		if n, _ := p["name"].(string); n != "" && named[strings.ToLower(n)] {
			setPerson(p, "", DefaultGunnery, DefaultPiloting, commander)
		}
	}
	for id := range target {
		if !found[id] {
			return fmt.Errorf("unit %s is not in the hangar force", id)
		}
	}
	return nil
}

// v2SetCommander marks the pilot of one unit as the force's commander (in version 2 the mark is the person's) and takes it off every other.
func v2SetCommander(force map[string]any, unitID string) {
	for _, um := range v2Units(force) {
		p := v2Pilot(um)
		if unitID != "" && v2UnitID(um) == unitID {
			p["commander"] = true
		} else {
			delete(p, "commander")
		}
	}
}

// v2Clean is CleanForce for a version 2 force: a copy with every unit pristine and no battle record.
func v2Clean(force map[string]any, instance string, now time.Time) {
	for _, um := range v2Units(force) {
		v2Pristine(um)
	}
	force["instanceId"] = instance
	force["timestamp"] = now.UnixMilli()
	delete(force, "owned")
	dropForceHistory(force)
}

// v2Subset is subsetForce for a version 2 hangar: the units a player takes, as they are (damage, ammunition), in a force of their own. The hangar's custom
// designs go with them (each unit points into that table).
func v2Subset(hangar map[string]any, units []string, instance, name string, now time.Time) (map[string]any, error) {
	want := map[string]bool{}
	for _, id := range units {
		want[id] = true
	}
	var taken []any
	for _, um := range v2Units(hangar) {
		if id := v2UnitID(um); want[id] {
			c, err := deepCopy(um)
			if err != nil {
				return nil, err
			}
			taken = append(taken, c)
			delete(want, id)
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for id := range want {
			missing = append(missing, id)
		}
		slices.Sort(missing)
		return nil, fmt.Errorf("units not in the hangar force: %s", strings.Join(missing, ", "))
	}
	f := newV2Force(instance, name, now)
	idx := make([]int, len(taken))
	for i := range taken {
		idx[i] = i
	}
	f["units"] = orEmpty(taken)
	f["groups"] = []any{v2Group("Deployed", idx)}
	if cd, ok := hangar["customDesigns"]; ok {
		f["customDesigns"] = cd
	}
	return f, nil
}
