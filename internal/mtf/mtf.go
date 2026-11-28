// Package mtf reads a mech's layout out of its MegaMek .mtf file: what sits in each critical slot, and the equipment components MekBay makes of those
// slots, numbered as MekBay numbers them.
//
// MekBay's version 2 forces record damage against that layout: a critical slot by its location and index (s:ct:3), a component or ammunition bin by
// MekBay's own number for it (c:m5, m:m12). Its MTF reader (mtf-parser.ts in MekBay's client) gives the numbers, so this follows it rule for rule:
//
//   - A location's rows are packed as MegaMek's MtfFile packs them: the engine, gyro, cockpit, life support and sensors keep their slots, and every other
//     row fills the remaining slots in file order, so an empty row between them closes up.
//   - Locations are read in MTF order (arms, torsos, head, legs). A row starts a new component unless it continues one: the same name in the slot right
//     after, while the component has slots to fill; a targeting computer's; spread equipment's (Endo Steel, Ferro-Fibrous ...); or a split weapon's, in
//     an adjacent location. A row tagged "#N" belongs to the component of its type with that tag.
//   - Components are numbered m1, m2 ... by their first row in the file (its section in file order, then the slot), then the equipment with no slot
//     ("nocrit" lines) in file order.
//
// Systems (engine, gyro, cockpit, life support, sensors, actuators ...) are not components here: MekBay names them by system (c:system:engine).
package mtf

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Layout is a mech's critical slots and components.
type Layout struct {
	Config   string            // Biped, Quad, Tripod, LAM or QuadVee
	TechBase string            // IS or Clan: the tech base equipment names are read for
	Slots    map[string][]Slot // by location code (HD, CT, LT ...), slot by slot
	Mounts   []Mount           // Mounts[i] is component m<i+1>
}

// Slot is one critical slot: a system's, a component's, or empty.
type Slot struct {
	System string // the system it holds: Engine, Gyro, Cockpit, Life Support, Sensors, Shoulder, Hip ... ("" for a component's or an empty slot)
	Mount  int    // the component it holds, numbered from 1 (0 for none)
}

// Mount is one equipment component.
type Mount struct {
	Equipment string // the equipment's internal name (as the row names it when the catalog doesn't know it)
	Name      string // its display name
	Type      string // weapon, ammo, misc, structure or armor ("" when the catalog doesn't know it)
	Loc       string // its location (a split weapon's primary one; "" for equipment placed nowhere)
	Slots     int    // critical slots it fills
	Shots     int    // shots it holds, for an ammunition bin
}

// ID is MekBay's name for the component numbered n.
func ID(n int) string { return "m" + strconv.Itoa(n) }

// MountByID finds a component by MekBay's name for it ("m5"); ok is false for anything else (a system's name, a component the layout doesn't have).
func (l *Layout) MountByID(id string) (Mount, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "m"))
	if !strings.HasPrefix(id, "m") || err != nil || n < 1 || n > len(l.Mounts) {
		return Mount{}, false
	}
	return l.Mounts[n-1], true
}

// SlotAt is the slot at a location's index; ok is false when the layout has no such slot.
func (l *Layout) SlotAt(loc string, index int) (Slot, bool) {
	slots := l.Slots[strings.ToUpper(loc)]
	if index < 0 || index >= len(slots) {
		return Slot{}, false
	}
	return slots[index], true
}

var (
	bipedSections  = map[string]string{"left arm": "LA", "right arm": "RA", "left torso": "LT", "right torso": "RT", "center torso": "CT", "head": "HD", "left leg": "LL", "right leg": "RL", "center leg": "CL"}
	quadSections   = map[string]string{"front left leg": "FLL", "front right leg": "FRL", "left torso": "LT", "right torso": "RT", "center torso": "CT", "head": "HD", "rear left leg": "RLL", "rear right leg": "RRL"}
	bipedOrder     = []string{"LA", "RA", "LT", "RT", "CT", "HD", "LL", "RL"}
	quadOrder      = []string{"FLL", "FRL", "LT", "RT", "CT", "HD", "RLL", "RRL"}
	tripodOrder    = []string{"LA", "RA", "LT", "RT", "CT", "HD", "LL", "RL", "CL"}
	splitAdjacent  = map[string]map[string]bool{"LA": {"LT": true}, "LT": {"LA": true, "CT": true}, "RA": {"RT": true}, "RT": {"RA": true, "CT": true}, "CT": {"LT": true, "RT": true}}
	torsoLocations = map[string]bool{"CT": true, "LT": true, "RT": true}
	// systemNames are the rows that name a system rather than equipment (SYSTEM_NAMES in mtf-parser.ts), by lowercase name.
	systemNames = map[string]string{}
	// placedSystems are the systems MtfFile places itself: other rows pack around them.
	placedSystems = map[string]bool{"Engine": true, "Gyro": true, "Cockpit": true, "Life Support": true, "Sensors": true}
	engineRows    = []string{"Fusion Engine", "XL Engine", "XXL Engine", "Light Engine", "Compact Engine", "No Engine", "Large Fusion Engine", "Large XL Engine",
		"Large XXL Engine", "Large Light Engine", "Large Compact Engine", "XL Fusion Engine", "XXL Fusion Engine", "Light Fusion Engine", "Compact Fusion Engine",
		"Large XL Fusion Engine", "Large XXL Fusion Engine", "Large Light Fusion Engine", "Large Compact Fusion Engine"}
	rowSuffix = regexp.MustCompile(`(?i)\s*\((omnipod|armored|r|t|split|fl|fr|f|rl|rr)\)`)
	mountTag  = regexp.MustCompile(`#([^\s#()]+)$`)
)

