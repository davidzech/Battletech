# Refusal War

A custom BattleTech tabletop campaign inspired by *MechWarrior 2*, set during the Refusal War between Clan Wolf and Clan Jade Falcon.

This repo holds the campaign as plain Markdown, YAML front matter and MegaMek force files. It is meant to be easy to read and write by hand, and structured enough to drive a campaign website later (scenario pages, branching between scenarios, and campaign-wide keywords).

## Layout

```
RefusalWar/
  Intro.md              Campaign introduction
  Keywords.md           Registry of campaign-wide keywords
  Scenario_NNN/
    Setup.md            Fluff, map, forces, deployment, special rules
    Outcome.md          Victory conditions and rewards
    OpFor1.mul, ...     MegaMek force files for the opposing forces
data/
  units.json            Vendored copy of MekBay's unit catalog
```

Scenarios are numbered from `Scenario_000`, the "Trial of Position", a graduation mission that sets each pilot's starting SP. Later sorties follow as `Scenario_001` and so on.

## Force files

The `.mul` files are in MegaMek's force format, so they can be loaded into [MekBay](https://mekbay.com) or MegaMek. Mech names in the scenario write-ups link to their MekBay pages.

## Unit data

`data/units.json` is a copy of the catalog served at `https://db.mekbay.com/units.json`, kept here so mech names and IDs can be looked up offline. To refresh it, download that URL over the file:

```bash
curl --compressed -o data/units.json https://db.mekbay.com/units.json
```

## Notes

- Guidance for working on this repo with Claude Code is in [CLAUDE.md](CLAUDE.md).
- This is an unofficial fan project. BattleTech and MechWarrior are trademarks of their respective owners. Unit data comes from [MekBay](https://github.com/MegaMek/mekbay) and the [MegaMek](https://megamek.org) project; see their repositories for licensing.
