package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// OperationURL is the link that opens an operation (its forces, with alignments) in MekBay.
func OperationURL(operationID string) string { return "https://mekbay.com/?operation=" + operationID }

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

// OperationRequest asks MekBay to (re)create an operation pairing the campaign's hangar force (friendly) with a
// synthesized OpFor force (enemy). OpForInstance and OperationID are reused when set, so repeating the request
// updates the same force and operation instead of creating new ones.
type OperationRequest struct {
	OwnerUID         string // MekBay account that owns the hangar; only its owner may save forces
	CampaignInstance string // the hangar force's instance ID
	OpFor            ForceDraft
	OpForInstance    string
	OperationName    string
	OperationID      string
}

// OperationResult identifies what was created.
type OperationResult struct {
	OpForInstance string
	OperationID   string
}

// MekBay creates operations on mekbay.com. It is an interface so tests can run without the network.
type MekBay interface {
	SyncOperation(ctx context.Context, req OperationRequest) (OperationResult, error)
}

// MekBayClient is the real implementation, speaking to MekBay's WebSocket server.
type MekBayClient struct {
	URL string // defaults to wss://mekbay.com/ws
}

// SyncOperation saves the OpFor force, then the operation, then reads the operation back to confirm it
// (MekBay doesn't acknowledge saveOperation).
func (c *MekBayClient) SyncOperation(ctx context.Context, req OperationRequest) (OperationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	url := c.URL
	if url == "" {
		url = mekbayWS
	}
	s, err := dialMekBay(ctx, url, req.OwnerUID)
	if err != nil {
		return OperationResult{}, err
	}
	defer s.close()

	camp, err := s.call(ctx, map[string]any{"action": "getForce", "uuid": req.OwnerUID, "instanceId": req.CampaignInstance, "ownedOnly": false})
	if err != nil {
		return OperationResult{}, err
	}
	var campaign struct {
		Data *struct {
			Timestamp string `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(camp, &campaign); err != nil || campaign.Data == nil {
		return OperationResult{}, errors.New("MekBay has no force for your hangar link; check that the link is saved in MekBay")
	}

	opforID := req.OpForInstance
	if opforID == "" {
		opforID = newID()
	}
	force := buildForce(opforID, time.Now().UTC(), req.OpFor)
	if _, err := s.call(ctx, map[string]any{"action": "saveForce", "uuid": req.OwnerUID, "data": force, "savedForceCount": 1}); err != nil {
		return OperationResult{}, err
	}

	opID := req.OperationID
	if opID == "" {
		opID = newID()
	}
	op := map[string]any{
		"operationId": opID,
		"name":        req.OperationName,
		"note":        "",
		"timestamp":   time.Now().UnixMilli(),
		"forces": []map[string]any{
			{"instanceId": req.CampaignInstance, "alignment": "friendly", "timestamp": campaign.Data.Timestamp},
			{"instanceId": opforID, "alignment": "enemy", "timestamp": force.Timestamp},
		},
	}
	if err := s.send(ctx, map[string]any{"action": "saveOperation", "data": op}); err != nil {
		return OperationResult{}, err
	}
	for attempt := 0; attempt < 6; attempt++ {
		select {
		case <-ctx.Done():
			return OperationResult{}, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		got, err := s.call(ctx, map[string]any{"action": "getOperation", "operationId": opID})
		if err != nil {
			return OperationResult{}, err
		}
		var back struct {
			Data *struct {
				Owned bool `json:"owned"`
			} `json:"data"`
		}
		if json.Unmarshal(got, &back) == nil && back.Data != nil {
			return OperationResult{OpForInstance: opforID, OperationID: opID}, nil
		}
	}
	return OperationResult{}, errors.New("MekBay did not confirm the operation was saved")
}

// mekbaySession is one registered WebSocket connection.
type mekbaySession struct {
	conn *websocket.Conn
	id   string
}

func dialMekBay(ctx context.Context, url, ownerUID string) (*mekbaySession, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not reach MekBay: %w", err)
	}
	conn.SetReadLimit(16 << 20)
	s := &mekbaySession{conn: conn, id: newID()}
	if err := s.send(ctx, map[string]any{"action": "register", "uuid": ownerUID, "version": mekbayProtocol}); err != nil {
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
	f := serializedForce{Version: 1, Timestamp: now.Format("2006-01-02T15:04:05.000Z"), InstanceID: instanceID, Type: "cbt", Name: d.Name, Groups: []serializedGroup{}}
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
