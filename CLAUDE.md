# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

A documentation-only repo: Markdown files for a custom tabletop BattleTech campaign ("Refusal War") inspired by the MechWarrior 2 video game, a conflict between Clan Wolf and Clan Jade Falcon. There is no code, build, lint, or test tooling.

## Structure

- `RefusalWar/Intro.md` – campaign introduction (currently empty).
- `RefusalWar/Scenario_NNN/` – one folder per scenario, zero-padded and numbered sequentially, containing `Setup.md` (fluff, map, forces, special rules) and `Outcome.md` (victory conditions and rewards). `Scenario_000` is the "Trial of Position", a one-off graduation mission whose kill count determines each pilot's starting SP. Later sorties are referred to as "Sortie 001", etc.

## Scenario file conventions

Follow the layout in `Scenario_000/Setup.md` and `Scenario_000/Outcome.md` when adding scenarios:

- `Setup.md`: `# Scenario NNN: <Title>`, a fluff placeholder for narrative text still to be written (leave it in place; don't invent lore to replace it), then `## Overview`, `## Map & Deployment`, `## Forces` (player pick rules, then `### OpFor N` bullet lists written as `Variant (gunnery/piloting)`, e.g. `Timber Wolf H (3/4)`, with the mech name hyperlinked to MekBay as `https://mekbay.com/?gs=cbt&shareUnit=BM<IS-name>_<Variant>&tab=Sheet`; the unit ID is not always obvious: the chassis name has no spaces, the variant follows an underscore, and a chassis with no named variant takes no suffix. Some chassis use the Inner Sphere name (Mist Lynx is `BMKoshi_E`, Kit Fox is `BMUller_J`) while others don't (`BMMadCat_H` for Timber Wolf H, `BMLoki_Prime`, `BMGrizzly`). Ask the user for the working URL rather than guessing a new one) and `## Special Rules`.
- `Outcome.md`: `# Scenario NNN: <Title> - Outcome` with `## Victory & Rewards`. Rewards are in SP (the campaign's pilot-advancement points). The kills-to-SP table is specific to Scenario 000's graduation mission; don't assume later scenarios use it.
- Campaign rules visible so far: pilots are named and persistent; a pilot who dies or fails to score a kill in the graduation mission is permanently removed and replaced by a new named pilot with 0 SP for Sortie 001; Clan honor rules such as zellbrigen apply and violations have mechanical consequences.

## Force files (.mul)

Each scenario folder also holds `OpFor1.mul` and `OpFor2.mul`: MegaMek's XML force format (MekBay exports and imports it), one `<entity>` per mech with its `<pilot>` skills and `<Game id>`. Prefer a real MekBay export over hand-writing. When hand-writing, `chassis` and `model` must match MegaMek's database. A mech with two names uses `chassis="<Inner Sphere name> (<Clan name>)"`, e.g. `Koshi (Mist Lynx)` or `Mad Cat (Timber Wolf)`; a single-name mech such as the Grizzly has a plain chassis and an empty `model`. Deployment attributes, `externalId` and `quirks` can be omitted.

## Working notes

- Mech names and variants are canon BattleTech Clan designs (e.g. Mist Lynx, Timber Wolf, Shadow Cat, Loki); keep to real variant names and Clan terminology (touman, zellbrigen, Trial, etc.).
- For mech data, the authoritative sources are the Master Unit List (`https://masterunitlist.battletech.com/units/<chassis>-<variant>`, e.g. `shadow-cat-a`) and MekBay, not Sarna. The MUL site is behind a Cloudflare bot challenge and returns 403 to automated fetches.
- `data/units.json` is a vendored copy of MekBay's unit catalog, downloaded from `https://db.mekbay.com/units.json` on 2026-10-12 (~27 MB; MekBay builds it from MegaMek's `mm-data`). To refresh it, re-download that URL over the file (e.g. `curl --compressed -o data/units.json https://db.mekbay.com/units.json`). Search it with Grep/Python rather than reading it whole. Each entry's `chassis` and `model` are exactly what `.mul` files need, and its `name` field is the `shareUnit` ID for MekBay links (e.g. `BMMadCat_H`). MekBay's source is at `../mekbay`.
- Maps referenced are official BattleTech battlemats (e.g. "Circles of Equals").
