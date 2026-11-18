package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // pure-Go PostgreSQL driver
	_ "modernc.org/sqlite"             // pure-Go SQLite driver
)

// Store is the database holding playthrough state: a local SQLite file, or PostgreSQL when the site is hosted
// somewhere without a persistent disk. Queries are written with ? placeholders and rebound for PostgreSQL.
type Store struct {
	db       *sql.DB
	postgres bool
}

// Playthrough is one company's tracked run through the campaigns, identified by their seed.
type Playthrough struct {
	ID          string // hash of the seed, never the seed itself
	AccountName string // the company's name in the lore, e.g. "Wolf's Dragoons" ("" until named)
	// PlayerIDs says which players (1 to 4) have a saved MekBay user ID. The IDs themselves are secret and never leave the store (see accounts.go).
	PlayerIDs [maxPlayers]bool
}

// Configured reports whether the company has been named, which is what setting a seed up amounts to.
func (p Playthrough) Configured() bool { return p.AccountName != "" }

// CampaignRun is a playthrough's run through one campaign. It exists once the campaign has been started, and has
// its own MekBay hangar.
type CampaignRun struct {
	Campaign       string // the campaign's slug, e.g. "refusalwar"
	HangarURL      string // the link to the site's own copy of the hangar, https://mekbay.com/?instance=<id>; never shown to the player
	HangarInstance string // that copy's instance ID: the hangar the site reads and manages (see clone.go)
	SourceURL      string // the MekBay link the player gave, which the copy was made from; kept so it can be imported again
	Warchest       int    // the force's finances in SP; starts at the campaign's starting_warchest
	ForceName      string // the campaign's force name, e.g. "Wolf's Dragoons", chosen when the campaign is started and editable on the Manage page ("" on a run from before it was asked)
	FreeRecruits   int    // recruits that cost no SP (a mission outcome can grant them, e.g. a replacement for a pilot who died)
	Players        int    // how many players the company plays this campaign with: the Deploy wizard's default count (0 on a run from before it was asked)
}

// ForceLabel is the force's name, or a plain "Your force" for a run from before forces were named.
func (r CampaignRun) ForceLabel() string {
	if r.ForceName == "" {
		return "Your force"
	}
	return r.ForceName
}

// WarchestText is the warchest with a thousands separator, e.g. "4,500".
func (r CampaignRun) WarchestText() string { return withCommas(r.Warchest) }

// Started reports whether the campaign has been started (a zero CampaignRun means it hasn't).
func (r CampaignRun) Started() bool { return r.HangarInstance != "" }

// Owned reports whether the run's hangar is the site's own copy. A run started before the site made copies points at the player's own force, which the site
// cannot change, so it has to be imported again before anything can be repaired, rearmed, bought or assigned.
func (r CampaignRun) Owned() bool { return r.SourceURL != "" }

// timestampColumn is the created_at / started_at column type, which differs between the two databases.
func (s *Store) timestampColumn() string {
	if s.postgres {
		return "TIMESTAMPTZ NOT NULL DEFAULT now()"
	}
	return "TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP"
}

func (s *Store) playthroughsDDL(table string, ifNotExists bool) string {
	exists := ""
	if ifNotExists {
		exists = "IF NOT EXISTS "
	}
	return fmt.Sprintf(`CREATE TABLE %s%s (
	id           TEXT PRIMARY KEY,
	created_at   %s,
	company_name TEXT NOT NULL DEFAULT '',
	owner_uid    TEXT NOT NULL DEFAULT ''
)`, exists, table, s.timestampColumn())
}

