package mtf

import (
	"crypto/sha1"
	"encoding/base64"
	"regexp"
	"strings"
)

// MekBay stamps each unit it saves with a canary of the design's source (nativeSourceHashCanary in source-hash-canary.ts): the first four characters of
// the SHA-1, in base64url, of the file with its artwork, comments, fluff and generator lines left out. A unit's saved state names slots and components of
// the revision with that canary.

var (
	artworkLine    = regexp.MustCompile(`(?im)^[^\S\r\n]*(fluffimage|icon)[^\S\r\n]*:([^\r\n]*)(?:\r\n|\r|\n|$)`)
	weaponsLine    = regexp.MustCompile(`(?i)^weapons\s*:`)
	ignoredFields  = map[string]bool{"generator": true, "iconpath": true, "overview": true, "capabilities": true, "deployment": true, "history": true, "manufacturer": true, "notes": true, "primaryfactory": true, "fluffdate": true, "systemmanufacturer": true, "systemmode": true}
	locationHeader = map[string]bool{"Left Arm:": true, "Right Arm:": true, "Left Torso:": true, "Right Torso:": true, "Center Torso:": true, "Head:": true,
		"Left Leg:": true, "Right Leg:": true, "Center Leg:": true, "Front Left Leg:": true, "Front Right Leg:": true, "Rear Left Leg:": true, "Rear Right Leg:": true}
)

// Canary is MekBay's source canary for a mech's .mtf file.
func Canary(src []byte) string {
	source := artworkLine.ReplaceAllString(string(src), "")
	source = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(source)
	inSection := false
	var locations [][]string
	var rows *[]string
	var kept []string
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			inSection, rows = false, nil
			if !strings.HasPrefix(line, "#") {
				kept = append(kept, line)
			}
			continue
		}
		header := locationHeader[line]
		if header {
			locations = append(locations, nil)
			rows = &locations[len(locations)-1]
		} else if rows != nil {
			*rows = append(*rows, line)
		}
		if header || weaponsLine.MatchString(line) {
			inSection = true
		}
		if inSection {
			kept = append(kept, line)
			continue
		}
		if sep := strings.Index(line, ":"); sep <= 0 || !ignoredFields[strings.ToLower(strings.TrimSpace(line[:sep]))] {
			kept = append(kept, line)
		}
	}
	normalized := strings.TrimSpace(strings.Join(kept, "\n"))
	for _, l := range locations {
		if closesHole(l) {
			normalized += "\npacked critical rows"
			break
		}
	}
	sum := sha1.Sum([]byte(normalized))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:4]
}

// closesHole says whether packing a location's rows moves one up into an empty row before it (mtfLocationRowsCloseHole).
func closesHole(lines []string) bool {
	hole := false
	for _, l := range lines {
		if l == "-Empty-" {
			hole = true
		} else if hole && !placedSystem(l) {
			return true
		}
	}
	return false
}
