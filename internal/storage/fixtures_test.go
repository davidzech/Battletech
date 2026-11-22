package storage

import (
	"github.com/davidzech/Battletech/internal/mekbay"
)

const (
	testAccount  = "0f1e2d3c-4b5a-4968-8776-655443322110"
	otherAccount = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

const (
	goodInstance = "01a12f9e-188f-7158-ad67-3fc9700ec733"
	goodSel      = "01a12f9d-a79b-717d-8c84-d5da73d2f9a5"
	// goodHangar is a link as a player pastes it, with a unit selection; the site keeps only goodHangarURL.
	goodHangar    = "https://mekbay.com/?instance=" + goodInstance + "&sel=" + goodSel
	goodHangarURL = "https://mekbay.com/?instance=" + goodInstance
)

// hangarOf builds a hangar of standard 4/5 units from MekBay unit IDs.
func hangarOf(names ...string) mekbay.HangarForce {
	h := mekbay.HangarForce{Name: "Test Hangar", Timestamp: "t"}
	for i, n := range names {
		h.Units = append(h.Units, mekbay.HangarUnit{ID: "u" + string(rune('a'+i)), Unit: n, Group: "Hangar", Gunnery: 4, Piloting: 5})
	}
	return h
}
