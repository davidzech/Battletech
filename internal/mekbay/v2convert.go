package mekbay

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/davidzech/Battletech/internal/layouts"
	"github.com/davidzech/Battletech/internal/mtf"
)

// Converting version 1 forces to version 2, and writing a battle's version 2 units back into a version 2 hangar.
//
// A hangar becomes version 2 once it has to be (MekBay saved a battle's forces in version 2, or MekBay's release is the version 2 one, see SetV2): every unit
// keeps its ID, its design (by the catalog's UUID) and its damage, which the design's layout turns into version 2 rows (a version 1 critical slot names its
// location and index, as version 2 does; spent ammunition and destroyed slotless equipment are placed on MekBay's component numbers).

// v2Marker is set on a battle unit read from a version 2 force (ReadBattleUnits), for CommitState.
const v2Marker = "_v2"

// ConvertToV2 converts a version 1 force (the decoded JSON of its saved data) into a version 2 one, in place of nothing: it returns a new record. A force that
// already is version 2 is returned as it is.
func ConvertToV2(force map[string]any) (map[string]any, error) {
	if ForceVersion(force) == 2 {
		return force, nil
	}
	name, _ := force["name"].(string)
	instance, _ := force["instanceId"].(string)
	now := time.Now()
	if ts, ok := force["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			now = t
		}
	}
	out := newV2Force(instance, name, now)
	for _, k := range []string{"note", "tags", "factionId", "factionLock", "eraId", "eraLock", "bv"} {
		if v, ok := force[k]; ok {
			out[k] = v
		}
	}
	var units, groups []any
	gs, _ := force["groups"].([]any)
	for _, g := range gs {
		gm, _ := g.(map[string]any)
		if gm == nil {
			continue
		}
		var idx []int
		us, _ := gm["units"].([]any)
		for _, u := range us {
			um, _ := u.(map[string]any)
			if um == nil {
				continue
			}
			row, err := v1UnitToV2(um)
			if err != nil {
				return nil, err
			}
			idx = append(idx, len(units))
			units = append(units, row)
		}
		gname, _ := gm["name"].(string)
		group := v2Group(gname, orInts(idx))
		if id, _ := gm["id"].(string); id != "" {
			group["id"] = packOpaqueID(id)
		}
		if c, _ := gm["color"].(string); c != "" {
			group["color"] = c
		}
		groups = append(groups, group)
	}
	out["units"], out["groups"] = orEmpty(units), orEmpty(groups)
	return out, nil
}

// v1UnitToV2 converts one version 1 unit (its row in a version 1 force) to a version 2 unit row with the same ID.
func v1UnitToV2(um map[string]any) (map[string]any, error) {
	id, _ := um["id"].(string)
	unit, _ := um["unit"].(string)
	design, err := designUUID(unit)
	if err != nil {
		return nil, err
	}
	state, _ := um["state"].(map[string]any)
	pilot := v2Person("", DefaultGunnery, DefaultPiloting, um["commander"] == true)
	if crew, _ := state["crew"].([]any); len(crew) > 0 {
		if c, _ := crew[0].(map[string]any); c != nil {
			n, _ := c["name"].(string)
			g, p := DefaultGunnery, DefaultPiloting
			if _, ok := c["gunnerySkill"]; ok {
				g = numberOf(c["gunnerySkill"])
			}
			if _, ok := c["pilotingSkill"]; ok {
				p = numberOf(c["pilotingSkill"])
			}
			setPerson(pilot, n, g, p, um["commander"] == true)
			health := map[string]any{}
			if h := numberOf(c["hits"]); h > 0 {
				health["wounds"] = h
			}
			if CrewIsDead(numberOf(c["state"]), numberOf(c["hits"])) {
				health["dead"] = true
			}
			if len(health) > 0 {
				pilot["health"] = health
			}
		}
	}
	row, err := v2UnitRow(id, design, pilot)
	if err != nil {
		return nil, err
	}
	rev, haveLayout := layoutRevision(design)
	if haveLayout {
		row["sourceHash"] = rev.canary
	}
	if state != nil {
		st := v1StateToV2(state, rev.layout)
		if len(st) > 0 {
			row["state"] = st
		}
		if d, _ := state["destroyed"].(bool); d {
			row["destroyed"] = true
		}
	}
	return row, nil
}

