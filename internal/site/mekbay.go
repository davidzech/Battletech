package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// MekBay's server is closed source and has no documented API: this speaks the WebSocket protocol its own web
// app uses (client source: github.com/MegaMek/mekbay, ws.service.ts and data.service.ts). It is unofficial and
// may break whenever MekBay changes it.
const (
	mekbayWS       = "wss://mekbay.com/ws"
	mekbayProtocol = 2 // PROTOCOL_VERSION in ws.service.ts
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
	Unit     string
	BV       int
	Pilot    string
	Gunnery  int
	Piloting int
}

// HangarUnit is one unit of a player's hangar force as MekBay stores it.
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

type HangarUnit struct {
	ID        string // the unit's own ID inside the force
	Unit      string // MekBay unit ID, e.g. BMMadCat_H
	Group     string // name of the group it currently sits in
	Pilot     string
	Gunnery   int
	Piloting  int
	PilotDead bool // the record sheet shows the pilot dead (see crewIsDead)
	Damaged   bool // has recorded armor/structure damage, critical hits or spent ammo
	Destroyed bool
	// The raw damage MekBay records, from which conditionOf works out the unit's state (see condition.go).
	Locations  map[string]LocationLoss // armor and internal structure points lost, by location (HD, CT, LT, RT, LA, RA, LL, RL ...)
	AmmoUsed   int                     // ammo consumed, summed over its ammo bins
	AmmoBins   int                     // ammo bins with some consumed (each counts as a ton to rearm)
	AmmoUsed2  []AmmoUse               // the ammo bins with some used, for the rearm breakdown
	Crits      []CritDamage            // critical slots that are hit or destroyed (ammo use is not damage), to know what a repair would fix
	EngineHits int                     // destroyed engine slots
	// TorsoCheckFailed is MekBay's stored result of the torso crippling check (rule check core.torso-crippling).
	TorsoCheckFailed bool
}

// HangarForce is a player's campaign force, read from MekBay.
type HangarForce struct {
	Name      string
	Timestamp string
	Units     []HangarUnit
}

// MekBay reads and creates things on mekbay.com. It is an interface so tests can run without the network.
type MekBay interface {
	// ReadHangar reads a saved force (anyone can read one by its instance ID).
	ReadHangar(ctx context.Context, instance string) (HangarForce, error)
	// CloneHangar copies the player's force into a clean force the site owns (see clone.go).
	CloneHangar(ctx context.Context, req CloneRequest) (HangarForce, error)
	// ServiceUnit repairs or rearms one unit of the player's hangar force (modify mode only).
	ServiceUnit(ctx context.Context, req ServiceRequest) error
	// PurchaseUnits adds bought units to the player's hangar force (modify mode only).
	PurchaseUnits(ctx context.Context, req PurchaseRequest) error
	// SellUnit removes a sold unit from the player's hangar force.
	SellUnit(ctx context.Context, req SellRequest) error
	// SwapUnit reconfigures an omnimech of the player's hangar force to another variant.
	SwapUnit(ctx context.Context, req SwapRequest) error
	// StartBattle builds the players' forces and the OpFor's and one operation that pairs them (see battle.go).
	StartBattle(ctx context.Context, req BattleRequest) (BattleResult, error)
	// ReadBattle reads an operation and every force in it, anonymously, for the battle's live status.
	ReadBattle(ctx context.Context, operationID string) (BattleStatus, error)
	// CommitBattle writes the units' state from a finished battle back into the matching hangar units (see commitbattle.go).
	CommitBattle(ctx context.Context, req CommitBattleRequest) (CommitBattleReport, error)
	// CopyForce saves a copy of one of the owner's forces under another instance ID (checkpoints, see checkpoint.go).
	CopyForce(ctx context.Context, req CopyRequest) error
	// DeleteForce deletes forces of the owner's (best effort).
	DeleteForce(ctx context.Context, owner string, instances ...string) error
	// DeleteBattle deletes a battle's operation and all its forces (best effort).
	DeleteBattle(ctx context.Context, owner, operationID string) error
}

// MekBayClient is the real implementation, speaking to MekBay's WebSocket server.
type MekBayClient struct {
	URL string // defaults to wss://mekbay.com/ws
}

func (c *MekBayClient) url() string {
	if c.URL == "" {
		return mekbayWS
	}
	return c.URL
}

