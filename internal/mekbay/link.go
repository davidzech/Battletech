package mekbay

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Hangar is a MekBay hangar link reduced to what the site needs: the force's instance ID. Whatever else a pasted link
// carries (the selected unit, an operation, tracking parameters, enemy forces) is dropped.
type Hangar struct {
	URL      string // the canonical link, https://mekbay.com/?instance=<id>
	Instance string // the hangar force's instance ID
	// Source, on a run's hangar, is the link the player gave. URL and Instance are then the site's own copy of that force (see clone.go), and parseHangar,
	// which reads what the player pasted, leaves it empty.
	Source string
}

var UUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// friendlyInstance picks our own force out of a MekBay "instance" value, which lists forces separated by commas, the
// hostile ones prefixed "enemy:" (instance=<mine>,enemy:<theirs>). It is the first entry that isn't hostile, or "" if
// there is none or it isn't a valid ID.
func friendlyInstance(list string) string {
	for _, entry := range strings.Split(list, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if strings.HasPrefix(entry, "enemy:") {
			continue
		}
		if UUIDPattern.MatchString(entry) {
			return entry
		}
		return ""
	}
	return ""
}

// ParseHangarLink accepts any MekBay link to a hangar and reduces it to https://mekbay.com/?instance=<id>. The link must be
// https, on mekbay.com, with an instance ID. Extra parameters are harmless and are dropped, the unit selection
// (&sel=) among them; a list of forces ("<mine>,enemy:<theirs>") gives its friendly force.
func ParseHangarLink(raw string) (Hangar, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	host := ""
	if err == nil {
		host = strings.ToLower(u.Hostname())
	}
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (host != "mekbay.com" && host != "www.mekbay.com") {
		return Hangar{}, errors.New("Paste a MekBay hangar link, like https://mekbay.com/?instance=…")
	}
	q := u.Query()
	instance := friendlyInstance(q.Get("instance"))
	if instance == "" {
		if q.Get("operation") != "" && q.Get("instance") == "" {
			return Hangar{}, errors.New("That is an operation link. Paste the link to your hangar instead (it has ?instance=…).")
		}
		return Hangar{}, errors.New("That MekBay link has no valid instance id (?instance=…).")
	}
	return Hangar{URL: "https://mekbay.com/?instance=" + instance, Instance: instance}, nil
}
