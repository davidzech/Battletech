package mekbay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/coder/websocket"

	"github.com/davidzech/Battletech/internal/catalog"
)

// MekBay's server is closed source and has no documented API: this speaks the WebSocket protocol its own web
// app uses (client source: github.com/MegaMek/mekbay, ws.service.ts and data.service.ts). It is unofficial and
// may break whenever MekBay changes it.
const (
	mekbayWS = "wss://mekbay.com/ws"
	Protocol = 2 // PROTOCOL_VERSION in ws.service.ts
	// PersistenceRevision is FORCE_PERSISTENCE_REVISION in ws.service.ts. MekBay's server refuses to read or delete a force saved in its version 2
	// format for a request that doesn't carry it; with it, a version 1 force still reads as version 1.
	PersistenceRevision = 2
)

// ForceDraft is a force to create in MekBay: groups of units, with their pilots.
type ForceDraft struct {
	Name   string
	Groups []GroupDraft
}

// GroupDraft is one named group of units inside a ForceDraft.
type GroupDraft struct {
	Name  string
	Units []UnitDraft
}

// UnitDraft is one unit: Unit is MekBay's unit ID (see Unit.Name).
type UnitDraft struct {
	Unit      string
	BV        int
	Pilot     string
	Gunnery   int
	Piloting  int
	Commander bool // the force's commander (MekBay's formation commander, shown on the record sheet)
}

// CritDamage is one hit or destroyed critical slot: where it is and whether it belongs to the engine.
type CritDamage struct {
	Loc    string
	Engine bool
}

// AmmoUse is one ammo bin with some of its shots used.
type AmmoUse struct {
	Name string // MekBay's name for the ammo
	Loc  string
	Used int // shots used
}

// HangarUnit is one unit of a force, read from MekBay's save format.
type HangarUnit struct {
	ID       string // the unit's own ID inside the force
	Unit     string // MekBay unit ID, e.g. BMMadCat_H ("" for a design the catalog doesn't know)
	UUID     string // MegaMek's UUID for the design, which version 2 forces name units by ("" in a version 1 force)
	Group    string // name of the group it currently sits in
	Pilot    string
	Gunnery  int
	Piloting int
	// CrewDead is the crew's own record: its stored state is dead, or it has taken the hits that kill (see CrewIsDead). A crew reset to an unnamed 4/5
	// when a battle is written back into the hangar is alive.
	CrewDead bool
	// CockpitGone is the unit's damage: its cockpit slot is destroyed, or its head has no internal structure left. MekBay's record sheet shows whoever sits
	// in it dead then, whatever the crew's stored state says. It stays with the unit (in the hangar too) until it is repaired, so on its own it says
	// nothing about a pilot: see PilotKilled.
	CockpitGone bool
	Damaged     bool // has recorded armor/structure damage, a blown-off or flooded location, critical hits or destroyed equipment (spent ammo is not damage)
	Destroyed   bool // MekBay's own destroyed flag; whether the unit counts as destroyed is IsDestroyed
	// The raw damage MekBay records, from which conditionOf works out the unit's state (see condition.go).
	Locations  map[string]LocationLoss // armor and internal structure points lost, by location (HD, CT, LT, RT, LA, RA, LL, RL ...)
	AmmoUsed   int                     // ammo consumed, summed over its ammo bins (and inventory items)
	AmmoBins   int                     // ammo bins (or inventory items) with some consumed (each counts as a ton to rearm)
	AmmoUsed2  []AmmoUse               // the ammo bins with some used, for the rearm breakdown
	Crits      []CritDamage            // critical slots hit or destroyed, and destroyed inventory items (spent ammo is not damage), to know what a repair fixes
	EngineHits int                     // destroyed engine slots
	// TorsoCheckFailed is MekBay's stored result of the torso crippling check (rule check core.torso-crippling), and TorsoCheckTrigger the torso it was
	// made for: it only counts while that torso is the one destroyed (mek-rules.ts).
	TorsoCheckFailed  bool
	TorsoCheckTrigger string
}

