package mekbay

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/davidzech/Battletech/internal/catalog"
)

// MekBay's version 2 force format, as its next release saves forces (force-storage-codec.ts, force-personnel-storage.ts and
// force-storage-vocabulary.ts in MekBay's client; its server stores the record as it is sent).
//
// A version 2 force names each unit's design by MegaMek's UUID for it (packed, see packUUID) and keeps only what differs from a pristine unit, as rows that
// point at a part of the unit by a target reference:
//
//	f:<loc> i:<loc> r:<loc>   front armor, internal structure, rear armor of a location (lowercase code: ct, lt, hd ...)
//	s:<loc>:<index>           a critical slot, by its location and 0-based index
//	c:<component>             a component: an equipment mount (m0, m1 ...: MekBay numbers them as it reads the design) or a system
//	m:<component>             an ammunition bin, by its mount
//
// The unit's crew sits on the unit row ("crew": one person per crew station, null for an empty one); everything else about its state is under "state", by
// short field names (CBT_UNIT_FIELD). Reading a version 2 force keeps every field, read or not, so writing it back loses nothing.

// Version 2 unit state fields (CBT_UNIT_FIELD) and pending combat fields (CBT_PENDING_COMBAT_FIELD) the site reads.
const (
	v2LocationState      = "l" // [target, damage]
	v2LocationConditions = "n" // [target, condition, value]
	v2SlotState          = "s" // [target, hits, destroyedTurn?]
	v2ComponentState     = "c" // [target, {s: status ...}]
	v2AmmoState          = "a" // [target, shotsSpent, munitionOverride?, hotLoaded?]
	v2RuleChecks         = "rC"
	v2Pending            = "p" // pending combat: l, n, s, c rows as above, c as [target, status]
)

// ForceVersion is a saved force's format version: 2 for MekBay's version 2, 1 otherwise (version 1 forces may leave it out).
func ForceVersion(force map[string]any) int {
	if v, _ := force["version"].(float64); v == 2 {
		return 2
	}
	if v, _ := force["version"].(int); v == 2 {
		return 2
	}
	return 1
}