type layoutRev struct {
	layout *mtf.Layout
	canary string
}

// layoutRevision is a design's newest layout and its canary.
func layoutRevision(design string) (layoutRev, bool) {
	customMu.RLock()
	l, ok := custom[design]
	customMu.RUnlock()
	if ok {
		return layoutRev{layout: l}, true
	}
	if r, ok := layoutsNewest(design); ok {
		return layoutRev{layout: r.Layout, canary: r.Canary}, true
	}
	return layoutRev{}, false
}

// v1StateToV2 converts a version 1 unit state's damage into version 2 rows, as MekBay's own conversion does (legacy-force-v1-converter.ts): pending damage is
// committed, critical slots keep their location and index, spent ammunition and destroyed slotless equipment go on the component the layout puts there.
// What the layout can't place (no layout, or a slot holding no component) is left out: a missing ammunition row is a full bin.
func v1StateToV2(state map[string]any, layout *mtf.Layout) map[string]any {
	st := map[string]any{}
	var locRows, condRows, slotRows, compRows, ammoRows []any
	locs, _ := state["locations"].(map[string]any)
	keys := make([]string, 0, len(locs))
	for k := range locs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m, _ := locs[k].(map[string]any)
		if m == nil {
			continue
		}
		code, rear := strings.CutSuffix(k, "-rear")
		code = strings.ToLower(code)
		armorFace := "f:"
		if rear {
			armorFace = "r:"
		}
		if a := numberOf(m["armor"]) + numberOf(m["pendingArmor"]); a > 0 {
			locRows = append(locRows, []any{armorFace + code, a})
		}
		if i := numberOf(m["internal"]) + numberOf(m["pendingInternal"]); i > 0 && !rear {
			locRows = append(locRows, []any{"i:" + code, i})
		}
		cs, _ := m["conditions"].([]any)
		for _, c := range cs {
			raw, _ := jsonRaw(c)
			if key := conditionKey(raw); destroysLocation(key) {
				condRows = append(condRows, []any{"i:" + code, key, 1})
			}
		}
	}
	mountAt := func(loc string, slot int) int {
		if layout == nil {
			return 0
		}
		s, _ := layout.SlotAt(loc, slot)
		return s.Mount
	}
	ammo := map[int]int{}
	taken := map[string]bool{}
	crits, _ := state["crits"].([]any)
	for _, c := range crits {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		loc, _ := cm["loc"].(string)
		slot := numberOf(cm["slot"])
		if _, hasSlot := cm["slot"]; !hasSlot {
			// A row without its index (older saves): the slot of the location that holds what it names, each slot taken once.
			name, _ := cm["name"].(string)
			slot = slotByName(layout, loc, name, taken)
		}
		if loc == "" || slot < 0 {
			continue
		}
		taken[strings.ToUpper(loc)+":"+strconv.Itoa(slot)] = true
		hits := numberOf(cm["hits"]) + numberOf(cm["pendingHits"])
		gone := isSet(cm["destroyed"]) || isSet(cm["destroying"])
		if gone && hits < 1 {
			hits = 1
		}
		if hits > 0 {
			row := []any{"s:" + strings.ToLower(loc) + ":" + strconv.Itoa(slot), hits}
			if gone {
				row = append(row, 1)
			}
			slotRows = append(slotRows, row)
		}
		if used := numberOf(cm["consumed"]); used > 0 {
			if n := mountAt(loc, slot); n > 0 {
				ammo[n] += used
			}
		}
	}
	inv, _ := state["inventory"].([]any)
	for _, it := range inv {
		im, _ := it.(map[string]any)
		if im == nil {
			continue
		}
		id, _ := im["id"].(string)
		n := inventoryMount(layout, id)
		if n == 0 {
			continue
		}
		destroyed := im["destroyed"] == true || im["destroying"] == true
		if destroyed {
			compRows = append(compRows, []any{"c:" + mtf.ID(n), map[string]any{"s": "destroyed"}})
		}
		if used := numberOf(im["consumed"]); used > 0 {
			ammo[n] += used
		}
	}
	nums := make([]int, 0, len(ammo))
	for n := range ammo {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		used := ammo[n]
		if m := layout.Mounts[n-1]; m.Shots > 0 && used > m.Shots {
			used = m.Shots
		}
		ammoRows = append(ammoRows, []any{"m:" + mtf.ID(n), used})
	}
	for field, rows := range map[string][]any{v2LocationState: locRows, v2LocationConditions: condRows, v2SlotState: slotRows, v2ComponentState: compRows, v2AmmoState: ammoRows} {
		if len(rows) > 0 {
			st[field] = rows
		}
	}
	checks, _ := state["ruleChecks"].(map[string]any)
	if raw, ok := checks[torsoCheckKey].(map[string]any); ok {
		// A check with no torso named counts for none (TorsoCheckTrigger), and MekBay can't load a row without one: it is left out.
		if status, _ := raw["status"].(string); status != "" && raw["trigger"] != nil && raw["trigger"] != "" {
			trig, _ := raw["trigger"].(string)
			token, _ := raw["token"].(string)
			if token == "" {
				token = "converted"
			}
			st[v2RuleChecks] = []any{[]any{torsoCheckKey, token, "i:" + strings.ToLower(trig), 0, status}}
		}
	}
	return st
}

