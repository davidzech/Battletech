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
	CompanyName string // the company's name in the lore, e.g. "Wolf's Dragoons" ("" until named)
	OwnerUID    string // optional MekBay account UID that owns the company's hangars; secret, never sent to the browser
}

// Configured reports whether the company has been named, which is what setting a seed up amounts to.
func (p Playthrough) Configured() bool { return p.CompanyName != "" }

// CanModify reports whether the playthrough is in modify mode: it holds the hangar owner's UID, so the site
// is allowed to write to the MekBay hangars (MekBay only accepts changes from the owner's UID).
func (p Playthrough) CanModify() bool { return p.OwnerUID != "" }

// CampaignRun is a playthrough's run through one campaign. It exists once the campaign has been started, and has
// its own MekBay hangar.
type CampaignRun struct {
	Campaign       string // the campaign's slug, e.g. "refusalwar"
	HangarURL      string // the MekBay hangar link, reduced to https://mekbay.com/?instance=<id>
	HangarInstance string // the link's ?instance= id
}

// Started reports whether the campaign has been started (a zero CampaignRun means it hasn't).
func (r CampaignRun) Started() bool { return r.HangarInstance != "" }

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
	created_at       %s,
	fallen           INTEGER NOT NULL DEFAULT 0,
	sp_gunnery       INTEGER NOT NULL DEFAULT 0 CHECK (sp_gunnery >= 0),
	sp_piloting      INTEGER NOT NULL DEFAULT 0 CHECK (sp_piloting >= 0),
	sp_edge_tokens   INTEGER NOT NULL DEFAULT 0 CHECK (sp_edge_tokens >= 0),
	sp_edge_abilities INTEGER NOT NULL DEFAULT 0 CHECK (sp_edge_abilities >= 0)
)`, s.timestampColumn()),
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
	if err := s.ensureColumn("named_pilots", "portrait", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
		s.q(`SELECT id, company_name, owner_uid FROM playthroughs WHERE id = ?`), id,
	).Scan(&p.ID, &p.CompanyName, &p.OwnerUID)
	if errors.Is(err, sql.ErrNoRows) {
		return Playthrough{}, false, nil
	}
	return p, err == nil, err
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

// SetCompanyName stores the company's name.
func (s *Store) SetCompanyName(ctx context.Context, id, name string) error {
	return s.updatePlaythrough(ctx, `UPDATE playthroughs SET company_name = ? WHERE id = ?`, name, id)
}

// SetOwnerUID stores the MekBay owner UID that puts a playthrough into modify mode; an empty uid turns it off.
func (s *Store) SetOwnerUID(ctx context.Context, id, uid string) error {
	return s.updatePlaythrough(ctx, `UPDATE playthroughs SET owner_uid = ? WHERE id = ?`, uid, id)
}

// Run loads a playthrough's run through a campaign; ok is false if the campaign hasn't been started.
func (s *Store) Run(ctx context.Context, playthroughID, campaign string) (r CampaignRun, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		s.q(`SELECT campaign, hangar_url, hangar_instance FROM campaign_runs WHERE playthrough_id = ? AND campaign = ?`),
		playthroughID, campaign,
	).Scan(&r.Campaign, &r.HangarURL, &r.HangarInstance)
	if errors.Is(err, sql.ErrNoRows) {
		return CampaignRun{}, false, nil
	}
	return r, err == nil, err
}

// StartRun starts a campaign with its MekBay hangar. Doing it again for a started campaign changes the hangar and
// keeps when it was started.
func (s *Store) StartRun(ctx context.Context, playthroughID, campaign string, h Hangar) error {
	_, err := s.db.ExecContext(ctx,
		s.q(`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance) VALUES (?, ?, ?, ?)
		     ON CONFLICT (playthrough_id, campaign) DO UPDATE SET hangar_url = excluded.hangar_url, hangar_instance = excluded.hangar_instance`),
		playthroughID, campaign, h.URL, h.Instance)
	return err
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

const pilotColumns = `id, name, portrait, fallen, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities`

func scanPilot(row interface{ Scan(...any) error }) (p NamedPilot, err error) {
	var fallen int
	err = row.Scan(&p.ID, &p.Name, &p.Portrait, &fallen, &p.SP.Gunnery, &p.SP.Piloting, &p.SP.EdgeTokens, &p.SP.EdgeAbilities)
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
	return out, rows.Err()
}

// AddPilot names a new pilot with the given starting SP. A command may have at most max living named pilots
// (ErrPilotLimit otherwise).
func (s *Store) AddPilot(ctx context.Context, playthroughID, campaign, name, portrait string, start SPAllocation, max int) (NamedPilot, error) {
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
	p := NamedPilot{ID: newID(), Name: name, Portrait: portrait, SP: start}
	if _, err := tx.ExecContext(ctx,
		s.q(`INSERT INTO named_pilots (id, playthrough_id, campaign, name, portrait, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		p.ID, playthroughID, campaign, name, portrait, start.Gunnery, start.Piloting, start.EdgeTokens, start.EdgeAbilities); err != nil {
		return NamedPilot{}, err
	}
	return p, tx.Commit()
}

// AwardSP adds SP to a living pilot's columns (sql.ErrNoRows if there is no such pilot, or they have fallen).
func (s *Store) AwardSP(ctx context.Context, playthroughID, campaign, pilotID string, a SPAllocation) error {
	res, err := s.db.ExecContext(ctx,
		s.q(`UPDATE named_pilots SET sp_gunnery = sp_gunnery + ?, sp_piloting = sp_piloting + ?, sp_edge_tokens = sp_edge_tokens + ?, sp_edge_abilities = sp_edge_abilities + ?
		     WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`),
		a.Gunnery, a.Piloting, a.EdgeTokens, a.EdgeAbilities, pilotID, playthroughID, campaign)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
func (s *Store) StartRunWithPilots(ctx context.Context, playthroughID, campaign string, h Hangar, pilots []NewPilot) error {
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
		s.q(`INSERT INTO campaign_runs (playthrough_id, campaign, hangar_url, hangar_instance) VALUES (?, ?, ?, ?)`),
		playthroughID, campaign, h.URL, h.Instance); err != nil {
		return err
	}
	for _, p := range pilots {
		if _, err := tx.ExecContext(ctx,
			s.q(`INSERT INTO named_pilots (id, playthrough_id, campaign, name, portrait, sp_gunnery, sp_piloting, sp_edge_tokens, sp_edge_abilities) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			newID(), playthroughID, campaign, p.Name, p.Portrait, p.SP.Gunnery, p.SP.Piloting, p.SP.EdgeTokens, p.SP.EdgeAbilities); err != nil {
			return err
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
	for _, table := range []string{"named_pilots", "mekbay_operations"} {
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