// HangarForce is a force as the site reads it: a campaign's hangar, or a battle's force read from MekBay.
type HangarForce struct {
	Name      string
	Timestamp string
	Units     []HangarUnit
}

// MekBay is what the site asks of mekbay.com: battles, and reading a force by its ID (a hangar link when a campaign starts, and the one-time import of the
// hangars MekBay used to hold, see internal/site/hangarimport.go). Campaign hangars themselves live in the site's database. It is an interface so tests can run
// without the network.
type MekBay interface {
	// ReadForce reads a saved force anonymously (anyone can read one by its instance ID) and returns its saved data as JSON, the format the site stores a
	// hangar in.
	ReadForce(ctx context.Context, instance string) (json.RawMessage, error)
	// StartBattle builds the players' forces and the OpFor's and one operation that pairs them (see battle.go).
	StartBattle(ctx context.Context, req BattleRequest) (BattleResult, error)
	// ReadBattle reads an operation and every force in it, anonymously, for the battle's live status.
	ReadBattle(ctx context.Context, operationID string) (BattleStatus, error)
	// ReadBattleUnits reads the units of a finished battle's players' forces, by their IDs, to write them back into the hangar (see commit.go).
	ReadBattleUnits(ctx context.Context, account, operationID string, skip []string) (map[string]map[string]any, error)
	// DeleteBattle deletes a battle's operation and all its forces (best effort).
	DeleteBattle(ctx context.Context, owner, operationID string) error
}

// Client is the real implementation, speaking to MekBay's WebSocket server.
type Client struct {
	URL string // defaults to wss://mekbay.com/ws
}

func (c *Client) url() string {
	if c.URL == "" {
		return mekbayWS
	}
	return c.URL
}

// ReadForce reads a force anonymously, with a throwaway identity, and returns its saved data.
func (c *Client) ReadForce(ctx context.Context, instance string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	s, err := Dial(ctx, c.url(), NewID())
	if err != nil {
		return nil, err
	}
	defer s.Close()
	data, err := s.getForce(ctx, instance, false)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, ErrNoForce
	}
	return json.Marshal(data)
}

// ErrNoForce is ReadForce's answer for a force MekBay doesn't have (never saved, or deleted), as against a failure to ask.
var ErrNoForce = errors.New("MekBay has no force for that hangar link; check that the link is saved in MekBay")

// ParseForce reads a force's saved data (the JSON the site stores a hangar as) into a HangarForce.
func ParseForce(data []byte) (HangarForce, error) {
	if len(data) == 0 {
		return HangarForce{}, errors.New("there is no hangar force")
	}
	raw := make([]byte, 0, len(data)+9)
	raw = append(append(append(raw, `{"data":`...), data...), '}')
	return ParseHangarForce(raw)
}