func init() {
	for _, s := range []string{"Shoulder", "Upper Arm Actuator", "Lower Arm Actuator", "Hand Actuator", "Hip", "Upper Leg Actuator", "Lower Leg Actuator",
		"Foot Actuator", "Life Support", "Sensors", "Cockpit", "Gyro", "Landing Gear", "Avionics", "Conversion Gear"} {
		systemNames[strings.ToLower(s)] = s
	}
}

// systemOf is the system a row's name stands for, "" for equipment.
func systemOf(name string) string {
	lower := strings.ToLower(name)
	if s, ok := systemNames[lower]; ok {
		return s
	}
	if lower == "engine" {
		return "Engine"
	}
	for _, e := range engineRows {
		if strings.HasPrefix(lower, strings.ToLower(e)) {
			return "Engine"
		}
	}
	return ""
}

// row is one name of a critical row (a superheavy's row may hold two, split by "|").
type row struct {
	name  string
	split bool    // (SPLIT): never continues a component in its own location
	tag   string  // the mount tag ("#2"), "" for none
	size  float64 // a variable-size row's size (":SIZE:2.0"), 1 by default
}

func parseRow(raw string) row {
	text, tag := raw, ""
	if m := mountTag.FindStringSubmatchIndex(raw); m != nil {
		t := raw[m[2]:m[3]]
		text = raw[:m[0]]
		if ok, _ := regexp.MatchString(`^[1-9]\d*$`, t); ok {
			tag = t
		}
	}
	r := row{tag: tag, size: 1}
	for _, m := range rowSuffix.FindAllStringSubmatch(text, -1) {
		if strings.EqualFold(m[1], "split") {
			r.split = true
		}
	}
	name := strings.TrimSpace(rowSuffix.ReplaceAllString(text, ""))
	if i := strings.Index(name, ":SIZE:"); i >= 0 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(name[i+len(":SIZE:"):]), 64); err == nil {
			r.size = v
		}
		name = name[:i]
	}
	r.name = name
	return r
}

func rowMembers(line string) []row {
	parts := strings.Split(line, "|")
	out := make([]row, len(parts))
	for i, p := range parts {
		out[i] = parseRow(p)
	}
	return out
}

// placedSystem says whether a row is one of the systems MtfFile places itself.
func placedSystem(line string) bool {
	for _, m := range rowMembers(line) {
		if placedSystems[systemOf(m.name)] {
			return true
		}
	}
	return false
}

// capacity is a location's number of critical slots.
func capacity(loc string) int {
	switch loc {
	case "HD", "LL", "RL", "CL", "FLL", "FRL", "RLL", "RRL":
		return 6
	}
	return 12
}

// packRows packs a location's file rows as MtfFile does (mtfLocationRows): placed systems keep their rows, everything else fills the other slots in order.
// MekBay places the systems where the design's configuration puts them; a file that MegaMek wrote has them there already, so their file rows are used.
func packRows(loc string, lines []string) []string {
	n := len(lines)
	if c := capacity(loc); c > n {
		n = c
	}
	var open, kept []int
	for i := 0; i < n; i++ {
		if i >= len(lines) || !placedSystem(lines[i]) {
			open = append(open, i)
		}
	}
	for i, l := range lines {
		if l != "-Empty-" && !placedSystem(l) {
			kept = append(kept, i)
		}
	}
	if len(kept) > len(open) {
		return append([]string(nil), lines...)
	}
	rows := make([]string, n)
	for i := range rows {
		rows[i] = "-Empty-"
		if i < len(lines) && placedSystem(lines[i]) {
			rows[i] = lines[i]
		}
	}
	for i, fileRow := range kept {
		rows[open[i]] = lines[fileRow]
	}
	return rows
}

