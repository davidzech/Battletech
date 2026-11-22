package content

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// MulEntity is one unit from a MegaMek/MekBay .mul force file.
type MulEntity struct {
	Chassis  string
	Model    string
	Pilot    string
	Gunnery  int
	Piloting int
}

// ParseMul reads the units out of a .mul file, in file order.
func ParseMul(data []byte) ([]MulEntity, error) {
	var doc struct {
		Entities []struct {
			Chassis string `xml:"chassis,attr"`
			Model   string `xml:"model,attr"`
			Pilot   struct {
				Name     string `xml:"name,attr"`
				Gunnery  int    `xml:"gunnery,attr"`
				Piloting int    `xml:"piloting,attr"`
			} `xml:"pilot"`
		} `xml:"entity"`
	}
	if err := xml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("read .mul: %w", err)
	}
	out := make([]MulEntity, 0, len(doc.Entities))
	for _, e := range doc.Entities {
		out = append(out, MulEntity{Chassis: e.Chassis, Model: e.Model, Pilot: e.Pilot.Name, Gunnery: e.Pilot.Gunnery, Piloting: e.Pilot.Piloting})
	}
	return out, nil
}