// FormatTime formats a time the way MekBay writes force timestamps.
func FormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// ParseHangarForce reads a getForce reply into a HangarForce.
//
// Damage is read as MekBay's record sheet shows it, pending damage included (MekBay's getArmorHits and getInternalHits add a location's pendingArmor and
// pendingInternal, and its crippling rule counts a slot that is being destroyed): a battle's live force may hold damage whose phase hasn't ended. A hangar holds
// none pending, since CommitState consolidates it when a battle is written back.
func ParseHangarForce(raw json.RawMessage) (HangarForce, error) {
	var head struct {
		Data *struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &head) == nil && head.Data != nil && head.Data.Version == 2 {
		var v2 struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &v2); err != nil {
			return HangarForce{}, fmt.Errorf("could not read MekBay's reply: %w", err)
		}
		return parseV2Force(v2.Data)
	}
	var reply struct {
		Data *struct {
			Name      string `json:"name"`
			Timestamp string `json:"timestamp"`
			Groups    []struct {
				Name  string `json:"name"`
				Units []struct {
					ID    string    `json:"id"`
					Unit  string    `json:"unit"`
					State unitState `json:"state"`
				} `json:"units"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return HangarForce{}, fmt.Errorf("could not read MekBay's reply: %w", err)
	}
	if reply.Data == nil {
		return HangarForce{}, errors.New("MekBay has no force for your hangar link; check that the link is saved in MekBay")
	}
	h := HangarForce{Name: reply.Data.Name, Timestamp: reply.Data.Timestamp}
	for _, g := range reply.Data.Groups {
		for _, u := range g.Units {
			h.Units = append(h.Units, u.State.view(u.ID, u.Unit, g.Name))
		}
	}
	return h, nil
}

// unitState is the part of a unit's saved state (MekBay's CBTSerializedState) the site reads.
type unitState struct {
	Destroyed bool `json:"destroyed"`
	Locations map[string]struct {
		Armor           int               `json:"armor"`
		Internal        int               `json:"internal"`
		PendingArmor    int               `json:"pendingArmor"`
		PendingInternal int               `json:"pendingInternal"`
		Conditions      []json.RawMessage `json:"conditions"`
	} `json:"locations"`
	Crits []struct {
		Name        string `json:"name"`
		Loc         string `json:"loc"`
		Hits        int    `json:"hits"`
		PendingHits int    `json:"pendingHits"`
		Consumed    int    `json:"consumed"`
		Destroyed   any    `json:"destroyed"`
		Destroying  any    `json:"destroying"`
	} `json:"crits"`
	Inventory []struct {
		ID         string `json:"id"`
		Destroyed  *bool  `json:"destroyed"`
		Destroying *bool  `json:"destroying"`
		Consumed   int    `json:"consumed"`
	} `json:"inventory"`
	Crew []struct {
		Name     string `json:"name"`
		Gunnery  int    `json:"gunnerySkill"`
		Piloting int    `json:"pilotingSkill"`
		Hits     int    `json:"hits"`
		State    int    `json:"state"`
	} `json:"crew"`
	RuleChecks map[string]json.RawMessage `json:"ruleChecks"`
}

// isSet says whether a crit's saved destroyed or destroying (a timestamp) is set: MekBay leaves it out (undefined) when it isn't. 0 counts as set, as in
// MekBay (a slot destroyed with its location may carry 0).
func isSet(v any) bool { return v != nil && v != false }

// view reads one unit's state as the site sees it.
func (st unitState) view(id, unit, group string) HangarUnit {
	hu := HangarUnit{ID: id, Unit: unit, Group: group, Destroyed: st.Destroyed, Gunnery: DefaultGunnery, Piloting: DefaultPiloting}
	if len(st.Crew) > 0 {
		c := st.Crew[0]
		hu.Pilot, hu.Gunnery, hu.Piloting = c.Name, c.Gunnery, c.Piloting
		hu.CrewDead = CrewIsDead(c.State, c.Hits)
	}
	for k, l := range st.Locations {
		loss := LocationLoss{Armor: l.Armor + l.PendingArmor, Internal: l.Internal + l.PendingInternal}
		for _, c := range l.Conditions {
			if destroysLocation(conditionKey(c)) {
				loss.Gone = true
			}
		}
		if loss.Armor > 0 || loss.Internal > 0 || loss.Gone {
			if hu.Locations == nil {
				hu.Locations = map[string]LocationLoss{}
			}
			hu.Locations[k] = loss
			hu.Damaged = true
		}
	}
	for _, c := range st.Crits {
		gone := isSet(c.Destroyed) || isSet(c.Destroying)
		hits := c.Hits + c.PendingHits
		engine := isEngineSlot(c.Name)
		if hits > 0 || gone { // spent ammunition is not damage: it is Ammo expended (AmmoBins), which a rearm fixes
			hu.Damaged = true
			hu.Crits = append(hu.Crits, CritDamage{Loc: c.Loc, Engine: engine})
		}
		hu.AmmoUsed += c.Consumed
		if c.Consumed > 0 {
			hu.AmmoBins++
			hu.AmmoUsed2 = append(hu.AmmoUsed2, AmmoUse{Name: c.Name, Loc: c.Loc, Used: c.Consumed})
		}
		// Each destroyed slot (or hit) of these components is a critical hit on it, which the crippling rules count.
		n := hits
		if gone && n < 1 {
			n = 1
		}
		if engine {
			hu.EngineHits += n
		}
		// The cockpit is gone when its slot is destroyed, or (below) when the location that holds it is destroyed.
		if name := strings.ToLower(c.Name); gone && strings.Contains(name, "cockpit") && !strings.Contains(name, "command") {
			hu.CockpitGone = true
		}
	}
	// Equipment MekBay records on the inventory rather than on a critical slot: destroyed is damage like a destroyed slot (a repair fixes it), consumed is
	// spent ammunition like a bin's (a rearm refills it).
	for _, it := range st.Inventory {
		name, loc := inventoryRef(it.ID)
		destroyed := it.Destroyed != nil && *it.Destroyed
		if it.Destroying != nil {
			destroyed = *it.Destroying // pending, as the record sheet shows it
		}
		if destroyed {
			hu.Damaged = true
			hu.Crits = append(hu.Crits, CritDamage{Loc: loc})
		}
		if it.Consumed > 0 {
			hu.AmmoUsed += it.Consumed
			hu.AmmoBins++
			hu.AmmoUsed2 = append(hu.AmmoUsed2, AmmoUse{Name: name, Loc: loc, Used: it.Consumed})
		}
	}
	// A mech's cockpit is in its head unless the catalog says the design has it elsewhere (a torso-mounted cockpit); a unit the catalog doesn't know is taken to
	// have it in the head (every standard head has 3 points of internal structure).
	cockpitLoc, tons := "HD", 0.0
	if cu, ok := catalog.ByName(unit); ok {
		tons = cu.Tons
		if cu.CockpitLoc != "" {
			cockpitLoc = cu.CockpitLoc
		}
	}
	if InternalGone(hu, cockpitLoc, tons) {
		hu.CockpitGone = true
	}
	if raw, ok := st.RuleChecks[torsoCheckKey]; ok {
		var check struct {
			Status  string `json:"status"`
			Trigger string `json:"trigger"`
		}
		if json.Unmarshal(raw, &check) == nil {
			hu.TorsoCheckFailed, hu.TorsoCheckTrigger = check.Status == "failed", check.Trigger
		}
	}
	return hu
}

// torsoCheckKey is the rule check MekBay stores for the torso crippling check (TORSO_CRIPPLE_CHECK_KEY in mek-rules.ts).
const torsoCheckKey = "core.torso-crippling"

// destroysLocation says whether a location condition destroys the location whatever its structure: blown off or flooded (MekBay's isLocationDestroyedByCondition).
func destroysLocation(key string) bool { return key == "blown-off" || key == "flooded" }

// conditionKey reads one saved condition (MekBay's SerializedCondition: a key, or {key, value, pending}); "" for anything else.
func conditionKey(raw json.RawMessage) string {
	var key string
	if json.Unmarshal(raw, &key) == nil {
		return strings.TrimSpace(key)
	}
	var obj struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return strings.TrimSpace(obj.Key)
	}
	return ""
}

// inventoryRef reads an inventory item's ID (MekBay's "<name>@<location>#<index>", see inventory-component-reference.model.ts) as its name and location.
func inventoryRef(id string) (name, loc string) {
	name = id
	if at := strings.LastIndex(id, "@"); at >= 0 {
		name, loc = id[:at], id[at+1:]
		if hash := strings.Index(loc, "#"); hash >= 0 {
			loc = loc[:hash]
		}
	}
	return name, strings.TrimSpace(loc)
}

// PilotKilled says whether the crew in a battle force's unit died in that battle, as MekBay's record sheet shows it: their own record says so, or the cockpit
// they sit in is gone. It is only for a battle's forces, whose crews are the ones that fought: a hangar unit's crew is reset to an unnamed 4/5 after every
// battle, so there only CrewDead speaks for its crew, and a cockpit left unrepaired is the unit's damage, not a dead pilot.
func (u HangarUnit) PilotKilled() bool { return u.CrewDead || u.CockpitGone }

// IsDestroyed says whether the unit counts as destroyed: MekBay's destroyed flag, or its cockpit gone. MekBay's record sheet sets the flag itself when every
// cockpit is lost (evaluateDestroyed in mek-rules.ts), and so does the battle write-back (CommitState), so the second is a defensive check for damage written
// otherwise. Either way only the destroyed-level repair brings the unit back (see ServiceUnitState). Only the crew's cockpit counts, never CrewDead: a hangar's
// crew is reset after every battle. Where the catalog doesn't know a design, its cockpit is taken to be in the head.
func (u HangarUnit) IsDestroyed() bool { return u.Destroyed || u.CockpitGone }

// isEngineSlot reports whether a critical slot with this name belongs to the engine.
func isEngineSlot(name string) bool { return strings.Contains(strings.ToLower(name), "engine") }

// Session is one registered WebSocket connection.
type Session struct {
	conn *websocket.Conn
	id   string
	uuid string
}

// NewSession wraps a WebSocket connection already open to MekBay (or to something that relays to it, such as the site's proxy) as a session that
// acts as the account uid. It does not register the session.
func NewSession(conn *websocket.Conn, uid string) *Session {
	return &Session{conn: conn, id: NewID(), uuid: uid}
}

// Dial opens a session to MekBay's WebSocket at url as the account uid.
func Dial(ctx context.Context, url, uid string) (*Session, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not reach MekBay: %w", err)
	}
	conn.SetReadLimit(16 << 20)
	s := &Session{conn: conn, id: NewID(), uuid: uid}
	if err := s.Send(ctx, map[string]any{"action": "register", "uuid": uid, "version": Protocol}); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) Close() { _ = s.conn.CloseNow() }

// Send writes one message, stamped with the session and a fresh request ID.
func (s *Session) Send(ctx context.Context, msg map[string]any) error {
	_, err := s.sendID(ctx, msg)
	return err
}

func (s *Session) sendID(ctx context.Context, msg map[string]any) (string, error) {
	rid := NewID()
	msg["sessionId"], msg["requestId"] = s.id, rid
	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	if err := s.conn.Write(ctx, websocket.MessageText, b); err != nil {
		return "", fmt.Errorf("could not reach MekBay: %w", err)
	}
	return rid, nil
}

// Call sends a message and returns the raw reply that carries its request ID. MekBay reports refusals as
// "error" or "warning" replies (e.g. code not_owner), which become Go errors.
func (s *Session) Call(ctx context.Context, msg map[string]any) (json.RawMessage, error) {
	rid, err := s.sendID(ctx, msg)
	if err != nil {
		return nil, err
	}
	for {
		_, b, err := s.conn.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("could not reach MekBay: %w", err)
		}
		var head struct {
			Action    string `json:"action"`
			RequestID string `json:"requestId"`
			Code      string `json:"code"`
			Message   string `json:"message"`
		}
		if json.Unmarshal(b, &head) != nil || head.RequestID != rid {
			continue // userState, lobbyResumeResult and replies to other requests
		}
		switch {
		case head.Code == "not_owner":
			return nil, errors.New("MekBay says that user ID doesn't own this hangar, so it can't be changed")
		case head.Action == "error" || head.Action == "warning":
			return nil, fmt.Errorf("MekBay refused the request: %s", head.Message)
		}
		return b, nil
	}
}

// getForce reads one force's saved data (the decoded JSON of its `data`) as the session's account. It returns nil, nil when MekBay has no such force (or, with
// ownedOnly, when the account doesn't own it).
func (s *Session) getForce(ctx context.Context, instance string, ownedOnly bool) (map[string]any, error) {
	raw, err := s.Call(ctx, getForceMessage(s.uuid, instance, ownedOnly))
	if err != nil {
		return nil, err
	}
	var reply struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("could not read MekBay's reply: %w", err)
	}
	return reply.Data, nil
}

// getForceMessage asks for one force's saved data as the account uid.
func getForceMessage(uid, instance string, ownedOnly bool) map[string]any {
	return map[string]any{"action": "getForce", "forcePersistenceRevision": PersistenceRevision, "uuid": uid, "instanceId": instance, "ownedOnly": ownedOnly}
}

// delForceMessage deletes one force the account uid owns. MekBay does not acknowledge it.
func delForceMessage(uid, instance string) map[string]any {
	return map[string]any{"action": "delForce", "forcePersistenceRevision": PersistenceRevision, "uuid": uid, "instanceId": instance}
}

// saveData saves a force (anything that encodes as one) as the session's account; only the account that owns a force may save it. A version 2 force is saved
// as MekBay's version 2 writer saves one (force-persistence.service.ts).
func (s *Session) saveData(ctx context.Context, force any) error {
	msg := map[string]any{"action": "saveForce", "uuid": s.uuid, "data": force, "savedForceCount": 1}
	if f, ok := force.(map[string]any); ok && ForceVersion(f) == 2 {
		msg["forcePersistenceRevision"] = PersistenceRevision
		msg["cbtPersistence"] = map[string]any{"writerVersion": 2, "expectedRevision": nil}
	}
	_, err := s.Call(ctx, msg)
	return err
}

// NewID returns a new version 7 UUID, the kind MekBay uses for force and operation IDs.
func NewID() string { return uuid.NewV7().String() }

// serializedForce mirrors the parts of MekBay's SerializedForce this site writes.
type serializedForce struct {
	Version    int               `json:"version"`
	Timestamp  string            `json:"timestamp"`
	InstanceID string            `json:"instanceId"`
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Groups     []serializedGroup `json:"groups"`
	BV         int               `json:"bv"`
}

type serializedGroup struct {
	ID    string           `json:"id"`
	Name  string           `json:"name,omitempty"`
	Color string           `json:"color"`
	Units []serializedUnit `json:"units"`
}

type serializedUnit struct {
	ID        string          `json:"id"`
	Unit      string          `json:"unit"`
	Alias     string          `json:"alias"`
	UpdatedTs int64           `json:"updatedTs"`
	Commander bool            `json:"commander,omitempty"`
	State     serializedState `json:"state"`
}

type serializedState struct {
	Crew      []serializedCrew          `json:"crew"`
	Crits     []any                     `json:"crits"`
	Heat      map[string]int            `json:"heat"`
	Locations map[string]map[string]int `json:"locations"`
	Modified  bool                      `json:"modified"`
	Destroyed bool                      `json:"destroyed"`
	TurnState map[string]bool           `json:"turnState"`
}

type serializedCrew struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Gunnery  int    `json:"gunnerySkill"`
	Piloting int    `json:"pilotingSkill"`
	Hits     int    `json:"hits"`
	State    int    `json:"state"`
}

// BuildForce turns a draft into a pristine (undamaged) Classic BattleTech force in MekBay's save format.
func BuildForce(instanceID string, now time.Time, d ForceDraft) serializedForce {
	f := serializedForce{Version: 1, Timestamp: FormatTime(now), InstanceID: instanceID, Type: "cbt", Name: d.Name, Groups: []serializedGroup{}}
	for _, g := range d.Groups {
		sg := serializedGroup{ID: NewID(), Name: g.Name, Units: []serializedUnit{}}
		for _, u := range g.Units {
			f.BV += u.BV
			sg.Units = append(sg.Units, serializedUnit{
				ID: NewID(), Unit: u.Unit, UpdatedTs: now.UnixMilli(), Commander: u.Commander,
				State: serializedState{
					Crew:      []serializedCrew{{Name: u.Pilot, Gunnery: u.Gunnery, Piloting: u.Piloting}},
					Crits:     []any{},
					Heat:      map[string]int{"current": 0, "previous": 0},
					Locations: map[string]map[string]int{},
					TurnState: map[string]bool{"applyMovePSR": false},
				},
			})
		}
		f.Groups = append(f.Groups, sg)
	}
	return f
}

// CrewIsDead says whether MekBay's record of a crew member shows them dead: its stored state is dead (2) or killed (4), or they have taken the six hits that kill.
// (The other states are healthy 0, unconscious 1, ejected 3 and stunned 5.)
func CrewIsDead(state, hits int) bool { return state == 2 || state == 4 || hits >= 6 }

// operationMessage reads or deletes an operation. MekBay refuses either for an operation holding a version 2 force unless it carries the persistence
// revision, as it does for the force itself.
func operationMessage(action, operationID string) map[string]any {
	return map[string]any{"action": action, "forcePersistenceRevision": PersistenceRevision, "operationId": operationID}
}