// schemaStatements are run one at a time (not as one multi-statement string) so both drivers accept them.
func (s *Store) schemaStatements() []string {
	return []string{
		s.playthroughsDDL("playthroughs", true),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS campaign_runs (
	playthrough_id  TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign        TEXT NOT NULL,
	started_at      %s,
	hangar_url      TEXT NOT NULL DEFAULT '',
	hangar_instance TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (playthrough_id, campaign)
)`, s.timestampColumn()),
		`CREATE TABLE IF NOT EXISTS player_accounts (
	playthrough_id TEXT NOT NULL,
	slot           TEXT NOT NULL,
	uid            TEXT NOT NULL,
	PRIMARY KEY (playthrough_id, slot)
)`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS scenario_completions (
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign       TEXT NOT NULL,
	scenario_id    TEXT NOT NULL,
	completed_at   %s,
	outcome        TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (playthrough_id, campaign, scenario_id)
)`, s.timestampColumn()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS campaign_keywords (
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign       TEXT NOT NULL,
	keyword        TEXT NOT NULL,
	scenario_id    TEXT NOT NULL,
	granted_at     %s,
	PRIMARY KEY (playthrough_id, campaign, keyword)
)`, s.timestampColumn()),
		`CREATE TABLE IF NOT EXISTS hangar_cache (
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign       TEXT NOT NULL,
	instance       TEXT NOT NULL,
	data           TEXT NOT NULL,
	fetched_at     BIGINT NOT NULL,
	PRIMARY KEY (playthrough_id, campaign)
)`,
		`CREATE TABLE IF NOT EXISTS mekbay_operations (
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign       TEXT NOT NULL,
	scenario_id    TEXT NOT NULL,
	opfor_instance TEXT NOT NULL,
	operation_id   TEXT NOT NULL,
	PRIMARY KEY (playthrough_id, campaign, scenario_id)
)`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS named_pilots (
	id               TEXT PRIMARY KEY,
	playthrough_id   TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign         TEXT NOT NULL,
	name             TEXT NOT NULL,
	portrait         TEXT NOT NULL DEFAULT '',
	rank             TEXT NOT NULL DEFAULT '',
	created_at       %s,
	fallen           INTEGER NOT NULL DEFAULT 0,
	sp_gunnery       INTEGER NOT NULL DEFAULT 0 CHECK (sp_gunnery >= 0),
	sp_piloting      INTEGER NOT NULL DEFAULT 0 CHECK (sp_piloting >= 0),
	sp_edge_tokens   INTEGER NOT NULL DEFAULT 0 CHECK (sp_edge_tokens >= 0),
	sp_edge_abilities INTEGER NOT NULL DEFAULT 0 CHECK (sp_edge_abilities >= 0),
	sp_total         INTEGER NOT NULL DEFAULT 0 CHECK (sp_total >= 0)
)`, s.timestampColumn()),
		`CREATE TABLE IF NOT EXISTS pilot_abilities (
	pilot_id TEXT NOT NULL REFERENCES named_pilots(id) ON DELETE CASCADE,
	ability  TEXT NOT NULL,
	seq      INTEGER NOT NULL,
	PRIMARY KEY (pilot_id, ability)
)`,
		`CREATE INDEX IF NOT EXISTS named_pilots_by_run ON named_pilots (playthrough_id, campaign)`,
	}
}