// slotByName finds the slot of a location that holds what a version 1 row names (a system, or a component of that equipment), skipping slots already taken;
// -1 when the layout has none.
func slotByName(layout *mtf.Layout, loc, name string, taken map[string]bool) int {
	if layout == nil || name == "" {
		return -1
	}
	loc = strings.ToUpper(loc)
	for i, sl := range layout.Slots[loc] {
		if taken[loc+":"+strconv.Itoa(i)] {
			continue
		}
		if strings.EqualFold(sl.System, name) {
			return i
		}
		if sl.Mount > 0 {
			if m := layout.Mounts[sl.Mount-1]; strings.EqualFold(m.Equipment, name) || strings.EqualFold(m.Name, name) {
				return i
			}
		}
	}
	return -1
}

// inventoryMount places a version 1 inventory item ("<name>@<location>#<index>") on the layout's component: the index-th component of that equipment in that
// location (0 when there is none).
func inventoryMount(layout *mtf.Layout, id string) int {
	if layout == nil {
		return 0
	}
	at := strings.LastIndex(id, "@")
	if at < 0 {
		return 0
	}
	name, rest := id[:at], id[at+1:]
	loc, idx, _ := strings.Cut(rest, "#")
	want, _ := strconv.Atoi(idx)
	seen := 0
	for i, m := range layout.Mounts {
		if (m.Equipment == name || m.Name == name) && strings.EqualFold(m.Loc, strings.TrimSpace(loc)) {
			if seen == want {
				return i + 1
			}
			seen++
		}
	}
	return 0
}

// v2CommitState is CommitState for a version 2 hangar: each battle unit's state (version 2 rows, or a version 1 unit converted) goes onto the hangar unit
// with the same ID and design, settled as MekBay settles a phase's end, its battle leftovers dropped and its crew reset.
func v2CommitState(hangar map[string]any, battle map[string]map[string]any) CommitBattleReport {
	var rep CommitBattleReport
	used := map[string]bool{}
	for _, um := range v2Units(hangar) {
		id := v2UnitID(um)
		bu, ok := battle[id]
		if !ok {
			continue
		}
		row := bu
		if bu[v2Marker] != true {
			conv, err := v1UnitToV2(bu)
			if err != nil {
				continue
			}
			row = conv
		}
		if v2Design(row) != v2Design(um) {
			continue
		}
		next, err := deepCopy(row)
		if err != nil {
			continue
		}
		delete(um, "state")
		delete(um, "destroyed")
		if st, _ := next["state"].(map[string]any); len(st) > 0 {
			um["state"] = st
		}
		if d, _ := next["destroyed"].(bool); d {
			um["destroyed"] = true
		}
		if s, _ := next["sourceHash"].(string); s != "" {
			um["sourceHash"] = s
		}
		v2Settle(um)
		p := v2Pilot(um)
		setPerson(p, "", DefaultGunnery, DefaultPiloting, false)
		um["crew"] = []any{p}
		used[id] = true
		rep.Matched++
	}
	for id, bu := range battle {
		if !used[id] {
			name, _ := bu["unit"].(string)
			if name == "" {
				name = v2Design(bu)
			}
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%v (%s)", name, id))
		}
	}
	return rep
}

