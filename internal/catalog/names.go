package catalog

import (
	"net/url"
	"regexp"
	"strings"
)

var chassisNames = regexp.MustCompile(`^(.*?)\s*\((.+)\)$`)

// Names returns the name to show for a unit and its model. A design with two names is written "Inner Sphere
// name (Clan name)" in the catalog, and the Clan name always wins: "Vulture (Mad Dog)" is shown as "Mad Dog". Only
// Clan-tech designs follow that pattern; the few Inner Sphere ones with a parenthesis ("Katana (Crockett)") have no
// Clan name, so they keep their first name.
func Names(u Unit) (name, model string) {
	name = u.Chassis
	if m := chassisNames.FindStringSubmatch(u.Chassis); m != nil {
		if u.TechBase == "Clan" {
			name = m[2]
		} else {
			name = m[1]
		}
	}
	return name, u.Model
}

// RoleLabel is the role shown in a mech row's band. The catalog gives a few mechs the role "None", which would read
// as a label in its own right, so those show their weight class instead (the first part of it, for the few classes
// that are long lists).
func RoleLabel(u Unit) string {
	if r := strings.TrimSpace(u.Role); r != "" && !strings.EqualFold(r, "none") {
		return r
	}
	// A few classes are long lists ("Ultra Light/PA(L)/Exoskeleton"): the first part is the label.
	class, _, _ := strings.Cut(u.Class, "/")
	return strings.TrimSpace(class)
}

// DisplayName is a unit's name and model on one line, e.g. "Mad Dog Prime".
func DisplayName(u Unit) string {
	name, model := Names(u)
	return strings.TrimSpace(name + " " + model)
}

// PageURL is the link to a unit's page on mekbay.com, from its MekBay unit ID (e.g. BMMadCat_H): the General tab of that design's page,
// not a unit in anyone's force. "" if there is no ID.
func PageURL(unitID string) string {
	if unitID == "" {
		return ""
	}
	return "https://mekbay.com/?" + url.Values{"gs": {"cbt"}, "shareUnit": {unitID}, "tab": {"General"}}.Encode()
}