// columnExists reports whether table has the named column (false if the table doesn't exist).
func (s *Store) columnExists(table, column string) (bool, error) {
	var n int
	var err error
	if s.postgres {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`, table, column).Scan(&n)
	} else {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	}
	return n > 0, err
}

// migrateLegacy upgrades a database from before companies and per-campaign hangars, when each playthrough held a
// single hangar link and player count. That hangar data, and the operations that hung off it, are dropped (players
// re-enter their hangar when they start a campaign); the seed, its creation time and any MekBay owner UID are kept.
func (s *Store) migrateLegacy() error {
	legacy, err := s.columnExists("playthroughs", "hangar_instance")
	if err != nil || !legacy {
		return err
	}
	owner := "''"
	if has, err := s.columnExists("playthroughs", "owner_uid"); err != nil {
		return err
	} else if has {
		owner = "owner_uid"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS mekbay_operations`,
		s.playthroughsDDL("playthroughs_new", false),
		`INSERT INTO playthroughs_new (id, created_at, owner_uid) SELECT id, created_at, ` + owner + ` FROM playthroughs`,
		`DROP TABLE playthroughs`,
		`ALTER TABLE playthroughs_new RENAME TO playthroughs`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// init migrates an older database if needed, then creates whatever is missing.
func (s *Store) init() error {
	if err := s.migrateLegacy(); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	for _, stmt := range s.schemaStatements() {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}
	// Columns added after a table first shipped. CREATE TABLE IF NOT EXISTS leaves an existing table as it was, so they
	// are added here (neither database has ADD COLUMN IF NOT EXISTS in common).
	if err := s.ensureColumn("campaign_runs", "warchest", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("campaign_runs", "source_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("campaign_runs", "players", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("campaign_runs", "force_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("campaign_runs", "free_recruits", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("scenario_completions", "outcome", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("named_pilots", "portrait", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := s.ensureColumn("named_pilots", "rank", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := s.ensureColumn("named_pilots", "sp_total", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	// A pilot's total can never be below what they have allocated. Pilots from before the total was tracked had every SP
	// allocated, so this sets their total to that sum (and does nothing to any pilot that is already consistent).
	if _, err := s.db.Exec(`UPDATE named_pilots SET sp_total = sp_gunnery + sp_piloting + sp_edge_tokens + sp_edge_abilities
		WHERE sp_total < sp_gunnery + sp_piloting + sp_edge_tokens + sp_edge_abilities`); err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}

// ensureColumn adds a column to a table that doesn't have it yet.
func (s *Store) ensureColumn(table, column, definition string) error {
	has, err := s.columnExists(table, column)
	if err != nil || has {
		return err
	}
	_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
	return err
}

// OpenSQLite opens (creating if needed) the SQLite database at path and applies the schema.
func OpenSQLite(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Personal-use scale: a single connection avoids "database is locked" surprises.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// OpenPostgres connects to the PostgreSQL database at url (postgres://user:pass@host:port/db?sslmode=require)
// and applies the schema.
func OpenPostgres(url string) (*Store, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	s := &Store{db: db, postgres: true}
	if err := s.init(); err != nil {
		diag := postgresDiagnostics(db)
		db.Close()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}
	return s, nil
}

// postgresDiagnostics describes who we are connected as and what that role may do, to explain a failed schema
// setup (managed Postgres 15+ only lets the database owner create tables in the public schema).
func postgresDiagnostics(db *sql.DB) string {
	var user, database, owner, schemas string
	var inPublic, inDatabase bool
	err := db.QueryRow(`
		SELECT current_user, current_database(),
		       (SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = current_database()),
		       has_schema_privilege('public', 'CREATE'),
		       has_database_privilege(current_database(), 'CREATE'),
		       (SELECT string_agg(nspname || ' owned by ' || pg_get_userbyid(nspowner), ', ') FROM pg_namespace
		         WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema')`,
	).Scan(&user, &database, &owner, &inPublic, &inDatabase, &schemas)
	if err != nil {
		return "diagnostics unavailable: " + err.Error()
	}
	return fmt.Sprintf("connected as %q to database %q owned by %q; create in public schema: %v; create schemas: %v; schemas: %s",
		user, database, owner, inPublic, inDatabase, schemas)
}

// q rebinds ? placeholders to $1, $2, ... for PostgreSQL.
func (s *Store) q(query string) string {
	if !s.postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// EnsurePlaythrough records a playthrough ID if it isn't already known and reports whether it was new.
func (s *Store) EnsurePlaythrough(ctx context.Context, id string) (created bool, err error) {
	res, err := s.db.ExecContext(ctx, s.q(`INSERT INTO playthroughs (id) VALUES (?) ON CONFLICT (id) DO NOTHING`), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Playthrough loads a playthrough; ok is false if the ID is unknown.
func (s *Store) Playthrough(ctx context.Context, id string) (p Playthrough, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT id, company_name FROM playthroughs WHERE id = ?`), id,
	).Scan(&p.ID, &p.AccountName)
	if errors.Is(err, sql.ErrNoRows) {
		return Playthrough{}, false, nil
	}
	if err != nil {
		return p, false, err
	}
	if err := s.loadPlayerIDs(ctx, &p); err != nil {
		return p, false, err
	}
	return p, true, nil
}

func (s *Store) updatePlaythrough(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, s.q(query), args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetAccountName stores the company's name.
func (s *Store) SetAccountName(ctx context.Context, id, name string) error {
	return s.updatePlaythrough(ctx, `UPDATE playthroughs SET company_name = ? WHERE id = ?`, name, id)
}

// SetWarchest overrides the force's funds (SP), to correct a mistake or a mismatch. ErrNoRows means the campaign isn't started.
func (s *Store) SetWarchest(ctx context.Context, playthroughID, campaign string, funds int) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET warchest = ? WHERE playthrough_id = ? AND campaign = ?`), funds, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrInsufficientFunds is returned when the warchest can't cover a charge.
var ErrInsufficientFunds = errors.New("not enough funds in the warchest")

// SpendWarchest takes funds out of the warchest in one statement, so two charges can't overspend it.
func (s *Store) SpendWarchest(ctx context.Context, playthroughID, campaign string, amount int) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET warchest = warchest - ? WHERE playthrough_id = ? AND campaign = ? AND warchest >= ?`), amount, playthroughID, campaign, amount)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrInsufficientFunds
	}
	return nil
}

// RefundWarchest puts funds back, when a charge's work failed.
func (s *Store) RefundWarchest(ctx context.Context, playthroughID, campaign string, amount int) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET warchest = warchest + ? WHERE playthrough_id = ? AND campaign = ?`), amount, playthroughID, campaign)
	return err
}

// Run loads a playthrough's run through a campaign; ok is false if the campaign hasn't been started.
func (s *Store) Run(ctx context.Context, playthroughID, campaign string) (r CampaignRun, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT campaign, hangar_url, hangar_instance, source_url, warchest, players, force_name, free_recruits FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`),
		playthroughID, campaign,
	).Scan(&r.Campaign, &r.HangarURL, &r.HangarInstance, &r.SourceURL, &r.Warchest, &r.Players, &r.ForceName, &r.FreeRecruits)
	if errors.Is(err, sql.ErrNoRows) {
		return CampaignRun{}, false, nil
	}
	return r, err == nil, err
}

// StartRun starts a campaign with its MekBay hangar. Doing it again for a started campaign changes the hangar and
// keeps when it was started.
func (s *Store) StartRun(ctx context.Context, playthroughID, campaign string, h Hangar) error {
	_, err := s.db.ExecContext(ctx,
		s.q(`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance, source_url) VALUES (?, ?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign) DO UPDATE SET hangar_url = excluded.hangar_url, hangar_instance = excluded.hangar_instance, source_url = excluded.source_url`),
		playthroughID, campaign, h.URL, h.Instance, h.Source)
	return err
}

// SetForceName renames a started campaign's force. sql.ErrNoRows if the campaign hasn't been started.
func (s *Store) SetForceName(ctx context.Context, playthroughID, campaign, name string) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET force_name = ? WHERE playthrough_id = ? AND campaign = ?`), name, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetRunPlayers changes how many players the company plays a started campaign with, which is the count the Deploy wizard starts at.
// sql.ErrNoRows if the campaign hasn't been started.
func (s *Store) SetRunPlayers(ctx context.Context, playthroughID, campaign string, players int) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET players = ? WHERE playthrough_id = ? AND campaign = ?`), players, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MekBayOperation records the MekBay force and operation the site synthesized for one scenario of a campaign run.
type MekBayOperation struct {
	OpForInstance string // the OpFor force's instance ID
	OperationID   string
}

// MekBayOperation loads the stored operation for a scenario; ok is false if none was created yet.
func (s *Store) MekBayOperation(ctx context.Context, playthroughID, campaign, scenarioID string) (op MekBayOperation, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT opfor_instance, operation_id FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`),
		playthroughID, campaign, scenarioID,
	).Scan(&op.OpForInstance, &op.OperationID)
	if errors.Is(err, sql.ErrNoRows) {
		return MekBayOperation{}, false, nil
	}
	return op, err == nil, err
}

// SaveMekBayOperation stores (or replaces) the operation for a scenario of a campaign run.
func (s *Store) SaveMekBayOperation(ctx context.Context, playthroughID, campaign, scenarioID string, op MekBayOperation) error {
	_, err := s.db.ExecContext(ctx,
		s.q(`INSERT INTO mekbay_operations (playthrough_id, campaign, scenario_id, opfor_instance, operation_id) VALUES (?, ?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign, scenario_id) DO UPDATE SET opfor_instance = excluded.opfor_instance, operation_id = excluded.operation_id`),
		playthroughID, campaign, scenarioID, op.OpForInstance, op.OperationID)
	return err
}

const pilotColumns = `id, name, portrait, rank, fallen, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities, sp_total`

func scanPilot(row interface{ Scan(...any) error }) (p NamedPilot, err error) {
	var fallen int
	err = row.Scan(&p.ID, &p.Name, &p.Portrait, &p.Rank, &fallen, &p.SP.Gunnery, &p.SP.Piloting, &p.SP.EdgeTokens, &p.SP.EdgeAbilities, &p.TotalSP)
	p.Fallen = fallen != 0
	return p, err
}

// Pilots lists a campaign run's named pilots, oldest first, fallen ones included.
func (s *Store) Pilots(ctx context.Context, playthroughID, campaign string) ([]NamedPilot, error) {
	rows, err := s.db.QueryContext(ctx,
		s.q(`SELECT `+pilotColumns+` FROM named_pilots WHERE playthrough_id = ? AND campaign = ? ORDER BY created_at, id`),
		playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NamedPilot
	for rows.Next() {
		p, err := scanPilot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // done with this result before the next query
	// The Edge abilities each pilot has chosen, in the order they chose them.
	arows, err := s.db.QueryContext(ctx,
		s.q(`SELECT a.pilot_id, a.ability FROM pilot_abilities a JOIN named_pilots p ON p.id = a.pilot_id
		     WHERE p.playthrough_id = ? AND p.campaign = ? ORDER BY a.pilot_id, a.seq`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	byPilot := map[string][]string{}
	for arows.Next() {
		var pid, ability string
		if err := arows.Scan(&pid, &ability); err != nil {
			return nil, err
		}
		byPilot[pid] = append(byPilot[pid], ability)
	}
	for i := range out {
		out[i].Learned = byPilot[out[i].ID]
	}
	return out, arows.Err()
}

// AddPilot names a new pilot with the given starting SP. A command may have at most max living named pilots
// (ErrPilotLimit otherwise).
// AddPilot adds a pilot with the lowest rank and every SP allocated (see AddPilotWithRank).
func (s *Store) AddPilot(ctx context.Context, playthroughID, campaign, name, portrait string, start SPAllocation, max int) (NamedPilot, error) {
	return s.AddPilotWithRank(ctx, playthroughID, campaign, name, portrait, "", start, start.Total(), max)
}

// AddPilotWithRank adds a living named pilot to a campaign run with the given rank ("" is the lowest) and starting SP, refusing
// it when the command already has max living pilots (ErrPilotLimit), another living pilot has the portrait (ErrPortraitTaken) or
// the name is taken (ErrNameTaken, ignoring case).
func (s *Store) AddPilotWithRank(ctx context.Context, playthroughID, campaign, name, portrait, rank string, start SPAllocation, total, max int) (NamedPilot, error) {
	total = max2(total, start.Total()) // the total can't be below what is allocated; any more is unallocated
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NamedPilot{}, err
	}
	defer tx.Rollback()
	var alive int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0`),
		playthroughID, campaign).Scan(&alive); err != nil {
		return NamedPilot{}, err
	}
	if alive >= max {
		return NamedPilot{}, ErrPilotLimit
	}
	if portrait != "" {
		var same int
		if err := tx.QueryRowContext(ctx,
			s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0 AND portrait = ?`),
			playthroughID, campaign, portrait).Scan(&same); err != nil {
			return NamedPilot{}, err
		}
		if same > 0 {
			return NamedPilot{}, ErrPortraitTaken
		}
	}
	var named int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND LOWER(name) = LOWER(?)`),
		playthroughID, campaign, name).Scan(&named); err != nil {
		return NamedPilot{}, err
	}
	if named > 0 {
		return NamedPilot{}, ErrNameTaken
	}
	p := NamedPilot{ID: newID(), Name: name, Portrait: portrait, Rank: rank, SP: start, TotalSP: total}
	if _, err := tx.ExecContext(ctx,
		s.q(`INSERT INTO named_pilots (id, playthrough_id, campaign, name, portrait, rank, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities, sp_total) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		p.ID, playthroughID, campaign, name, portrait, rank, start.Gunnery, start.Piloting, start.EdgeTokens, start.EdgeAbilities, total); err != nil {
		return NamedPilot{}, err
	}
	return p, tx.Commit()
}

// AwardSP adds SP to a living pilot's columns (sql.ErrNoRows if there is no such pilot, or they have fallen).
func (s *Store) AwardSP(ctx context.Context, playthroughID, campaign, pilotID string, a SPAllocation) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_gunnery = sp_gunnery + ?, sp_piloting = sp_piloting + ?, sp_edge_tokens = sp_edge_tokens + ?, sp_edge_abilities = sp_edge_abilities + ?, sp_total = sp_total + ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		a.Gunnery, a.Piloting, a.EdgeTokens, a.EdgeAbilities, a.Total(), pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrNotEnoughSP is returned when more SP is allocated than the pilot has unallocated.
var ErrNotEnoughSP = errors.New("that is more SP than the pilot has unallocated")

// EarnSP adds earned SP to a living pilot's total, unallocated until they spend it (sql.ErrNoRows if there is no such
// living pilot).
func (s *Store) EarnSP(ctx context.Context, playthroughID, campaign, pilotID string, n int) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_total = sp_total + ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		n, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if r, _ := res.RowsAffected(); r == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AllocateSP spends a living pilot's unallocated SP: a is added to their four columns, and may not exceed what is
// unallocated (total minus the columns' sum), checked in the same statement that writes it so two requests can't both
// spend the same SP. sql.ErrNoRows if there is no such living pilot, ErrNotEnoughSP if a is too much.
func (s *Store) AllocateSP(ctx context.Context, playthroughID, campaign, pilotID string, a SPAllocation) error {
	return s.ApplySP(ctx, playthroughID, campaign, pilotID, 0, a)
}

// ApplySP adds `earned` SP to a living pilot's total and allocates `a` to their columns, both in one statement, so the
// Manage SP dialog can stage earned SP and an allocation and commit them together or not at all. The allocation may use
// the pilot's unallocated SP including the SP being earned. sql.ErrNoRows if there is no such living pilot, ErrNotEnoughSP
// if a is more than that.
func (s *Store) ApplySP(ctx context.Context, playthroughID, campaign, pilotID string, earned int, a SPAllocation) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_total = sp_total + ?,
		       sp_gunnery = sp_gunnery + ?, sp_piloting = sp_piloting + ?, sp_edge_tokens = sp_edge_tokens + ?, sp_edge_abilities = sp_edge_abilities + ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0
		       AND sp_total + ? - (sp_gunnery + sp_piloting + sp_edge_tokens + sp_edge_abilities) >= ?`),
		earned, a.Gunnery, a.Piloting, a.EdgeTokens, a.EdgeAbilities, pilotID, playthroughID, campaign, earned, a.Total())
	if err != nil {
		return err
	}
	if r, _ := res.RowsAffected(); r > 0 {
		return nil
	}
	var one int
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT 1 FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), pilotID, playthroughID, campaign).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	return ErrNotEnoughSP
}

// MarkFallen records that a pilot died, which frees their place among the command's named pilots
// (sql.ErrNoRows if there is no such living pilot).
func (s *Store) MarkFallen(ctx context.Context, playthroughID, campaign, pilotID string) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET fallen = 1 WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrAlreadyStarted is returned when pilots are given to start a campaign that has already been started.
var ErrAlreadyStarted = errors.New("this campaign has already been started")

// StartRunWithPilots starts a campaign and creates its starting named pilots, all or nothing. It refuses
// (ErrAlreadyStarted) if the campaign was already started, so submitting the wizard twice can't make duplicate pilots.
func (s *Store) StartRunWithPilots(ctx context.Context, playthroughID, campaign string, h Hangar, pilots []NewPilot, warchest, players int, forceName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`),
		playthroughID, campaign).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return ErrAlreadyStarted
	}
	if _, err := tx.ExecContext(ctx,
		s.q(`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance, source_url, warchest, players, force_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		playthroughID, campaign, h.URL, h.Instance, h.Source, warchest, players, forceName); err != nil {
		return err
	}
	for _, p := range pilots {
		id := newID()
		if _, err := tx.ExecContext(ctx,
			s.q(`INSERT INTO named_pilots (id, playthrough_id, campaign, name, portrait, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities, sp_total) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			id, playthroughID, campaign, p.Name, p.Portrait, p.SP.Gunnery, p.SP.Piloting, p.SP.EdgeTokens, p.SP.EdgeAbilities, p.SP.Total()); err != nil {
			return err
		}
		for i, a := range p.Abilities {
			if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO pilot_abilities (pilot_id, ability, seq) VALUES (?, ?, ?)`), id, a, i+1); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// SetPilotPortrait changes a living pilot's portrait. sql.ErrNoRows if there is no such living pilot, ErrPortraitTaken if
// another living pilot of the campaign already has it.
func (s *Store) SetPilotPortrait(ctx context.Context, playthroughID, campaign, pilotID, portrait string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var same int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0 AND portrait = ? AND id <> ?`),
		playthroughID, campaign, portrait, pilotID).Scan(&same); err != nil {
		return err
	}
	if same > 0 {
		return ErrPortraitTaken
	}
	res, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET portrait = ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		portrait, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// DeleteRun removes a started campaign from a playthrough: its named pilots, its saved MekBay operation IDs and the run
// itself, in one transaction. The company, its other campaigns and anything saved in MekBay are left alone. It reports
// whether there was a run to delete.
func (s *Store) DeleteRun(ctx context.Context, playthroughID, campaign string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		s.q(`DELETE FROM pilot_abilities WHERE pilot_id IN (SELECT id FROM named_pilots WHERE playthrough_id = ? AND campaign = ?)`),
		playthroughID, campaign); err != nil {
		return false, err
	}
	for _, table := range []string{"named_pilots", "mekbay_operations", "hangar_cache", "scenario_completions", "campaign_keywords"} {
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+table+" WHERE playthrough_id = ? AND campaign = ?"), playthroughID, campaign); err != nil {
			return false, err
		}
	}
	res, err := tx.ExecContext(ctx, s.q(`DELETE FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, tx.Commit()
}

// RevivePilot brings a pilot marked killed in action back, to correct a misclick (sql.ErrNoRows if there is no such
// killed pilot, ErrPilotLimit if the command already has the most living pilots it may have). If another living pilot has
// taken the portrait in the meantime, the pilot gets altPortrait instead.
func (s *Store) RevivePilot(ctx context.Context, playthroughID, campaign, pilotID, altPortrait string, max int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var portrait string
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT portrait FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 1`),
		pilotID, playthroughID, campaign).Scan(&portrait); err != nil {
		return err // sql.ErrNoRows when there is no such killed pilot
	}
	var alive int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0`),
		playthroughID, campaign).Scan(&alive); err != nil {
		return err
	}
	if alive >= max {
		return ErrPilotLimit
	}
	if portrait != "" {
		var same int
		if err := tx.QueryRowContext(ctx,
			s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND fallen = 0 AND portrait = ?`),
			playthroughID, campaign, portrait).Scan(&same); err != nil {
			return err
		}
		if same > 0 {
			portrait = altPortrait
		}
	}
	if _, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET fallen = 0, portrait = ? WHERE id = ? AND playthrough_id = ? AND campaign = ?`),
		portrait, pilotID, playthroughID, campaign); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePilot permanently removes a pilot, living or killed, to correct a misclick or a desync (sql.ErrNoRows if there is
// no such pilot).
func (s *Store) DeletePilot(ctx context.Context, playthroughID, campaign, pilotID string) error {
	if _, err := s.db.ExecContext(ctx,
		s.q(`DELETE FROM pilot_abilities WHERE pilot_id IN (SELECT id FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?)`),
		pilotID, playthroughID, campaign); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		s.q(`DELETE FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ErrTotalBelowAllocated is returned when a pilot's total SP would be set below what is already allocated without
