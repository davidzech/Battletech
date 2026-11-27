# CLAUDE.md

Guidance for Claude Code in this repository. Keep it compact: describe conventions, rules and where things live, not every UI detail or test name (the code and git history hold those).

## What this repo is

Custom tabletop BattleTech campaigns, one folder per campaign under `campaigns/`, mostly Markdown, plus a Go web server that turns them into a website with per-playthrough tracking. The only campaign so far is `RefusalWar/` ("Refusal War", inspired by MechWarrior 2: Clan Wolf vs Clan Jade Falcon). Expect more campaigns: keep campaign-specific rules in the campaign's folder; only genuinely shared things (like `data/units.json`) go at the top level. A new campaign or scenario that follows the conventions below needs no code changes.

## Running and checks

- Run from the repo root: `go run .` (flags `-addr` (defaults to `localhost:8080` under `go run`), `-content campaigns`, `-db playthroughs.db`, `-postgres <url>` (defaults to `$DATABASE_URL`; used instead of `-db` when set)).
- Live reload: Air (`go install github.com/air-verse/air@v1.67.4`, then `air`; `.air.toml` passes `-dev-reload`, which makes pages reload via an EventSource on `/dev/reload`). Don't enable Air's browser-reload proxy (it breaks the same-origin check, `mekbay-<id>` hosts and the WebSocket relay).
- Checks (all run in `.github/workflows/ci.yml`, plus `docker build`): `gofmt -l .` (prints nothing), `go build ./...`, `go vet ./...`, `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...` (`staticcheck.conf` disables ST1005), `go test ./...`.
- Postgres tests run only with `TEST_DATABASE_URL`, e.g. `docker run -d -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=bt -p 55432:5432 postgres:17-alpine` then `TEST_DATABASE_URL=postgres://postgres:pw@localhost:55432/bt?sslmode=disable go test ./...`.
- Live MekBay tests run only with env vars set (`MEKBAY_LIVE_BATTLE=1`, `MEKBAY_LIVE_PROXY=1`).
- **Browser tests**: a Playwright suite in `browsertests/` (its own `package.json` and lockfile, versions pinned) drives the key flows (start and deploy wizards, outcome dialog, hangar dialogs, rollback). `TestBrowser` (`internal/site/browser_test.go`) serves the site with the fake MekBay plus test-only `/__test/hangar` (a run's hangar with `Campaign`, else the link force) and `/__test/battle` endpoints and runs it; opt-in: `cd browsertests && npm ci && npx playwright install chromium` once, then `BROWSER_TESTS=1 go test ./internal/site -run TestBrowser` (`BROWSER_TESTS_ARGS` passes e.g. a spec name or `--headed`). CI runs it in its own job, alongside the Go one. `TestScriptsParse` syntax-checks every script. When changing a browser script, extend the suite (or check by hand in a headless browser for visual changes).
- Docker: multi-stage, `CGO_ENABLED=0`, distroless nonroot, bundles `campaigns/`, SQLite in `/data` unless `DATABASE_URL` is set, listens on 8080. `data/units.json` is not in the image. On DigitalOcean App Platform the disk is ephemeral, so use Postgres there. Footer commit hash: Go build stamp, else `COMMIT_HASH` env (`${_self.COMMIT_HASH}` on App Platform), else `dev` (`internal/site/commit.go`).

## License

Code is **GPL-3.0-or-later** (`LICENSE`). Everything with its own terms (CC BY-NC-SA 4.0 unit data and icons from MekBay/MegaMek, MekBay's GPL weapon icons, MechWarrior 5 portraits, the MIT QR library, the font, and the campaign content in `campaigns/` and `Pages/`) is listed in `NOTICE.md`. **When vendoring anything new, add it to `NOTICE.md`.** The NC terms mean the site must stay non-commercial.

## Code layout

Packages under `internal/`, dependencies only pointing down this list:

- `site`: HTTP server (`NewHandler`), pages, JSON endpoints, templates (`templates/*.html`) and static files (`static/`), plus glue: seeds/cookies, starting a campaign, deploying, battles, checkpoints, hangar import.
- `mekbayproxy`: the MekBay proxy (web app, `/ws` relay, host parsing, `CheckMekBay`).
- `storage`: the database (`Store`, `OpenSQLite`/`OpenPostgres`): schema, migrations, runs, pilots, battles, outcomes, history, checkpoints, hangars, links.
- `content`: campaign folders (`Library`, `Campaign`, `Scenario`): front matter, names, `.mul` parsing, the outcome engine and expressions, keywords, aftermath.
- `hangar`: prices and repair rules (repair, rearm, sell, swap, purchase).
- `mekbay`: MekBay WebSocket client (`mekbay.MekBay` interface, faked in tests), pure edits of force JSON, unit condition, link parsing, seed-derived account.
- `pilot`: named pilots: SP ladders, levels, Edge abilities, ranks, portraits, name validation.
- `catalog`: embedded unit catalog (`units_index.json`), names, icons, search, availability, unit cards, MekBay links; generators via `go generate ./internal/catalog`.
- `format`: number formatting.

Conventions:
- Tests live with their package; a test file is named after the source file it tests (suffix like `pilots_recruit_test.go` when needed). Shared site fakes/helpers are in `internal/site/testfakes_test.go`.
- Every endpoint that changes a campaign starts with `runRequest` (same origin, seed, named account, started campaign) or `pilotRequest` (the same checks, for handlers that do not need the run itself); hangar changes go through `changeHangar` (`hangaredit.go`): check and edit run on the stored hangar in `Store.EditHangarGuarded`'s transaction with the warchest charge (run row lock: safe across servers; a battle claimed or recorded since the battle lock check refuses it); a refusal changes nothing. Endpoints are same-origin only.
- A run's hangar is MekBay's force JSON in `campaign_runs.hangar` (so checkpoints hold it); only battles live in MekBay. The old `hangar_instance`/`source_url`/`hangar_copy` columns are read only by `site.ImportHangars` (run by `main` before serving), which clears each once imported or skipped (only MekBay transport errors stop startup). A run left without a hangar answers `noHangarMessage` (409) wherever its hangar is needed.
- Every endpoint that changes a campaign logs a history line once the change is made (`Server.logHistory`, `site/history.go`).
- Pages render into a buffer first, so a template error is a clean 500.
- Schema changes go through `Store.init` (`CREATE TABLE IF NOT EXISTS`, one statement at a time, after `migrateLegacy`); add columns with `Store.ensureColumn`. Write SQL once with `?` placeholders (rebound for Postgres by `Store.q`) and keep to what both SQLite and Postgres accept. Add a migration test with any migration.
- Browser scripts share `static/dom.js` (`Dom.el`, `Dom.commas`, `Dom.statBox`, `Dom.players` (the player-count picker), `Dom.costLine` ("Cost X SP. Warchest A → B SP."), the arrow and refresh icons), loaded by the layout first. Load order matters for shared components: `mechrow.js` → `unitcard.js` → `forcesummary.js` → page script; `abilitycard.js` before `pilots.js`/`mekbay.js`; `variantpicker.js` → `swapdialog.js` → `hangarlist.js`/`mekbay.js`; `confirm.js` before its users.
- Insert names, roles and user text as text, never HTML.

## Campaign folder structure

Each campaign folder (e.g. `campaigns/RefusalWar/`) has:

- `Intro.md`: body is the campaign overview, shown as an "incoming transmission" briefing block (leading `## ` heading becomes the tag; first paragraph is the lede; `>` blockquote is a pull quote). Folded with Show more once the campaign is started. Front matter (`introMeta`) holds campaign settings: `min_players`/`max_players` (Refusal War 1–2, at most 4), `starting_named_pilots` (2), `starting_pilot_sp` (150), `starting_bv` (10000), `starting_min_mechs` (5), `starting_warchest` (5000), `max_named_pilots` (4), `recruit_cost` (300), `recruit_rank` (MechWarrior), `mission_term` (default `Scenario`), `mvp_sp` (20; 0 = no MVP), `faction` (`clan`), optional `ranks:` (Refusal War adds **Cadet** below MechWarrior, so starting pilots are cadets), `availability_faction`/`availability_era` (`Clan Wolf`, `Clan Invasion`; filter Purchase/Reconfigure and give the campaign's era label), `icon` and `image` (files in the campaign's `images/`; emblem and 3:1 banner).
- `Names.md` (optional): front matter `force_names`, `male_pilot_names`, `female_pilot_names`, `neutral_pilot_names`, replacing the baseline lists in `static/pilotnames.js` as placeholders.
- `Keywords.md`: front matter `keywords:` list of `{id, name, scenario, description}` (`id` lowercase snake_case, never shown to players; `scenario` is the **one** scenario that grants it). Body is author notes, not shown.
- `Scenario_NNN/`: zero-padded, sequential, with `Setup.md`, `Outcome.md`, `.mul` force files and `images/`.
- Refusal War's paths: every run goes 000 → 001 → (002 or 003) → … → 012 Golden Spade → 013 Epilogue, 6 to 8 sorties. 005 is reached only from 002 (Sudeten), 006 only from 003 (Zoetermeer); every Act III mission offers 007 or 008; 007 goes to 012 directly or via 010 or 011; 008 goes to 009 or 011. `TestScenarioBranches` walks every path.

**Campaign files are checked.** Front matter is decoded strictly (`content.readMarkdown` with `KnownFields`), so unknown keys are errors. Every problem (bad front matter, outcome rules that don't compile, unregistered keywords, branches to missing scenarios, unknown unit links, bad names) goes into `Campaign.Problems`, shown in a red box on the campaign page; `TestCampaignFilesAreValid` keeps real campaigns clean.

## Scenario file conventions

Follow `Scenario_000` and `Scenario_001`. The folder name lowercased (`scenario_000`) is the scenario ID; all cross-references use IDs, never titles.

- **`Setup.md` front matter**: `id`, `title`, `planet` (every scenario has one), `map`, `forces` (`opfor1`, `opfor2`… → `.mul` files), optional `allies` (`.mul` files of uncontrolled friendly units, built free into their own force), `provided_units` (MekBay unit IDs replacing the hangar for that scenario, a repeated ID giving another copy; ephemeral, never written to MekBay), `provided_swaps` (free omnimech reconfiguration of provided units; needs `provided_units`), `bv_limit`, `unit_limit`, `pilots_required`, `commander` (default true), `readout` (`Label: value` lines over the first image) and `readout_title` (default 'Planetary survey'), `images` (paths in `images/`).
- **Setup.md body**: `# Scenario NNN: <Title>`, `## Briefing`, `## Overview`, `## Map`, `## Forces` (`### Player Forces`, `### OpFor N`, each with a `**Deployment:**` line), `## Special Rules`, and optionally `## Objectives` last (`### Objective N` subsections; shown redacted with Reveal buttons; text before the first `###` is dropped; placement/reveal rules go in Map and Special Rules).
- **Briefing fluff**: an in-world commander addressing the player in second person, formal and terse, MechWarrior 2 style. Less is more: show, don't tell, slightly alien yet familiar (Miyazaki-style lore, Dune), a few lines, no restating rules. Quotes are `>` blockquotes (don't type quotation marks) ending with `> — Name, Title` or `> -- Name, Title` (not `> - `). If lore isn't written yet, leave the placeholder rather than inventing it.
- **OpFor lines**: `Variant (gunnery/piloting) Role: Pilot, hex XXYY`, e.g. `Nova Prime (3/4) Skirmisher: Star Commander Alrek, hex 2113`. Role is the unit's MUL role from `role` in `data/units.json` (Scout, Striker, Skirmisher, Brawler, Missile Boat, Sniper, Juggernaut, Ambusher). Omit the hex for reserves, and `: Pilot, hex XXYY` when neither is set. Link the mech name to `https://mekbay.com/?gs=cbt&shareUnit=<ID>&tab=General`; the ID is the `name` field in `data/units.json` and isn't always obvious (Mist Lynx is `BMKoshi_E`, Kit Fox `BMUller_J`, but `BMMadCat_H`, `BMLoki_Prime`, `BMGrizzly`). **Ask the user for the working URL rather than guessing.** Links to catalog units get hover unit cards automatically; unknown IDs are campaign problems.
- **Hexes** are `XXYY` (column, row). Play uses standard two-mapsheet battlemats. Leave unknown deployment as `TBD`; don't invent coordinates or a schema.
- **Images** are 3:1 banners (another shape is cropped to the top, so keep the subject high).
- **`Outcome.md` front matter**: `id`, `default_next`, `branches` (list of scenario IDs; omitted → `default_next` or next number; empty list ends the path), `mvp_sp` override, outcome engine fields (`pilot_sp`, `objectives`, `inputs`, `outcomes`), `aftermath: [{when, text}]`. Body: `# Scenario NNN: <Title> - Outcome`, `## Victory & Rewards`, optional `## Aftermath` (third-person war news) and `## Next Steps` (what players do next and the branch choice); both are shown in an Aftermath block after completion. Rewards are in SP.
- `grants`/required keywords are IDs registered in `Keywords.md`; add them there first. A scenario may grant only keywords registered as its own.
- Aftermath/Next Steps texts come from `Pages/Sortie NNN/` docs. Pages numbering is out of date: Sorties 001–007 = Scenarios 001–007, Sortie 008 = 009 (Aquiline Fire), 009 = 011 (Velvet Hammer), 010 = 012 (Golden Spade), 011 = 013 (Epilogue); 008 Silver Staff and 010 Cold Crescent have no Pages doc. A `.pages` file is a folder whose `Index.zip` holds snappy-compressed protobuf `.iwa` files; text can be extracted from `Index/Document.iwa` without the Pages app.
- Scenario 000 is the "Trial of Position" graduation mission (back-to-back 1v1s, provided lighter Clan omnimechs, 2 mechs, pilots required). Its kills-to-SP table (1–4 kills: 150/400/650/900) is specific to it; a pilot with 0 kills or killed is removed and earns a free recruit. Don't assume later scenarios use it.

## Force files (.mul)

MegaMek's XML force format, one `<entity>` per unit with `<pilot>` skills. Prefer a real MekBay export. When hand-writing, `chassis`/`model` must match MegaMek's database (exactly `chassis`/`model` in `data/units.json`); two-named mechs use `chassis="<Inner Sphere name> (<Clan name>)"` (e.g. `Koshi (Mist Lynx)`); single-name mechs like the Grizzly have an empty `model`. Deployment attributes, `externalId` and `quirks` can be omitted.

## Game rules and domain decisions

- **Ruleset**: custom Classic BattleTech in the style of BattleTech: Aces, partly homebrewed. Named Pilots follow *Hot Spots: Draconis Reach* pp. 34–35 (the PDF is at `C:\Users\david\iCloudDrive\Documents\Battletech\Books\`, too big for Read; use `pdftotext -f N -l M`; the PDF page is 2 higher than the printed number).
- **BV is always base BV** (a 4/5 pilot's); pilot skill never changes BV for limits or costs. `catalog.AdjustedBV` is only for MekBay metadata and card display.
- **BV is SP**: buying costs base BV; selling pays half base BV, or a quarter if not pristine (`hangar.SellValue`); reconfiguring an omnimech costs half tonnage (rounded up) plus half the BV gained (no refund for going down) and needs a pristine unit; repair/rearm follow the SP Activity Cost Table (`hangar`, `service.go`). A truly destroyed mech (center torso structure gone) can only be sold.
- **Named pilots** track total SP earned (`sp_total`) and SP allocated in four columns (Gunnery, Piloting, Edge tokens, Edge abilities); unallocated = total − sum, never stored. Levels come from the book's improvement table as ladders (`internal/pilot/pilot.go`). **Handicap is deliberately not tracked; don't add it back.** The book's 100-SP-per-track cap is deliberately not enforced. New pilot: 4/5, 1 Edge token. At most `max_named_pilots` living pilots; Killed in Action pilots keep their row (code says `fallen`), move to a Memorial, free their slot and portrait.
- Edge abilities: the 12 from *Hot Spots: Draconis Reach* p. 166 (`pilot.AbilityCatalog`); the site records choices, doesn't play them. Pending = Edge abilities level − learned. Choices are permanent; resetting a pilot's SP allocation also forgets their abilities.
- Ranks are cosmetic, per faction (`internal/pilot/ranks.go`) or the campaign's `ranks:`; changed by outcome `promote` rules or by hand in Advanced Management.
- Keywords are tracked by the campaign as a whole (like BattleTech: Aces), not per pilot. **Keywords that gate scenarios are not built yet; don't build them unprompted.**
- Clan honor rules (zellbrigen) apply. Keep mech names and variants to canon Clan designs and terminology (touman, zellbrigen, Trial).
- For mech data, the authorities are the Master Unit List (behind a Cloudflare challenge, 403 to automated fetches) and MekBay, not Sarna.

## Site features (where to look)

- **Pages and routes**: `/` (campaign cards), `/<campaign>/` (intro, scenario list with path graph, or the started campaign's force overview, keywords and History), `/<campaign>/<scenario>/` (briefing, banner, Setup, collapsed Objectives and Outcome folds, Deploy section, Mission log, Aftermath, Next missions), `/<campaign>/manage/` (Overview/warchest, named pilots, hangar, Danger zone with Advanced Management). Dark mode only, industrial-yellow BattleTech look; titles use `static/bt_oldstyle.ttf`. Fold animation is a Web Animations script in `layout.html` (don't switch to CSS `::details-content`; it fails in Firefox).
- **Seeds**: the only credential, 8–32 lowercase letters/digits, in an HttpOnly cookie; hashed into the database key (`seed.go`), never stored. A seed is an account with an account name; each campaign run has its own force name. In code a tracked run is a "playthrough".
- **Starting a campaign**: a wizard (`campaign.js`): force name, player count (if a range), one step per starting pilot (name, portrait, SP allocation via shared `static/allocator.js`, Edge abilities if earned), then the hangar: by default picking mechs on the site (`buildforce.go` builds the force JSON), or a MekBay hangar link (read once, stored clean). `checkStartingHangar` (`startcheck.go`) enforces BV cap and mech minimum. `POST /api/campaign/start`; a second start is 409.
- **MekBay accounts**: never ask a player for a MekBay ID. One account per seed, derived by `MekBayUID(seed, SlotHangar)` (salt `p1` kept for compatibility), never stored or logged. It owns OpFors, battle forces and operations.
- **Deploy wizard** (`static/mekbay.js`): players divide one shared pool of mechs and pilots, one step per player, assign named pilots by drag or click, an optional commander step, then always a review. Going over the BV limit gates only Start battle: the review's Reconfigure buttons (the shared dialog) propose omnimech variants, free for provided mechs, paid at Manage prices for the hangar's (the drop cost). **Start battle** (`battlestart.go`) first pays for and makes those swaps in the hangar in one transaction (`reconfigureForBattle`; they stay if the battle then fails), then builds OpFor, allies, one force per player (copied from the stored hangar, keeping unit IDs) and one MekBay operation (not a lobby; lobbies were tried and dropped). Named pilots are written only into battle forces; **the hangar never holds a named pilot**. A battle in progress locks the campaign (`battleLock`) until completed or aborted; starts are serialized with `Store.ClaimBattle`.
- **Outcome engine** (`internal/content/outcome.go`, `expr.go`, `storage/outcomes.go`, `outcomehttp.go`): most scenarios use the Aces model: objectives fill a pool, `pilot_sp` to each active pilot (half if benched, none if killed or removed by a `kia` rule, as if never deployed) drawn from it, remainder to the warchest, objectives grant keywords. Custom scenarios use `inputs` and `outcomes` rules (`when`, `sp`, `pool`, `warchest`, `kia`, `free_recruits`, `grants`, `promote`) with expr-lang expressions type-checked at load. Every mission has an MVP (extra `mvp_sp`, not drawn from the pool). Committing reads the battle's units from MekBay, then applies everything, battle damage into the hangar too, in one transaction (`mekbay.CommitState`, matched by unit ID; crew reset to an unnamed 4/5; pending damage consolidated and battle leftovers dropped as MekBay's phase end does, `mekbay/unitstate.go`). Blown-off/flooded locations count as destroyed.
- **Branching**: available scenarios = completed ones plus the branches of the last completed one; missions are played in increasing order. Graph drawing in `graph.go`.
- **Checkpoints and rollback** (`checkpoint.go`): taken at campaign start and after each mission completion (row snapshot, hangar included); History lines with a checkpoint get Roll back; Abort mission rolls back to the latest. A checkpoint whose snapshot has no hangar can't be rolled back to. Lines a checkpoint should keep are logged **before** the checkpoint is recorded; a rollback deletes later history lines.
- **Hangar on pages** (`hangarjson.go`): pages embed it (`data-hangar`); `/api/campaign/force` and `/api/mekbay/hangar` answer the same.
- **MekBay proxy** (`internal/mekbayproxy/proxy.go`, `storage/links.go`): serves MekBay's own web app from `mekbay-<link id>.<host>` and relays `/ws`, swapping the top-level `uuid` for the link's account, so a link acts as the seed's account. The link id is the credential (160-bit, sliding 14-day expiry); responses carry `Referrer-Policy: no-referrer`. Only the bare `mekbay.localhost` from loopback is a shared sandbox. Production needs the wildcard domain `*.battletech.zech.io`.
- **Shared UI components** (`static/`): `mechrow.js` (`MechRow`: the one mech row, also `attachTip` floating cards as popovers inside open dialogs, `attachCard` for a row's unit card, `withoutPilot`), `unitcard.js` (the one unit card), `pilotcard.js`, `abilitycard.js`, `unitsearch.js`, `variantpicker.js`, `swapdialog.js` (`SwapDialog`: the Reconfigure dialog, for Manage and the Deploy review), `allocator.js`, `portraitpicker.js`, `confirm.js`, `wizardkeys.js` (Enter/Backspace for `data-wizard-next`/`data-wizard-back`). Reuse them; every list of mechs must use `MechRow`, every unit card `UnitCard`.

## UI rules

- **Never use `window.confirm`**; use `Confirm.ask` (`static/confirm.js`), with `danger` for destructive actions and `action` to keep the dialog open while the change saves.
- Button labels never end in an ellipsis (`TestButtonsHaveNoTrailingEllipsis`).
- **Always show the Clan name** of a two-named design (`Vulture (Mad Dog)` → "Mad Dog", model separate); use `catalog.Names`/`catalog.DisplayName`.
- **Icons are vendored, never hotlinked** (`internal/catalog/icons/`, MegaMek mm-data, CC BY-NC-SA 4.0). To refresh: sparse-clone mm-data's `data/images/units/{meks,vehicles,battle armor,defaults}`, run `go run gen_icons.go gen_common.go -src <mm-data>/data/images/units` from `internal/catalog`, then `go generate ./internal/catalog`.
- Players never see keyword IDs, MekBay IDs or account IDs; history lines use names, never IDs.

## Data

- `data/units.json` is MekBay's unit catalog (`https://db.mekbay.com/units.json`, with `factions.json` and `eras.json`, downloaded 2026-11-16, ~27 MB). Refresh with `curl --compressed -o data/units.json https://db.mekbay.com/units.json`; search it with grep/Python, don't read it whole. Then run `go generate ./internal/catalog` to rebuild the embedded `internal/catalog/units_index.json` and `availability.json` (also needed when a campaign names a new faction or era).
- MekBay's front-end source is at `../mekbay`; its server is closed source and reached only over `wss://mekbay.com/ws`, so the integration is unofficial and may break. Anyone can read a force by ID; writes need the owner's UUID. MekBay stores no "crippled" flag; `internal/mekbay/condition.go` recomputes it like MekBay's client. Damage in a unit's `state.locations[loc].armor`/`.internal` is points *lost*.
- Portraits: 79 MechWarrior 5 busts in `internal/pilot/portraits/` (refresh with `go run gen_portraits.go -src <folder>` there).

## Long-term direction

Not built yet, don't build unprompted: scenarios gated by required keywords, outcomes that choose a branch automatically, answering unknown subdomains with a 404. When adding or reformatting data keep it both machine-parsable and human-readable: consistent headings, plain Markdown/YAML/XML/JSON, structured fields and stable IDs rather than free-text references.
