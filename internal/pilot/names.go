package pilot

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// nameStyle allows the kinds of names the lore uses ("Wolf's Dragoons", "1st Somerset Strikers", "Kell Hounds").
var nameStyle = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} '’.,&:!()\-]*$`)

// ParseName validates and tidies a name: 1 to 40 characters, with runs of spaces collapsed. what ("account", "force") is only for the message.
func ParseName(raw, what string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 || !nameStyle.MatchString(name) {
		return "", errors.New("Name your " + what + " with 1 to 40 letters, numbers and spaces (apostrophes, hyphens and a few marks are fine).")
	}
	return name, nil
}