// v2Settle is the end of a phase for a unit back in the hangar (commitPending in MekBay's unit-instance.ts), then its battle leftovers dropped: pending
// armor and structure loss and slot hits are added to what it had, pending component statuses and location conditions are committed, NARC pods are removed,
// and heat, the turn, movement, the unit's conditions and its place on the table are forgotten. A unit left with three engine hits or no cockpit is marked
// destroyed, as MekBay's evaluateDestroyed does.
func v2Settle(um map[string]any) {
	state, _ := um["state"].(map[string]any)
	if state == nil {
		return
	}
	if pending, _ := state[v2Pending].(map[string]any); pending != nil {
		addRows(state, v2LocationState, v2Rows(pending, v2LocationState))
		addRows(state, v2SlotState, v2Rows(pending, v2SlotState))
		for _, row := range v2Rows(pending, v2LocationConditions) {
			setRow(state, v2LocationConditions, row, 2)
		}
		for _, row := range v2Rows(pending, v2ComponentState) {
			status := rowText(row, 1)
			found := false
			keepRows(state, v2ComponentState, func(r []any) bool {
				if rowText(r, 0) == rowText(row, 0) {
					found = true
					st, _ := r[1].(map[string]any)
					if st == nil {
						st = map[string]any{}
						r[1] = st
					}
					st["s"] = status
				}
				return true
			})
			if !found {
				state[v2ComponentState] = append(asRows(state[v2ComponentState]), []any{rowText(row, 0), map[string]any{"s": status}})
			}
		}
		delete(state, v2Pending)
	}
	// A slot hit is a destroyed slot (MekBay records the turn it happened; the hangar has none, so turn 1).
	if rows := asRows(state[v2SlotState]); rows != nil {
		for i, r := range rows {
			row, _ := r.([]any)
			if row != nil && rowInt(row, 1) > 0 && (len(row) < 3 || row[2] == nil) {
				rows[i] = []any{row[0], row[1], 1}
			}
		}
	}
	keepRows(state, v2LocationConditions, func(row []any) bool {
		c := rowText(row, 1)
		return c != "narc" && c != "new-narc"
	})
	for _, k := range []string{"z", "t", "m", "o", "c3", "np", "sw", "tow", "er", "vr", "v", "vcs", "cm"} {
		delete(state, k)
	}
	// What a component holds only while a battle goes on: a jam, a capacitor's charge, a failure in progress.
	keepRows(state, v2ComponentState, func(row []any) bool {
		st, _ := row[1].(map[string]any)
		for _, k := range []string{"j", "e", "p", "b", "c", "g"} {
			delete(st, k)
		}
		return len(st) > 0
	})
	if view, err := v2UnitView(um, ""); err == nil && (view.EngineHits >= engineDestructionHits || view.CockpitGone) {
		um["destroyed"] = true
	}
	tidyState(um)
}

func asRows(v any) []any {
	a, _ := v.([]any)
	return a
}

// addRows adds damage rows ([target, amount, ...]) into a state's rows of the same field, summing the amounts of rows with the same target.
func addRows(state map[string]any, field string, add [][]any) {
	rows := asRows(state[field])
	for _, a := range add {
		found := false
		for i, r := range rows {
			row, _ := r.([]any)
			if row != nil && rowText(row, 0) == rowText(a, 0) {
				row[1] = rowInt(row, 1) + rowInt(a, 1)
				rows[i] = row
				found = true
				break
			}
		}
		if !found {
			rows = append(rows, append([]any(nil), a...))
		}
	}
	if len(rows) > 0 {
		state[field] = rows
	}
}

// setRow puts a row in, replacing one whose first n cells are the same.
func setRow(state map[string]any, field string, row []any, n int) {
	rows := asRows(state[field])
	for i, r := range rows {
		old, _ := r.([]any)
		same := old != nil && len(old) >= n
		for j := 0; same && j < n && j < len(row); j++ {
			same = fmt.Sprint(old[j]) == fmt.Sprint(row[j])
		}
		if same {
			rows[i] = row
			state[field] = rows
			return
		}
	}
	state[field] = append(rows, row)
}

func jsonRaw(v any) (json.RawMessage, error) { return json.Marshal(v) }

func layoutsNewest(design string) (layouts.Revision, bool) { return layouts.Current().Newest(design) }