// mount is a component being built: its rows, in reading order.
type mount struct {
	name   string // the row name every row of it carries (after material names are unified)
	eq     *Equipment
	loc    string
	places [][2]int // (section index in reading order, slot)
	last   [2]any   // location and slot of the last row
	tagged bool
	member int
	size   float64
	heavy  bool // on a superheavy mech
}

// need is how many critical slots the component takes (getNumCriticalSlots), unbounded when that depends on more than the row says: rows of it then keep
// continuing it while they follow each other, as in MekBay.
func (m *mount) need() int {
	switch {
	case m.eq == nil:
		return math.MaxInt
	case m.eq.Slots > 0 && m.heavy: // a superheavy's slot holds two
		return (m.eq.Slots + 1) / 2
	case m.eq.Slots > 0:
		return m.eq.Slots
	case m.eq.PerTon:
		return int(math.Ceil(m.size))
	}
	return math.MaxInt
}

// Parse reads a mech's layout from its .mtf file. Equipment names are found in eq; a name it doesn't know still makes a component (it numbers like any
// other), only without a type, and a component of it continues over consecutive rows of the same name.
func Parse(src []byte, eq *Equipments) (*Layout, error) {
	header := map[string]string{}
	sections := map[string][]string{}
	var fileOrder []string // section headers in file order (lowercase, no colon)
	var nocrit []string
	sc := bufio.NewScanner(bytes.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	current := ""
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			current = ""
			continue
		}
		if strings.HasPrefix(trimmed, "#") && current == "" {
			continue
		}
		if strings.HasSuffix(trimmed, ":") {
			name := strings.ToLower(strings.TrimSuffix(trimmed, ":"))
			if _, ok := bipedSections[name]; ok || quadSections[name] != "" {
				current = name
				if _, seen := sections[name]; !seen {
					fileOrder = append(fileOrder, name)
				}
				sections[name] = nil
				continue
			}
		}
		if current != "" {
			sections[current] = append(sections[current], trimmed)
			continue
		}
		if k, v, ok := strings.Cut(trimmed, ":"); ok {
			key := strings.ToLower(strings.TrimSpace(k))
			if key == "nocrit" {
				nocrit = append(nocrit, strings.TrimSpace(v))
			} else if _, seen := header[key]; !seen {
				header[key] = strings.TrimSpace(v)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	config := header["config"]
	lc := strings.ToLower(config)
	l := &Layout{Config: "Biped", TechBase: "IS", Slots: map[string][]Slot{}}
	locMap, order := bipedSections, bipedOrder
	switch {
	case strings.Contains(lc, "lam"):
		l.Config = "LAM"
	case strings.Contains(lc, "quadvee"):
		l.Config, locMap, order = "QuadVee", quadSections, quadOrder
	case strings.Contains(lc, "quad"):
		l.Config, locMap, order = "Quad", quadSections, quadOrder
	case strings.Contains(lc, "tripod"):
		l.Config, order = "Tripod", tripodOrder
	}
	if tb := strings.ToLower(header["techbase"]); strings.Contains(tb, "clan") && !strings.Contains(tb, "is chassis") {
		l.TechBase = "Clan"
	}
	mass, _ := strconv.ParseFloat(header["mass"], 64)
	superheavy := mass > 100
	if len(fileOrder) == 0 {
		return nil, fmt.Errorf("no critical slot sections: not a mech's .mtf file")
	}
	// File order of each location, for numbering.
	fileIndex := map[string]int{}
	for i, h := range fileOrder {
		if loc := locMap[h]; loc != "" {
			fileIndex[loc] = i
		}
	}
	rowsByLoc := map[string][]string{}
	for _, h := range fileOrder {
		if loc := locMap[h]; loc != "" {
			rowsByLoc[loc] = packRows(loc, sections[h])
		}
	}
	find := func(name string) *Equipment { return eq.Find(name, l.TechBase) }
	// Armor and structure rows are one component per material whatever their spelling (MekBay names them after the installed material first).
	material := func(name string, e *Equipment) string {
		if e != nil && (e.Type == "armor" || e.Type == "structure") {
			return "\x00" + e.Type
		}
		return name
	}
	var mounts []*mount
	byKey := map[string]*mount{}     // name@loc@member → the component rows of that name continue
	byTag := map[string]*mount{}     // type#tag → its component
	slotMount := map[[2]any]*mount{} // (location, slot) → component
	for _, loc := range order {
		rows, ok := rowsByLoc[loc]
		if !ok {
			continue
		}
		slots := make([]Slot, len(rows))
		for idx, raw := range rows {
			if raw == "-Empty-" {
				continue
			}
			for member, r := range rowMembers(raw) {
				if s := systemOf(r.name); s != "" {
					slots[idx].System = s
					continue
				}
				e := find(r.name)
				name := material(r.name, e)
				place := [2]any{loc, idx}
				add := func(m *mount, newLoc string) {
					m.places = append(m.places, [2]int{fileIndex[loc], idx})
					m.last = place
					if newLoc != "" {
						m.loc = newLoc
					}
					slotMount[place] = m
				}
				// A tagged row of equipment that splits belongs to the component of its type with that tag.
				tagKey := ""
				if r.tag != "" && e != nil && e.CanSplit && !e.Spreadable {
					tagKey = e.ID + "#" + r.tag
				}
				if m := byTag[tagKey]; tagKey != "" && m != nil {
					primary := loc
					if m.loc != loc {
						primary = splitPrimary(m.loc, loc)
					}
					add(m, primary)
					continue
				}
				key := fmt.Sprintf("%s@%s@%d", name, loc, member)
				if m := byKey[key]; m != nil && !r.split && tagKey == "" {
					if len(m.places) < m.need() && m.last == [2]any{loc, idx - 1} {
						add(m, "")
						continue
					}
				}
				joined := false
				if tagKey == "" {
					for _, m := range mounts { // a targeting computer: one component of each type, everywhere
						if m.name == name && m.eq != nil && m.eq.TC && !m.tagged {
							add(m, "")
							joined = true
							break
						}
					}
				}
				if !joined && tagKey == "" {
					for _, m := range mounts { // spread equipment
						if m.name != name || m.tagged || m.eq == nil || !m.eq.Spreadable {
							continue
						}
						if m.eq.Type == "structure" || m.eq.Type == "armor" || len(m.places) < m.need() {
							add(m, "")
							joined = true
							break
						}
					}
				}
				if !joined && tagKey == "" {
					for _, m := range mounts { // a split weapon continuing into an adjacent location
						if m.name != name || m.tagged || m.eq == nil || !m.eq.CanSplit {
							continue
						}
						if len(m.places) < m.need() && m.loc != loc && splitAdjacent[m.loc][loc] {
							add(m, splitPrimary(m.loc, loc))
							byKey[fmt.Sprintf("%s@%s@%d", m.name, m.loc, member)] = m
							joined = true
							break
						}
					}
				}
				if joined {
					continue
				}
				m := &mount{name: name, eq: e, loc: loc, member: member, tagged: tagKey != "", size: r.size, heavy: superheavy}
				add(m, "")
				mounts = append(mounts, m)
				if tagKey != "" {
					byTag[tagKey] = m
				} else {
					byKey[key] = m
				}
			}
		}
		l.Slots[loc] = slots
	}
	// Number the components by their first row in the file, then (stably) in the order they were made.
	first := func(m *mount) int {
		best := math.MaxInt
		for _, p := range m.places {
			if v := p[0]*100 + p[1]; v < best {
				best = v
			}
		}
		return best
	}
	sorted := append([]*mount(nil), mounts...)
	sort.SliceStable(sorted, func(i, j int) bool { return first(sorted[i]) < first(sorted[j]) })
	number := map[*mount]int{}
	for i, m := range sorted {
		number[m] = i + 1
		l.Mounts = append(l.Mounts, describe(m.name, m.eq, m.loc, len(m.places)))
	}
	for place, m := range slotMount {
		loc, idx := place[0].(string), place[1].(int)
		l.Slots[loc][idx].Mount = number[m]
	}
	// Equipment with no slot ("nocrit:<name>:<location>") comes after.
	for _, n := range nocrit {
		name, loc, _ := strings.Cut(n, ":")
		loc = strings.ToUpper(strings.TrimSpace(loc))
		if loc == "NONE" {
			loc = ""
		}
		l.Mounts = append(l.Mounts, describe(strings.TrimSpace(name), find(strings.TrimSpace(name)), loc, 0))
	}
	return l, nil
}

func describe(name string, e *Equipment, loc string, slots int) Mount {
	m := Mount{Equipment: strings.TrimPrefix(name, "\x00"), Name: strings.TrimPrefix(name, "\x00"), Loc: loc, Slots: slots}
	if e != nil {
		m.Equipment, m.Name, m.Type = e.ID, e.Name, e.Type
		if e.Type == "ammo" {
			m.Shots = e.Shots
		}
	}
	return m
}

// splitPrimary is the location a split weapon counts as being in: a torso over an arm, the center torso over a side one (getMekSplitPrimaryLocation).
func splitPrimary(a, b string) string {
	switch {
	case torsoLocations[b] && !torsoLocations[a]:
		return b
	case torsoLocations[a] && !torsoLocations[b]:
		return a
	case a == "CT":
		return a
	case b == "CT":
		return b
	}
	return a
}