// packUUID writes a UUID the way MekBay's version 2 format does (packUuid in compact-uuid.ts): its 16 bytes in unpadded base64url, 22 characters.
func packUUID(u string) (string, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(u, "-", ""))
	if err != nil || len(b) != 16 {
		return "", fmt.Errorf("%q is not a UUID", u)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// unpackUUID reads a packed UUID back into its usual lowercase form.
func unpackUUID(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != 16 || len(s) != 22 {
		return "", fmt.Errorf("%q is not a packed UUID", s)
	}
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// isUUID says whether s is a UUID in its usual form (any version, any case).
func isUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil
}

// packOpaqueID writes an ID as MekBay's version 2 format does (packOpaqueId): a UUID packed behind "~", anything else as it is ("~" doubled in front).
func packOpaqueID(id string) string {
	if isUUID(id) {
		p, _ := packUUID(id)
		return "~" + p
	}
	if strings.HasPrefix(id, "~") {
		return "~" + id
	}
	return id
}

// unpackOpaqueID reads an ID packOpaqueID wrote.
func unpackOpaqueID(s string) string {
	switch {
	case !strings.HasPrefix(s, "~"):
		return s
	case strings.HasPrefix(s, "~~"):
		return s[1:]
	}
	if u, err := unpackUUID(s[1:]); err == nil {
		return u
	}
	return s
}

// packInstanceID writes a unit's ID inside its force (packUnitInstanceId): MekBay's own "unit:<uuid>" IDs pack behind "~u", any other ID as an opaque one.
// A version 1 force's unit IDs (plain UUIDs) therefore keep their value through MekBay's conversion to version 2.
func packInstanceID(id string) string {
	if u, ok := strings.CutPrefix(id, "unit:"); ok && isUUID(u) {
		p, _ := packUUID(u)
		return "~u" + p
	}
	return packOpaqueID(id)
}

// unpackInstanceID reads a unit ID packInstanceID wrote.
func unpackInstanceID(s string) string {
	if p, ok := strings.CutPrefix(s, "~u"); ok && len(p) == 22 {
		if u, err := unpackUUID(p); err == nil {
			return "unit:" + u
		}
	}
	return unpackOpaqueID(s)
}

// v2Target reads a saved target reference (parseSavedTargetRef) as its kind letter and the location it is in, uppercase ("" when it names no location:
// components and ammunition bins), with a slot's index (-1 for anything else).
func v2Target(ref string) (kind byte, loc string, slot int) {
	if len(ref) < 3 || ref[1] != ':' {
		return 0, "", -1
	}
	kind, id, slot := ref[0], ref[2:], -1
	switch kind {
	case 'f', 'i', 'r':
		return kind, strings.ToUpper(id), -1
	case 's':
		l, n, ok := strings.Cut(id, ":")
		if i, err := strconv.Atoi(n); ok && err == nil {
			slot = i
		}
		return kind, strings.ToUpper(l), slot
	}
	return kind, "", -1
}

// v2Rows reads a list of rows (each a JSON array) out of a state object; anything malformed is skipped.
func v2Rows(state map[string]any, field string) [][]any {
	raw, _ := state[field].([]any)
	var out [][]any
	for _, r := range raw {
		if row, ok := r.([]any); ok && len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

func rowText(row []any, i int) string {
	if i < len(row) {
		s, _ := row[i].(string)
		return s
	}
	return ""
}

func rowInt(row []any, i int) int {
	if i < len(row) {
		switch v := row[i].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	return 0
}

func intField(m map[string]any, key string, def int) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

// parseV2Force reads a version 2 force's saved data into a HangarForce. A unit's design is its catalog unit when the catalog knows its UUID; a design it doesn't
// know (a custom one) keeps an empty Unit and its UUID.
func parseV2Force(data map[string]any) (HangarForce, error) {
	h := HangarForce{}
	h.Name, _ = data["name"].(string)
	switch ts := data["timestamp"].(type) {
	case float64:
		h.Timestamp = strconv.FormatInt(int64(ts), 10)
	case string:
		h.Timestamp = ts
	}
	units, _ := data["units"].([]any)
	groupOf := make([]string, len(units))
	groups, _ := data["groups"].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		name, _ := gm["name"].(string)
		idx, _ := gm["unitIndices"].([]any)
		for _, i := range idx {
			if n, ok := i.(float64); ok && int(n) >= 0 && int(n) < len(units) {
				groupOf[int(n)] = name
			}
		}
	}
	for i, u := range units {
		um, ok := u.(map[string]any)
		if !ok {
			return HangarForce{}, fmt.Errorf("unit %d of the force is not an object", i)
		}
		hu, err := v2UnitView(um, groupOf[i])
		if err != nil {
			return HangarForce{}, fmt.Errorf("unit %d of the force: %w", i, err)
		}
		h.Units = append(h.Units, hu)
	}
	return h, nil
}

// v2UnitView reads one version 2 unit row as the site sees it, like unitState.view does a version 1 unit. Damage still pending counts, as on MekBay's record
// sheet.
//
// What a damaged critical slot or component holds (an engine slot, the cockpit) is not in the force: it takes the design's layout, which the site doesn't
// know yet, so for now a version 2 unit counts no engine hits and loses its cockpit only with its head's internal structure.
func v2UnitView(um map[string]any, group string) (HangarUnit, error) {
	id, _ := um["id"].(string)
	packed, _ := um["uuid"].(string)
	design, err := unpackUUID(packed)
	if err != nil {
		return HangarUnit{}, err
	}
	hu := HangarUnit{ID: unpackInstanceID(id), UUID: design, Group: group, Gunnery: DefaultGunnery, Piloting: DefaultPiloting}
	if cu, ok := catalog.ByUUID(design); ok {
		hu.Unit = cu.Name
	}
	hu.Destroyed, _ = um["destroyed"].(bool)
	if crew, _ := um["crew"].([]any); len(crew) > 0 {
		if c, ok := crew[0].(map[string]any); ok {
			hu.Pilot, _ = c["name"].(string)
			hu.Gunnery, hu.Piloting = intField(c, "g", DefaultGunnery), intField(c, "p", DefaultPiloting)
			if health, ok := c["health"].(map[string]any); ok {
				dead, _ := health["dead"].(bool)
				hu.CrewDead = dead || intField(health, "wounds", 0) >= 6
			}
		}
	}
	state, _ := um["state"].(map[string]any)
	pending, _ := state[v2Pending].(map[string]any)
	loss := func(loc string) LocationLoss {
		if hu.Locations == nil {
			hu.Locations = map[string]LocationLoss{}
		}
		return hu.Locations[loc]
	}
	for _, src := range []map[string]any{state, pending} {
		for _, row := range v2Rows(src, v2LocationState) {
			kind, loc, _ := v2Target(rowText(row, 0))
			if loc == "" || rowInt(row, 1) <= 0 {
				continue
			}
			l := loss(loc)
			if kind == 'i' {
				l.Internal += rowInt(row, 1)
			} else {
				l.Armor += rowInt(row, 1)
			}
			hu.Locations[loc] = l
			hu.Damaged = true
		}
		for _, row := range v2Rows(src, v2LocationConditions) {
			if _, loc, _ := v2Target(rowText(row, 0)); loc != "" && destroysLocation(rowText(row, 1)) {
				l := loss(loc)
				l.Gone = true
				hu.Locations[loc] = l
				hu.Damaged = true
			}
		}
		for _, row := range v2Rows(src, v2SlotState) {
			_, loc, _ := v2Target(rowText(row, 0))
			destroyed := len(row) > 2 && row[2] != nil
			if rowInt(row, 1) > 0 || destroyed {
				hu.Damaged = true
				hu.Crits = append(hu.Crits, CritDamage{Loc: loc})
			}
		}
	}
	for _, row := range v2Rows(state, v2ComponentState) {
		if st, _ := row[1].(map[string]any); st != nil && st["s"] == "destroyed" {
			hu.Damaged = true
			hu.Crits = append(hu.Crits, CritDamage{})
		}
	}
	for _, row := range v2Rows(pending, v2ComponentState) {
		if rowText(row, 1) == "destroyed" {
			hu.Damaged = true
			hu.Crits = append(hu.Crits, CritDamage{})
		}
	}
	for _, row := range v2Rows(state, v2AmmoState) {
		if used := rowInt(row, 1); used > 0 {
			hu.AmmoUsed += used
			hu.AmmoBins++
			hu.AmmoUsed2 = append(hu.AmmoUsed2, AmmoUse{Name: strings.TrimPrefix(rowText(row, 0), "m:"), Used: used})
		}
	}
	tons, cockpitLoc := 0.0, "HD"
	if cu, ok := catalog.ByUUID(design); ok {
		tons = cu.Tons
		if cu.CockpitLoc != "" {
			cockpitLoc = cu.CockpitLoc
		}
	}
	if InternalGone(hu, cockpitLoc, tons) {
		hu.CockpitGone = true
	}
	for _, row := range v2Rows(state, v2RuleChecks) {
		if rowText(row, 0) == torsoCheckKey {
			_, loc, _ := v2Target(rowText(row, 2))
			hu.TorsoCheckFailed, hu.TorsoCheckTrigger = rowText(row, 4) == "failed", loc
		}
	}
	return hu, nil
}
