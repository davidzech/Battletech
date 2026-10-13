# BattleTech Campaigns

A collection of custom BattleTech tabletop campaigns, each in its own folder under `campaigns/`.

Campaigns are plain Markdown, YAML front matter and MegaMek force files. They are meant to be easy to read and write by hand, and structured enough to drive a campaign website later (scenario pages, branching between scenarios, and campaign-wide keywords).

## Campaigns

| Folder | Campaign |
|---|---|
| [`campaigns/RefusalWar/`](campaigns/RefusalWar) | The Refusal War between Clan Wolf and Clan Jade Falcon, inspired by *MechWarrior 2* |

## Campaign layout

Each campaign folder follows the same shape:

```
campaigns/<Campaign>/
  Intro.md              Campaign introduction
  Keywords.md           Registry of campaign-wide keywords
  Scenario_NNN/
    Setup.md            Fluff, map, forces, deployment, special rules
    Outcome.md          Victory conditions and rewards
    OpFor1.mul, ...     MegaMek force files for the opposing forces
```

Scenarios are numbered from `Scenario_000`. In the Refusal War, `Scenario_000` is the "Trial of Position", a graduation mission that sets each pilot's starting SP.

## Shared data

```
data/
  units.json            Vendored copy of MekBay's unit catalog, shared by all campaigns
```

The `.mul` files are in MegaMek's force format, so they can be loaded into [MekBay](https://mekbay.com) or MegaMek. Mech names in the scenario write-ups link to their MekBay pages.

`data/units.json` is a copy of the catalog served at `https://db.mekbay.com/units.json`, kept here so mech names and IDs can be looked up offline. To refresh it, download that URL over the file:

```bash
curl --compressed -o data/units.json https://db.mekbay.com/units.json
```

## Website

A small Go web server renders the campaign folders. It needs [Go](https://go.dev/dl/) 1.26 or newer:

```bash
go run .     # then open http://localhost:8080
```

Visitors pick or generate a **seed** in a popup (no login) and give their MekBay hangar link and number of human players; that identifies their playthrough, which is stored in a local SQLite file (`playthroughs.db`, git-ignored; change it with `-db`). It re-reads the Markdown on every request, so edits show up when you refresh. Use `-addr` to change the port and `-content` to point at a different campaigns folder (default `campaigns`).

## Notes

- Guidance for working on this repo with Claude Code is in [CLAUDE.md](CLAUDE.md).
- This is an unofficial fan project. BattleTech and MechWarrior are trademarks of their respective owners. Unit data comes from [MekBay](https://github.com/MegaMek/mekbay) and the [MegaMek](https://megamek.org) project; see their repositories for licensing.