// agreeing to reset the allocation.
var ErrTotalBelowAllocated = errors.New("that total is below the SP already allocated")

// ResetPilotSP is the factory reset for a pilot's SP allocation, to repair a desync or a mistake: all four columns go back to
// zero, so the pilot is a starting 4/5 pilot again, and every SP they have earned becomes unallocated (the total is
// unchanged). The Edge abilities they had learned depended on that SP, so they are forgotten too, in the same transaction.
// It works on living and killed pilots (sql.ErrNoRows if there is no such pilot).
func (s *Store) ResetPilotSP(ctx context.Context, playthroughID, campaign, pilotID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_gunnery = 0, sp_piloting = 0, sp_edge_tokens = 0, sp_edge_abilities = 0
		     WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM pilot_abilities WHERE pilot_id = ?`), pilotID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPilotTotal overrides a pilot's total SP earned. A total below what is already allocated can't be honoured without
// undoing allocations, so it is refused (ErrTotalBelowAllocated) unless resetAllocation is true, which also zeroes the
// four columns, leaving the whole new total unallocated, and forgets the pilot's learned Edge abilities (they depended on that
// SP; see ResetPilotSP). A total at or above the allocation changes neither. sql.ErrNoRows if there is no such pilot.
func (s *Store) SetPilotTotal(ctx context.Context, playthroughID, campaign, pilotID string, total int, resetAllocation bool) error {
	if resetAllocation {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		res, err := tx.ExecContext(ctx,
			s.q(`UPDATE named_pilots SET sp_total = ?, sp_gunnery = 0, sp_piloting = 0, sp_edge_tokens = 0, sp_edge_abilities = 0
			     WHERE id = ? AND playthrough_id = ? AND campaign = ?`), total, pilotID, playthroughID, campaign)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM pilot_abilities WHERE pilot_id = ?`), pilotID); err != nil {
			return err
		}
		return tx.Commit()
	}
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_total = ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ?
		       AND ? >= sp_gunnery + sp_piloting + sp_edge_tokens + sp_edge_abilities`), total, pilotID, playthroughID, campaign, total)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var one int
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT 1 FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	return ErrTotalBelowAllocated
}

// ErrNameTaken is returned when another pilot of the campaign already has the name (compared ignoring case).
var ErrNameTaken = errors.New("another pilot already has that name")

// RenamePilot changes a pilot's name, living or killed in action. Names stay unique within a campaign, ignoring case
// (ErrNameTaken); sql.ErrNoRows if there is no such pilot.
func (s *Store) RenamePilot(ctx context.Context, playthroughID, campaign, pilotID, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var same int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT COUNT(*) FROM named_pilots WHERE playthrough_id = ? AND campaign = ? AND LOWER(name) = LOWER(?) AND id <> ?`),
		playthroughID, campaign, name, pilotID).Scan(&same); err != nil {
		return err
	}
	if same > 0 {
		return ErrNameTaken
	}
	res, err := tx.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET name = ? WHERE id = ? AND playthrough_id = ? AND campaign = ?`), name, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// SetPilotRank sets a pilot's cosmetic rank. The caller checks it against the campaign's ranks (sql.ErrNoRows if there is no
// such pilot).
func (s *Store) SetPilotRank(ctx context.Context, playthroughID, campaign, pilotID, rank string) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET rank = ? WHERE id = ? AND playthrough_id = ? AND campaign = ?`), rank, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Edge ability errors.
var (
	ErrNoPendingAbilities = errors.New("this pilot has no new Edge ability to choose")
	ErrAbilityCount       = errors.New("choose exactly as many Edge abilities as the pilot has earned")
	ErrUnknownAbility     = errors.New("that is not an Edge ability")
	ErrAbilityLearned     = errors.New("that Edge ability is already learned (or chosen twice)")
)

