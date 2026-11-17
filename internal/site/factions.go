package site

// A Faction is the side a campaign's player force fights for. It decides the cosmetic flavour of the player's named pilots,
// starting with their ranks. Each campaign names its faction in Intro.md front matter (`faction: clan`), so campaigns can
// differ: Refusal War is Clan against Clan, while a future campaign could field an Inner Sphere Royal House force with
// Sergeant, Lieutenant and so on. To add one, add an entry below and name its ID in that campaign's Intro.md.
//
// A campaign can also list its own ranks (`ranks:` in Intro.md, lowest first), which replaces its faction's list, for a
// campaign that needs a special ladder without a code change.
type Faction struct {
	ID    string
	Name  string
	Ranks []string // lowest first; the first is what every new pilot holds
}

// factions are the known factions by ID.
var factions = map[string]Faction{
	// The Clan warrior ranks (https://www.sarna.net/wiki/Clan_Military_Structure), from a lowly MechWarrior to the Khan.
	"clan": {
		ID:   "clan",
		Name: "Clan",
		Ranks: []string{
			"MechWarrior",
			"Star Commander",
			"Star Captain",
			"Star Colonel",
			"Galaxy Commander",
			"saKhan",
			"Khan",
		},
	},
}

// factionRanks is a faction's rank ladder, or nil for an unknown or empty faction ID (which turns ranks off).
func factionRanks(id string) []string {
	f, ok := factions[id]
	if !ok {
		return nil
	}
	return append([]string(nil), f.Ranks...)
}

// rankFor is the rank to show for a pilot who holds `stored`: that rank if the campaign has it, else the lowest rank (so a
// new pilot, or one whose old rank is no longer in the list, is simply the lowest). "" when the campaign has no ranks.
func rankFor(ranks []string, stored string) string {
	if len(ranks) == 0 {
		return ""
	}
	for _, r := range ranks {
		if r == stored {
			return stored
		}
	}
	return ranks[0]
}

// validRank reports whether the campaign has this rank.
func validRank(ranks []string, rank string) bool {
	for _, r := range ranks {
		if r == rank {
			return true
		}
	}
	return false
}
