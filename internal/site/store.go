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

// Playthrough is one account's tracked progress through the campaigns, identified by its seed.
type Playthrough struct {
	ID          string // hash of the seed, never the seed itself
	AccountName string // the account's name, whatever the player likes to call it ("" until named); stored in the playthroughs.company_name column
}

// Configured reports whether the account has been named, which is what setting a seed up amounts to.
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
	Players        int    // how many players this campaign is played with: the Deploy wizard's default count (0 on a run from before it was asked)
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
		// The players' own MekBay user IDs are no longer asked for (every player's account is derived from the seed); forget any that were saved.
		`DROP TABLE IF EXISTS player_accounts`,
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
	status_json    TEXT NOT NULL DEFAULT '',
	status_at      INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (playthrough_id, campaign, scenario_id)
)`,
		`CREATE TABLE IF NOT EXISTS campaign_checkpoints (
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign       TEXT NOT NULL,
	seq            INTEGER NOT NULL,
	label          TEXT NOT NULL,
	created_at     INTEGER NOT NULL,
	hangar_copy    TEXT NOT NULL DEFAULT '',
	snapshot       TEXT NOT NULL,
	PRIMARY KEY (playthrough_id, campaign, seq)
)`,
		`CREATE TABLE IF NOT EXISTS mekbay_links (
	id             TEXT PRIMARY KEY,
	playthrough_id TEXT NOT NULL REFERENCES playthroughs(id) ON DELETE CASCADE,
	campaign       TEXT NOT NULL,
	label          TEXT NOT NULL,
	sealed         TEXT NOT NULL,
	ttl_seconds    BIGINT NOT NULL,
	expires_at     BIGINT NOT NULL,
	UNIQUE (playthrough_id, campaign, label)
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
	if err := s.ensureColumn("mekbay_operations", "status_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("mekbay_operations", "status_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
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

// SetAccountName stores the account's name.
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

// SetRunPlayers changes how many players a started campaign is played with, which is the count the Deploy wizard starts at.
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

// DeleteRun removes a started campaign from a playthrough: its named pilots, its saved MekBay operation IDs and the run
// itself, in one transaction. The account, its other campaigns and anything saved in MekBay are left alone. It reports
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
	for _, table := range []string{"named_pilots", "mekbay_operations", "mekbay_links", "hangar_cache", "scenario_completions", "campaign_keywords", "campaign_checkpoints"} {
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
