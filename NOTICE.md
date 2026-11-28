# Notices

This is an unofficial fan project. BattleTech, BattleMech, 'Mech, MechWarrior and AeroTech are registered trademarks of
The Topps Company, Inc. MechWarrior Copyright Microsoft Corporation. This project is not endorsed by or affiliated with
The Topps Company, Catalyst Game Labs, Microsoft, Piranha Games, the MegaMek Team or MekBay.

## This project's code

The website's code (the Go server and its generators, templates, stylesheets and scripts) is licensed under the
**GNU General Public License v3.0 or later**; see [LICENSE](LICENSE). Parts of it follow MekBay's own code (the
crippled-unit rules in `internal/mekbay/condition.go` and the firepower and range figures worked out by
`internal/catalog/gen_units.go`), which is also GPL-3.0.

The GPL does not cover the material listed below, which keeps its own license.

## Material under other licenses

| What | Where | Source | License |
|---|---|---|---|
| Unit catalog, factions and eras | `data/units.json`, `data/factions.json`, `data/eras.json` | [MekBay](https://mekbay.com) (`db.mekbay.com`), built from [MegaMek Data](https://github.com/MegaMek/mm-data) | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/), as MegaMek Data |
| Unit index and availability generated from that catalog | `internal/catalog/units_index.json`, `internal/catalog/availability.json` | derived from the files above | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/) |
| Mech layouts and equipment catalog derived from MekBay's unit files | `internal/layouts/layouts.json.gz`, `internal/layouts/equipment.json.gz` | built by `internal/layouts/gen_layouts.go` from [MegaMek Data](https://github.com/MegaMek/mm-data) unit files as published by [MekBay](https://mekbay.com) | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/), as MegaMek Data |
| Mech unit files and equipment catalog used as test fixtures | `internal/mtf/testdata/` (`.mtf` files, `equipment.json` excerpt) | [MegaMek Data](https://github.com/MegaMek/mm-data) `data/mekfiles`, as published by [MekBay](https://mekbay.com) (`online-assets/generated/units`, `online-assets/static/equipment.json`) | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/), as MegaMek Data |
| Unit icons | `internal/catalog/icons/` | [MegaMek Data](https://github.com/MegaMek/mm-data), `data/images/units` | [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/); see `internal/catalog/icons/NOTICE.md` |
| Weapon type icons | `internal/site/static/weapon-*.svg` | [MekBay](https://github.com/MegaMek/mekbay) | GPL-3.0-or-later; see `internal/site/static/weapon-icons-NOTICE.txt` |
| Pilot portraits | `internal/pilot/portraits/` | MechWarrior 5 (Piranha Games / Microsoft) | used under Microsoft's [Game Content Usage Rules](https://www.xbox.com/en-US/developers/rules); see `internal/pilot/portraits/NOTICE.md` |
| QR code generator | `internal/site/static/qrcode.js` | qrcode-generator, (C) 2009 Kazuhiko Arase | MIT; see `internal/site/static/qrcode-LICENSE.txt` |
| BattleTech old-style font | `internal/site/static/bt_oldstyle.ttf` | "Battletech" font, version 1.00 (2004); its author is not named in the file | not covered by the GPL; its author's terms |
| Era icons | `internal/site/static/era-*.webp` (Clan Invasion) | official BattleTech era artwork (The Topps Company / Catalyst Game Labs), supplied by the campaign author | not covered by the GPL; used as fan, non-commercial material under its owners' terms |
| Campaign emblems | `campaigns/*/images/icon.webp` (Refusal War: the Clan Wolf ilClan emblem) | official BattleTech artwork (The Topps Company / Catalyst Game Labs), supplied by the campaign author | not covered by the GPL; used as fan, non-commercial material under its owners' terms |
| Campaign content | `campaigns/`, `Pages/` | the campaign author | not licensed under the GPL; all rights reserved by the author |

The non-commercial terms of CC BY-NC-SA 4.0 and of Microsoft's Game Content Usage Rules mean the site, as shipped with
this material, must not be used commercially.

## MekBay's web app (the proxy)

The site's MekBay proxy (`internal/mekbayproxy/proxy.go`) serves MekBay's web app to its visitors, fetched from
mekbay.com and modified in one way: the WebSocket address in its JavaScript (`` `wss://mekbay.com/ws` ``) is replaced
with the proxy's own `/ws`, and its service workers are replaced with stubs that unregister themselves. MekBay is
Copyright (C) The MegaMek Team and licensed under the GNU General Public License v3.0; its source code is at
<https://github.com/MegaMek/mekbay>, and the modification is the code in `internal/mekbayproxy/proxy.go`.