// ReadHangar reads a force anonymously, with a throwaway identity.
func (c *MekBayClient) ReadHangar(ctx context.Context, instance string) (HangarForce, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	s, err := dialMekBay(ctx, c.url(), newID())
	if err != nil {
		return HangarForce{}, err
	}
	defer s.close()
	raw, err := s.call(ctx, map[string]any{"action": "getForce", "uuid": s.uuid, "instanceId": instance, "ownedOnly": false})
	if err != nil {
		return HangarForce{}, err
	}
	return parseHangarForce(raw)
}

// mekbayTime formats a time the way MekBay writes force timestamps.
func mekbayTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// parseHangarForce reads a getForce reply into a HangarForce.
func parseHangarForce(raw json.RawMessage) (HangarForce, error) {
	var reply struct {
		Data *struct {
			Name      string `json:"name"`
			Timestamp string `json:"timestamp"`
			Groups    []struct {
				Name  string `json:"name"`
				Units []struct {
					ID    string `json:"id"`
					Unit  string `json:"unit"`
					State struct {
						Destroyed bool `json:"destroyed"`
						Locations map[string]struct {
							Armor    int `json:"armor"`
							Internal int `json:"internal"`
						} `json:"locations"`
						Crits []struct {
							Name      string `json:"name"`
							Loc       string `json:"loc"`
							Hits      int    `json:"hits"`
							Consumed  int    `json:"consumed"`
							Destroyed any    `json:"destroyed"`
						} `json:"crits"`
						Crew []struct {
							Name     string `json:"name"`
							Gunnery  int    `json:"gunnerySkill"`
							Piloting int    `json:"pilotingSkill"`
							Hits     int    `json:"hits"`
							State    int    `json:"state"`
						} `json:"crew"`
						RuleChecks map[string]struct {
							Status string `json:"status"`
						} `json:"ruleChecks"`
					} `json:"state"`
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
			hu := HangarUnit{ID: u.ID, Unit: u.Unit, Group: g.Name, Destroyed: u.State.Destroyed, Gunnery: defaultGunnery, Piloting: defaultPiloting}
			if len(u.State.Crew) > 0 {
				c := u.State.Crew[0]
				hu.Pilot, hu.Gunnery, hu.Piloting = c.Name, c.Gunnery, c.Piloting
				hu.PilotDead = crewIsDead(c.State, c.Hits)
			}
			hu.Damaged = len(u.State.Locations) > 0
			for k, l := range u.State.Locations {
				if hu.Locations == nil {
					hu.Locations = map[string]LocationLoss{}
				}
				hu.Locations[k] = LocationLoss{Armor: l.Armor, Internal: l.Internal}
			}
			for _, c := range u.State.Crits {
				gone := c.Destroyed != nil && c.Destroyed != float64(0)
				if c.Hits > 0 || c.Consumed > 0 || gone {
					hu.Damaged = true
				}
				if c.Hits > 0 || gone {
					hu.Crits = append(hu.Crits, CritDamage{Loc: c.Loc, Engine: strings.Contains(strings.ToLower(c.Name), "engine")})
				}
				hu.AmmoUsed += c.Consumed
				if c.Consumed > 0 {
					hu.AmmoBins++
					hu.AmmoUsed2 = append(hu.AmmoUsed2, AmmoUse{Name: c.Name, Loc: c.Loc, Used: c.Consumed})
				}
				// Each destroyed slot (or hit) of these components is a critical hit on it, which the crippling rules count.
				n := c.Hits
				if gone && n < 1 {
					n = 1
				}
				if strings.Contains(strings.ToLower(c.Name), "engine") {
					hu.EngineHits += n
				}
			}
			// MekBay also counts a pilot dead when their cockpit is gone, whatever the crew's stored state says: the cockpit slot destroyed, or the head (which holds it)
			// with no internal structure left. Every standard mech's head has 3 points of internal structure.
			for _, c := range u.State.Crits {
				name := strings.ToLower(c.Name)
				if strings.Contains(name, "cockpit") && !strings.Contains(name, "command") && c.Destroyed != nil && c.Destroyed != float64(0) {
					hu.PilotDead = true
				}
			}
			if hd, ok := hu.Locations["HD"]; ok && hd.Internal >= 3 && len(u.State.Crew) > 0 {
				hu.PilotDead = true
			}
			hu.TorsoCheckFailed = u.State.RuleChecks["core.torso-crippling"].Status == "failed"
			h.Units = append(h.Units, hu)
		}
	}
	return h, nil
}

// mekbaySession is one registered WebSocket connection.
type mekbaySession struct {
	conn *websocket.Conn
	id   string
	uuid string
}

func dialMekBay(ctx context.Context, url, uid string) (*mekbaySession, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not reach MekBay: %w", err)
	}
	conn.SetReadLimit(16 << 20)
	s := &mekbaySession{conn: conn, id: newID(), uuid: uid}
	if err := s.send(ctx, map[string]any{"action": "register", "uuid": uid, "version": mekbayProtocol}); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *mekbaySession) close() { _ = s.conn.CloseNow() }

// send writes one message, stamped with the session and a fresh request ID.
func (s *mekbaySession) send(ctx context.Context, msg map[string]any) error {
	_, err := s.sendID(ctx, msg)
	return err
}

func (s *mekbaySession) sendID(ctx context.Context, msg map[string]any) (string, error) {
	rid := newID()
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

// call sends a message and returns the raw reply that carries its request ID. MekBay reports refusals as
// "error" or "warning" replies (e.g. code not_owner), which become Go errors.
func (s *mekbaySession) call(ctx context.Context, msg map[string]any) (json.RawMessage, error) {
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

// errNoHangarForce is what the hangar edits answer when MekBay no longer has the site's copy of the hangar.
var errNoHangarForce = errors.New("MekBay no longer has the force for your hangar")

// getForce reads one force's saved data (the decoded JSON of its `data`) as the session's account. It returns nil, nil when MekBay has no such force (or, with
// ownedOnly, when the account doesn't own it).
func (s *mekbaySession) getForce(ctx context.Context, instance string, ownedOnly bool) (map[string]any, error) {
	raw, err := s.call(ctx, map[string]any{"action": "getForce", "uuid": s.uuid, "instanceId": instance, "ownedOnly": ownedOnly})
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

// saveData saves a force (anything that encodes as one) as the session's account; only the account that owns a force may save it.
func (s *mekbaySession) saveData(ctx context.Context, force any) error {
	_, err := s.call(ctx, map[string]any{"action": "saveForce", "uuid": s.uuid, "data": force, "savedForceCount": 1})
	return err
}

// saveForce saves a force read with getForce after changing it: the "owned" flag MekBay sets for readers is dropped (the app never sends it back) and the
// timestamp is set to now, so every device that has the force loaded takes the new version.
func (s *mekbaySession) saveForce(ctx context.Context, force map[string]any, now time.Time) error {
	delete(force, "owned")
	force["timestamp"] = mekbayTime(now)
	return s.saveData(ctx, force)
}

// editForce reads one of the owner's forces, changes it with edit and saves it, on one connection. It is how every change to the site's copy of a hangar is
// made (repairs, rearms, purchases, sales, swaps).
func (c *MekBayClient) editForce(ctx context.Context, owner, instance string, edit func(force map[string]any, now time.Time) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := dialMekBay(ctx, c.url(), owner)
	if err != nil {
		return err
	}
	defer s.close()
	force, err := s.getForce(ctx, instance, false)
	if err != nil {
		return err
	}
	if force == nil {
		return errNoHangarForce
	}
	now := time.Now()
	if err := edit(force, now); err != nil {
		return err
	}
	return s.saveForce(ctx, force, now)
}

func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err) // only fails if the system has no randomness
	}
	return id.String()
}

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

// buildForce turns a draft into a pristine (undamaged) Classic BattleTech force in MekBay's save format.
func buildForce(instanceID string, now time.Time, d ForceDraft) serializedForce {
	f := serializedForce{Version: 1, Timestamp: mekbayTime(now), InstanceID: instanceID, Type: "cbt", Name: d.Name, Groups: []serializedGroup{}}
	for _, g := range d.Groups {
		sg := serializedGroup{ID: newID(), Name: g.Name, Units: []serializedUnit{}}
		for _, u := range g.Units {
			f.BV += u.BV
			sg.Units = append(sg.Units, serializedUnit{
				ID: newID(), Unit: u.Unit, UpdatedTs: now.UnixMilli(),
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

// crewIsDead says whether MekBay's record of a crew member shows them dead: its stored state is dead (2) or killed (4), or they have taken the six hits that kill.
// (The other states are healthy 0, unconscious 1, ejected 3 and stunned 5.)
func crewIsDead(state, hits int) bool { return state == 2 || state == 4 || hits >= 6 }