// LearnAbilities records the Edge abilities a living pilot chooses for the levels they have earned. The pilot may hold as many as their
// Edge abilities level; ids must name exactly as many new ones as are still pending, each from the book's list and none already learned,
// chosen together in one go. The choice is permanent until Store.ResetEdgeAbilities. sql.ErrNoRows if there is no such living pilot.
func (s *Store) LearnAbilities(ctx context.Context, playthroughID, campaign, pilotID string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sp int
	if err := tx.QueryRowContext(ctx,
		s.q(`SELECT sp_edge_abilities FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		pilotID, playthroughID, campaign).Scan(&sp); err != nil {
		return err // sql.ErrNoRows when there is no such living pilot
	}
	var learned int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM pilot_abilities WHERE pilot_id = ?`), pilotID).Scan(&learned); err != nil {
		return err
	}
	pending := edgeAbilitiesLadder.at(sp).Level - learned
	if pending <= 0 {
		return ErrNoPendingAbilities
	}
	if len(ids) != pending {
		return ErrAbilityCount
	}
	have := map[string]bool{}
	rows, err := tx.QueryContext(ctx, s.q(`SELECT ability FROM pilot_abilities WHERE pilot_id = ?`), pilotID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return err
		}
		have[a] = true
	}
	rows.Close()
	for _, id := range ids {
		if _, ok := edgeAbilityByID(id); !ok {
			return ErrUnknownAbility
		}
		if have[id] {
			return ErrAbilityLearned
		}
		have[id] = true
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO pilot_abilities (pilot_id, ability, seq) VALUES (?, ?, ?)`), pilotID, id, learned+i+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResetEdgeAbilities forgets every Edge ability a pilot has learned, so they can choose them again (their SP is untouched). It works on
// living and killed pilots, to repair a mistake (sql.ErrNoRows if there is no such pilot).
func (s *Store) ResetEdgeAbilities(ctx context.Context, playthroughID, campaign, pilotID string) error {
	var one int
	if err := s.db.QueryRowContext(ctx,
		s.q(`SELECT 1 FROM named_pilots WHERE id = ? AND playthrough_id = ? AND campaign = ?`), pilotID, playthroughID, campaign).Scan(&one); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM pilot_abilities WHERE pilot_id = ?`), pilotID)
	return err
}
