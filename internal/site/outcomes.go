package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// The outcome engine. A scenario's Outcome.md says, in its front matter, what the players report when the mission is over and what follows from it. Most scenarios
// follow the BattleTech: Aces model:
//
//  1. SP are granted to the force for the objectives that were met: the mission's POOL.
//  2. The mission gives a flat amount of SP to each active pilot (`pilot_sp`); a benched pilot (one who did not take part) gets half, and a pilot killed gets none and
//     is killed in action. These shares are drawn from the pool.
//  3. What is left of the pool goes to the warchest.
//  4. Objectives met (or missed) grant KEYWORDS, which go into the campaign's ledger (Keywords.md is the registry).
//
// Say the objectives met total 600 SP and the mission gives each pilot 80. Four pilots, two active: (80 + 80) + (40 + 40) = 240 come out of the pool and go to the
// pilots, and the other 360 go to the warchest. If an active pilot was killed instead, they get nothing and do not draw: 80 + 0 + 40 + 40 = 160, and 440 are left.
// (A pool too small to pay every share in full is split between the pilots in proportion to what they were due.)
//
//	pilot_sp: 80                       # the flat share of each active pilot (a number or an expression over the mission inputs)
//	objectives:                        # shorthand for a yes/no mission input, and a rule adding its SP to the pool
//	  - id: depot_destroyed
//	    label: Destroy the depot
//	    sp: 400
//	    grants: [depot_gone]           # keywords granted when it is met
//	    unmet_grants: [depot_standing] # ... and when it is not
//
// A scenario that is not like that, such as the bootstrap mission Scenario 000, can say what it needs with `inputs` and `outcomes` (a list of rules) instead, and
// grant SP straight to a pilot rather than from a pool:
//
//	inputs:
//	  - id: kills            # a number (the default type), asked for each named pilot (the default scope)
//	    label: Kills
//	    max: 4
//	outcomes:
//	  - when: kills == 2 and not killed
//	    sp: 400              # SP the pilot earns, directly: a number or an expression (see expr.go)
//	  - when: kills == 0
//	    kia: true            # the pilot is killed in action
//	    free_recruits: 1     # one free recruit (no SP cost) for the replacement
//
// Every pilot has two built-in inputs, `Active` (unticked, the pilot is benched) and `killed` (a bool, true if they were killed in action). An input has a `scope`:
// `pilot` (one value for each active named pilot) or `mission` (one value for the whole mission). A rule is a pilot rule when it gives SP or kills the pilot or reads
// a pilot input (`killed` is one): it is tried for each active pilot. Any other rule is a mission rule and is tried once. `when` is optional (always true). Pilot rules
// may give `sp` (straight to the pilot) and `kia`; mission rules may give `pool` (SP into the pool) and `grants` (keywords); either may give `warchest` (SP added to
// the warchest) and `free_recruits` (recruits that cost no SP), which add up over every rule that matches. A `label` replaces the rule's automatic note in the report.
//
// The players enter what happened in the completion dialog, which previews the result by asking the server (POST /api/campaign/scenario-outcome); committing it
// (POST /api/campaign/scenario-outcome/commit) applies every effect and records the report in one transaction, so a mission can only be completed once.

// OutcomeInput is something the players report when a mission is completed.
type OutcomeInput struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
	Scope string `yaml:"scope" json:"scope"` // "pilot" (the default) or "mission"
	Type  string `yaml:"type" json:"type"`   // "number" (the default) or "bool"
	Min   int    `yaml:"min" json:"min"`
	Max   int    `yaml:"max" json:"max"` // for a number: 99 when omitted
	Help  string `yaml:"help" json:"help,omitempty"`
	// Required (a yes/no input only): it must be true to complete the mission, so the dialog ticks it and cannot untick it (a primary objective).
	Required bool `yaml:"required" json:"required,omitempty"`
}

// OutcomeObjective is the shorthand for an objective of the mission: a yes/no mission input, and what meeting it (or missing it) grants.
type OutcomeObjective struct {
	ID          string   `yaml:"id"`
	Label       string   `yaml:"label"`
	SP          any      `yaml:"sp"`           // SP added to the pool when the objective is met
	Grants      []string `yaml:"grants"`       // keywords granted when it is met
	UnmetGrants []string `yaml:"unmet_grants"` // keywords granted when it is not
	Required    bool     `yaml:"required"`     // a primary objective: the mission cannot be completed without it
}

// OutcomeRule is one rule of a scenario's outcomes.
type OutcomeRule struct {
	When         string   `yaml:"when"`
	Label        string   `yaml:"label"`
	SP           any      `yaml:"sp"`
	Pool         any      `yaml:"pool"`
	Warchest     any      `yaml:"warchest"`
	KIA          bool     `yaml:"kia"`
	FreeRecruits int      `yaml:"free_recruits"`
	Grants       []string `yaml:"grants"`

	when, sp, pool, warchest *Expr
	pilot                    bool
}

// HasOutcome reports whether the scenario has an outcome engine (something to report and rules to apply).
func (sc *Scenario) HasOutcome() bool {
	return sc != nil && sc.OutcomeError == "" && (len(sc.OutcomeRules) > 0 || sc.pilotShare != nil)
}

// keywordID is a keyword's ID: lowercase snake_case.
var keywordID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// compileOutcomes turns the objectives into inputs and rules, fills in the input defaults and compiles every expression; a mistake in the front matter is kept in
// OutcomeError (and the scenario then has no engine).
func (sc *Scenario) compileOutcomes() {
	fail := func(format string, args ...any) { sc.OutcomeError = fmt.Sprintf(format, args...) }
	for _, o := range sc.OutcomeObjectives {
		if o.ID == "" {
			fail("an objective has no id")
			return
		}
		in := OutcomeInput{ID: o.ID, Label: o.Label, Scope: "mission", Type: "bool", Required: o.Required}
		if o.Label == "" {
			in.Label = o.ID
		}
		if n, ok := o.SP.(int); ok {
			in.Help = fmt.Sprintf("%d SP", n)
		}
		sc.OutcomeInputs = append(sc.OutcomeInputs, in)
		sc.OutcomeRules = append(sc.OutcomeRules, OutcomeRule{When: o.ID, Pool: o.SP, Grants: o.Grants})
		if len(o.UnmetGrants) > 0 {
			sc.OutcomeRules = append(sc.OutcomeRules, OutcomeRule{When: "not " + o.ID, Grants: o.UnmetGrants})
		}
	}
	sc.OutcomeObjectives = nil // expanded
	scope := map[string]string{"killed": "pilot"}
	for i := range sc.OutcomeInputs {
		in := &sc.OutcomeInputs[i]
		if in.ID == "" {
			fail("an input has no id")
			return
		}
		if _, dup := scope[in.ID]; dup {
			fail("input %q is listed twice (or is a built-in)", in.ID)
			return
		}
		if in.Scope == "" {
			in.Scope = "pilot"
		}
		if in.Type == "" {
			in.Type = "number"
		}
		if in.Label == "" {
			in.Label = in.ID
		}
		switch {
		case in.Scope != "pilot" && in.Scope != "mission":
			fail("input %q: scope must be pilot or mission", in.ID)
			return
		case in.Type != "number" && in.Type != "bool":
			fail("input %q: type must be number or bool", in.ID)
			return
		}
		if in.Type == "bool" {
			in.Min, in.Max = 0, 1
		} else if in.Max == 0 {
			in.Max = 99
		}
		if in.Min > in.Max {
			fail("input %q: min is above max", in.ID)
			return
		}
		scope[in.ID] = in.Scope
	}
	// Expressions are type-checked against the inputs: a number input is an int, a bool input a bool.
	env := map[string]any{"killed": false}
	for _, in := range sc.OutcomeInputs {
		if in.Type == "bool" {
			env[in.ID] = false
		} else {
			env[in.ID] = 0
		}
	}
	if sc.OutcomePilotSP != nil {
		e, err := CompileAmount(fmt.Sprint(sc.OutcomePilotSP), env)
		if err != nil {
			fail("pilot_sp: %v", err)
			return
		}
		for _, name := range e.Names() {
			if scope[name] != "mission" {
				fail("pilot_sp uses %q, which is not a mission input", name)
				return
			}
		}
		sc.pilotShare = e
	}
	for i := range sc.OutcomeRules {
		r := &sc.OutcomeRules[i]
		var err error
		if strings.TrimSpace(r.When) != "" {
			if r.when, err = CompileCondition(r.When, env); err != nil {
				fail("outcome %d: when %q: %v", i+1, r.When, err)
				return
			}
		}
		for _, amount := range []struct {
			name string
			src  any
			dst  **Expr
		}{{"sp", r.SP, &r.sp}, {"pool", r.Pool, &r.pool}, {"warchest", r.Warchest, &r.warchest}} {
			if amount.src == nil {
				continue
			}
			if *amount.dst, err = CompileAmount(fmt.Sprint(amount.src), env); err != nil {
				fail("outcome %d: %s: %v", i+1, amount.name, err)
				return
			}
		}
		for _, g := range r.Grants {
			if !keywordID.MatchString(g) {
				fail("outcome %d: %q is not a keyword ID (lowercase snake_case)", i+1, g)
				return
			}
		}
		r.pilot = r.sp != nil || r.KIA
		for _, e := range []*Expr{r.when, r.sp, r.pool, r.warchest} {
			if e == nil {
				continue
			}
			for _, name := range e.Names() {
				s, ok := scope[name]
				if !ok {
					fail("outcome %d uses %q, which is not an input", i+1, name)
					return
				}
				r.pilot = r.pilot || s == "pilot"
			}
		}
		if r.pilot && (r.pool != nil || len(r.Grants) > 0) {
			fail("outcome %d: pool and grants belong to the mission as a whole, but this rule is tried for each pilot (it gives SP, kills a pilot or reads a pilot input)", i+1)
			return
		}
	}
}

// GrantedKeywords lists every keyword the scenario's outcome can grant.
func (sc *Scenario) GrantedKeywords() []string {
	var out []string
	for _, r := range sc.OutcomeRules {
		out = append(out, r.Grants...)
	}
	return out
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// OutcomeEntry is what the players reported for one named pilot.
type OutcomeEntry struct {
	PilotID  string         `json:"id"`
	TookPart bool           `json:"tookPart"` // active; false means benched
	Killed   bool           `json:"killed"`   // killed in action (only an active pilot can be)
	MVP      bool           `json:"mvp"`      // the mission's most valuable pilot: the mission's extra MVP SP (at most one, and one who took part and lives)
	Values   map[string]any `json:"values"`   // numbers, or true/false for a bool input
}

// OutcomeInputs is everything the players reported.
type OutcomeInputs struct {
	Pilots  []OutcomeEntry `json:"pilots"`
	Mission map[string]any `json:"mission"`
}

// PilotOutcome is what a mission's outcome does to one named pilot.
type PilotOutcome struct {
	PilotID string   `json:"id"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`  // active, benched or killed
	Entered []string `json:"entered"` // what was reported, as text ("Kills: 2")
	SP      int      `json:"sp"`      // SP the pilot earns in all
	KIA     bool     `json:"kia"`     // killed in action: removed from the campaign
	MVP     bool     `json:"mvp"`     // chosen as the mission's MVP
	Notes   []string `json:"notes"`
}

// OutcomeResult is the full outcome of a mission: what it does to each pilot and to the force.
type OutcomeResult struct {
	Pilots       []PilotOutcome `json:"pilots"`
	Pool         int            `json:"pool"`         // SP granted for the objectives met, before the pilots take their shares
	Paid         int            `json:"paid"`         // SP of the pool the pilots took as their shares (the rest of the pool went to the warchest)
	Warchest     int            `json:"warchest"`     // SP added to the warchest: what is left of the pool, and any direct grants
	FreeRecruits int            `json:"freeRecruits"` // recruits that cost no SP
	Keywords     []string       `json:"keywords"`     // keywords added to the campaign ledger
	Notes        []string       `json:"notes"`        // notes about the mission as a whole
}

// outcomeValue turns a reported value into a number within the input's range: a number must be whole, a bool is true or false.
func outcomeValue(in OutcomeInput, v any) (float64, error) {
	var f float64
	switch x := v.(type) {
	case nil:
		f = 0
	case bool:
		f = b2f(x)
	case float64:
		f = x
	default:
		return 0, fmt.Errorf("%s: not a number", in.Label)
	}
	if f != math.Trunc(f) {
		return 0, fmt.Errorf("%s must be a whole number", in.Label)
	}
	if in.Type == "bool" && f != 0 && f != 1 {
		return 0, fmt.Errorf("%s is yes or no", in.Label)
	}
	if int(f) < in.Min || int(f) > in.Max {
		return 0, fmt.Errorf("%s must be from %d to %d", in.Label, in.Min, in.Max)
	}
	return f, nil
}

func (sc *Scenario) inputValues(scope string, raw map[string]any) (map[string]any, error) {
	known := map[string]bool{}
	out := map[string]any{}
	for _, in := range sc.OutcomeInputs {
		if in.Scope != scope {
			continue
		}
		known[in.ID] = true
		f, err := outcomeValue(in, raw[in.ID])
		if err != nil {
			return nil, err
		}
		if in.Type == "bool" {
			out[in.ID] = f != 0
			if in.Required && f == 0 {
				return nil, fmt.Errorf("%q is required to complete the mission", in.Label)
			}
		} else {
			out[in.ID] = int(f)
		}
	}
	for k := range raw {
		if !known[k] {
			return nil, fmt.Errorf("unknown input %q", k)
		}
	}
	return out, nil
}

func (sc *Scenario) entered(values map[string]any, scope string) []string {
	var out []string
	for _, in := range sc.OutcomeInputs {
		if in.Scope != scope {
			continue
		}
		if in.Type == "bool" {
			out = append(out, in.Label+": "+map[bool]string{true: "yes", false: "no"}[values[in.ID] == true])
		} else {
			out = append(out, fmt.Sprintf("%s: %d", in.Label, values[in.ID]))
		}
	}
	return out
}

// EvaluateOutcome applies the scenario's rules to what the players reported. names maps a living named pilot's ID to their name; an entry for any other pilot is
// refused. Every pilot listed is in the result: active ones go through the rules, benched ones get their half share of the pilot SP, killed ones nothing.
func (sc *Scenario) EvaluateOutcome(in OutcomeInputs, names map[string]string) (OutcomeResult, error) {
	res := OutcomeResult{Pilots: []PilotOutcome{}, Keywords: []string{}, Notes: []string{}}
	mission, err := sc.inputValues("mission", in.Mission)
	if err != nil {
		return res, err
	}
	seen := map[string]bool{}
	mvps := 0
	type taken struct {
		out  *PilotOutcome
		vars map[string]any
	}
	var active []taken
	for _, e := range in.Pilots {
		name, ok := names[e.PilotID]
		if !ok {
			return res, errors.New("one of the pilots is not a living named pilot of this campaign")
		}
		if seen[e.PilotID] {
			return res, fmt.Errorf("%s is listed twice", name)
		}
		seen[e.PilotID] = true
		po := PilotOutcome{PilotID: e.PilotID, Name: name, Status: "benched", Entered: []string{}, Notes: []string{}}
		if e.Killed && !e.TookPart {
			return res, fmt.Errorf("%s was benched, so they cannot have been killed", name)
		}
		if e.MVP {
			mvps++
			if !e.TookPart || e.Killed {
				return res, fmt.Errorf("%s cannot be the MVP: only a pilot who took part and lives can be", name)
			}
			if mvps > 1 {
				return res, errors.New("only one pilot can be the MVP")
			}
			if sc.MVPSP <= 0 {
				return res, errors.New("this mission has no MVP")
			}
		}
		if e.TookPart {
			vals, err := sc.inputValues("pilot", e.Values)
			if err != nil {
				return res, fmt.Errorf("%s: %v", name, err)
			}
			po.Status = "active"
			if e.Killed {
				po.Status = "killed"
			}
			po.Entered = sc.entered(vals, "pilot")
			vars := map[string]any{"killed": e.Killed}
			for k, v := range mission {
				vars[k] = v
			}
			for k, v := range vals {
				vars[k] = v
			}
			po.MVP = e.MVP
			res.Pilots = append(res.Pilots, po)
			active = append(active, taken{vars: vars})
			continue
		}
		res.Pilots = append(res.Pilots, po)
	}
	// res.Pilots may have been reallocated while appending, so the pointers are taken now.
	idx := 0
	for i := range res.Pilots {
		if res.Pilots[i].Status != "benched" {
			active[idx].out = &res.Pilots[i]
			idx++
		}
	}
	granted := map[string]bool{}
	add := func(r *OutcomeRule, vars map[string]any, p *PilotOutcome) error {
		if r.when != nil {
			ok, err := r.when.Holds(vars)
			if err != nil || !ok {
				return err
			}
		}
		var notes []string
		if r.sp != nil {
			v, err := r.sp.Number(vars)
			if err != nil {
				return err
			}
			if n := max(0, roundInt(v)); n > 0 {
				p.SP += n
				notes = append(notes, fmt.Sprintf("+%d SP", n))
			}
		}
		if r.KIA {
			p.KIA = true
			notes = append(notes, "Killed in Action.")
		}
		if r.pool != nil {
			v, err := r.pool.Number(vars)
			if err != nil {
				return err
			}
			if n := max(0, roundInt(v)); n > 0 {
				res.Pool += n
				notes = append(notes, fmt.Sprintf("+%d SP to the pool", n))
			}
		}
		if r.warchest != nil {
			v, err := r.warchest.Number(vars)
			if err != nil {
				return err
			}
			res.Warchest += roundInt(v)
			notes = append(notes, fmt.Sprintf("%+d SP to the warchest", roundInt(v)))
		}
		if r.FreeRecruits > 0 {
			res.FreeRecruits += r.FreeRecruits
			notes = append(notes, fmt.Sprintf("%d free recruit(s)", r.FreeRecruits))
		}
		for _, g := range r.Grants {
			if !granted[g] {
				granted[g] = true
				res.Keywords = append(res.Keywords, g)
			}
			notes = append(notes, "Keyword: "+g)
		}
		if r.Label != "" {
			notes = []string{r.Label}
		} else if p == nil {
			notes = nil // a mission rule's effects show in the totals (pool, warchest, keywords); only a label of its own is a note
		}
		if p != nil {
			p.Notes = append(p.Notes, notes...)
		} else {
			res.Notes = append(res.Notes, notes...)
		}
		return nil
	}
	// Mission rules first (they settle the pool), then each active pilot's own rules.
	for i := range sc.OutcomeRules {
		if r := &sc.OutcomeRules[i]; !r.pilot {
			if err := add(r, mission, nil); err != nil {
				return res, fmt.Errorf("outcome %d: %v", i+1, err)
			}
		}
	}
	for i := range sc.OutcomeRules {
		if r := &sc.OutcomeRules[i]; r.pilot {
			for _, t := range active {
				if err := add(r, t.vars, t.out); err != nil {
					return res, fmt.Errorf("outcome %d: %v", i+1, err)
				}
			}
		}
	}
	for i := range res.Pilots {
		p := &res.Pilots[i]
		if p.Status == "killed" && !p.KIA {
			p.KIA = true
			p.Notes = append(p.Notes, "Killed in Action.")
		}
	}
	// The pilots' shares come out of the pool: the pilot SP to each active pilot, half to each benched one, nothing to a pilot who was killed. What is left goes to
	// the warchest. A pool that can't pay every share in full is split in proportion to the shares.
	if sc.pilotShare != nil {
		v, err := sc.pilotShare.Number(mission)
		if err != nil {
			return res, fmt.Errorf("pilot_sp: %v", err)
		}
		share := max(0, roundInt(v))
		due := make([]int, len(res.Pilots))
		total := 0
		for i, p := range res.Pilots {
			switch p.Status {
			case "active":
				due[i] = share
			case "benched":
				due[i] = share / 2
			}
			total += due[i]
		}
		for i := range res.Pilots {
			p := &res.Pilots[i]
			pay := due[i]
			if total > res.Pool && total > 0 {
				pay = due[i] * res.Pool / total
			}
			if pay > 0 {
				p.SP += pay
				if p.Status == "benched" {
					p.Notes = append(p.Notes, fmt.Sprintf("+%d SP (benched: half)", pay))
				} else {
					p.Notes = append(p.Notes, fmt.Sprintf("+%d SP", pay))
				}
			}
			due[i] = pay
		}
		paid := 0
		for _, d := range due {
			paid += d
		}
		res.Warchest += res.Pool - paid
		res.Paid = paid
	} else if res.Pool > 0 {
		res.Warchest += res.Pool
	}
	// The MVP earns the mission's extra MVP SP, on top of their share and not drawn from the pool.
	for i := range res.Pilots {
		if p := &res.Pilots[i]; p.MVP {
			p.SP += sc.MVPSP
			p.Notes = append(p.Notes, fmt.Sprintf("+%d SP (MVP)", sc.MVPSP))
		}
	}
	for i := range res.Pilots {
		if p := &res.Pilots[i]; len(p.Notes) == 0 {
			if p.Status == "benched" {
				p.Notes = append(p.Notes, "Benched: no rewards, no losses.")
			} else {
				p.Notes = append(p.Notes, "No effect.")
			}
		}
	}
	return res, nil
}

// ---- storage

// ErrAlreadyCompleted is returned when a mission that has already been completed is completed again.
var ErrAlreadyCompleted = errors.New("this mission is already completed")

// ErrOutcomePilot is returned when a pilot the outcome affects is no longer a living named pilot of the campaign.
var ErrOutcomePilot = errors.New("a pilot in the outcome is no longer a living named pilot")

// ErrOutcomeFunds is returned when an outcome would take the warchest below zero.
var ErrOutcomeFunds = errors.New("the outcome would take the warchest below zero")

// CompleteWithOutcome applies a mission's outcome and records the mission as completed, all in one transaction: SP to the pilots who earned it, pilots killed in
// action, the warchest and the free recruits, the keywords granted (into the campaign ledger), and the completion row holding the report. It fails with ErrAlreadyCompleted if the mission was already completed, so
// the rewards can never be given twice.
func (s *Store) CompleteWithOutcome(ctx context.Context, playthroughID, campaign, scenario string, res OutcomeResult) error {
	record, err := json.Marshal(res)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM scenario_completions WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrAlreadyCompleted
	}
	for _, p := range res.Pilots {
		if p.SP > 0 {
			r, err := tx.ExecContext(ctx, s.q(`UPDATE named_pilots SET sp_total = sp_total + ? WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), p.SP, p.PilotID, playthroughID, campaign)
			if err != nil {
				return err
			}
			if c, _ := r.RowsAffected(); c == 0 {
				return ErrOutcomePilot
			}
		}
		if p.KIA {
			r, err := tx.ExecContext(ctx, s.q(`UPDATE named_pilots SET fallen = 1 WHERE id = ? AND playthrough_id = ? AND campaign = ? AND fallen = 0`), p.PilotID, playthroughID, campaign)
			if err != nil {
				return err
			}
			if c, _ := r.RowsAffected(); c == 0 {
				return ErrOutcomePilot
			}
		}
	}
	if res.Warchest != 0 || res.FreeRecruits != 0 {
		r, err := tx.ExecContext(ctx, s.q(`UPDATE campaign_runs SET warchest = warchest + ?, free_recruits = free_recruits + ? WHERE playthrough_id = ? AND campaign = ? AND warchest + ? >= 0`), res.Warchest, res.FreeRecruits, playthroughID, campaign, res.Warchest)
		if err != nil {
			return err
		}
		if c, _ := r.RowsAffected(); c == 0 {
			return ErrOutcomeFunds
		}
	}
	for _, k := range res.Keywords {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO campaign_keywords (playthrough_id, campaign, keyword, scenario_id) VALUES (?, ?, ?, ?) ON CONFLICT (playthrough_id, campaign, keyword) DO NOTHING`), playthroughID, campaign, k, scenario); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id, outcome) VALUES (?, ?, ?, ?)`), playthroughID, campaign, scenario, string(record)); err != nil {
		return err
	}
	// The battle is over: the scenario has no operation in progress any more.
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM mekbay_operations WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario); err != nil {
		return err
	}
	return tx.Commit()
}

// CompletionOutcome returns the recorded report of a completed mission ("" with ok when it was completed without an outcome); ok is false if it isn't completed.
func (s *Store) CompletionOutcome(ctx context.Context, playthroughID, campaign, scenario string) (res *OutcomeResult, ok bool, err error) {
	var raw string
	err = s.db.QueryRowContext(ctx, s.q(`SELECT outcome FROM scenario_completions WHERE playthrough_id = ? AND campaign = ? AND scenario_id = ?`), playthroughID, campaign, scenario).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if raw == "" {
		return nil, true, nil
	}
	var r OutcomeResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, true, nil // a record this version can't read is still a completion
	}
	return &r, true, nil
}

// ConsumeFreeRecruit uses up one free recruit, if the run has one; it reports whether it did.
func (s *Store) ConsumeFreeRecruit(ctx context.Context, playthroughID, campaign string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET free_recruits = free_recruits - 1 WHERE playthrough_id = ? AND campaign = ? AND free_recruits > 0`), playthroughID, campaign)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RestoreFreeRecruit gives a free recruit back, when the recruit it was used for failed.
func (s *Store) RestoreFreeRecruit(ctx context.Context, playthroughID, campaign string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE campaign_runs SET free_recruits = free_recruits + 1 WHERE playthrough_id = ? AND campaign = ?`), playthroughID, campaign)
	return err
}

// ---- endpoints

// outcomeRequest checks a scenario-outcome request and evaluates it: a same-origin request from a started campaign, for an available scenario with an outcome engine
// that is not completed yet. It returns what the engine makes of the reported inputs.
func (s *Server) outcomeRequest(w http.ResponseWriter, r *http.Request) (pt Playthrough, c *Campaign, sc *Scenario, res OutcomeResult, ok bool) {
	pt, c, ok = s.pilotRequest(w, r)
	if !ok {
		return
	}
	ok = false
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if err != nil || !started || !run.Started() {
		http.Error(w, "Start this campaign first.", http.StatusBadRequest)
		return
	}
	sc, found := c.Scenario(r.PostFormValue("scenario"))
	if !found {
		http.Error(w, "unknown scenario", http.StatusNotFound)
		return
	}
	if !sc.HasOutcome() {
		http.Error(w, "This mission has no outcome to report.", http.StatusBadRequest)
		return
	}
	completed, err := s.store.Completions(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load the campaign log", http.StatusInternalServerError)
		return
	}
	if completed[sc.Slug] {
		http.Error(w, "This mission is already completed.", http.StatusConflict)
		return
	}
	if !c.Available(completed)[sc.Slug] {
		http.Error(w, "That scenario isn't available yet: complete the one before it first.", http.StatusConflict)
		return
	}
	var in OutcomeInputs
	if err := json.Unmarshal([]byte(r.PostFormValue("inputs")), &in); err != nil {
		http.Error(w, "The outcome form could not be read.", http.StatusBadRequest)
		return
	}
	pilots, err := s.store.Pilots(r.Context(), pt.ID, c.Slug)
	if err != nil {
		http.Error(w, "could not load your pilots", http.StatusInternalServerError)
		return
	}
	names := map[string]string{}
	for _, p := range pilots {
		if !p.Fallen {
			names[p.ID] = p.Name
		}
	}
	res, err = sc.EvaluateOutcome(in, names)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	return pt, c, sc, res, true
}

// previewOutcome is POST /api/campaign/scenario-outcome: what the reported inputs would do, without changing anything.
func (s *Server) previewOutcome(w http.ResponseWriter, r *http.Request) {
	_, _, _, res, ok := s.outcomeRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}

// commitOutcome completes a scenario that has an outcome engine: the same request as the preview, applied.
func (s *Server) commitOutcome(w http.ResponseWriter, r *http.Request) {
	pt, c, sc, res, ok := s.outcomeRequest(w, r)
	if !ok {
		return
	}
	if !s.writeBackBattle(w, r, pt, c, sc) {
		return
	}
	// Two stages for the hangar: its frozen copy is made now, and the checkpoint that holds it is recorded once the completion is saved.
	copyID, cerr := "", error(nil)
	if run, ok, err := s.store.Run(r.Context(), pt.ID, c.Slug); err == nil && ok {
		copyID, cerr = s.checkpointCopy(r.Context(), currentSeed(r), run)
	}
	if cerr != nil {
		http.Error(w, "Could not save a checkpoint of your hangar in MekBay: "+cerr.Error(), http.StatusBadGateway)
		return
	}
	label := "Completed " + c.MissionName(*sc)
	switch err := s.store.CompleteWithOutcome(r.Context(), pt.ID, c.Slug, sc.Slug, res); {
	case errors.Is(err, ErrAlreadyCompleted):
		http.Error(w, "This mission is already completed.", http.StatusConflict)
	case errors.Is(err, ErrOutcomePilot):
		http.Error(w, "One of the pilots is no longer a living named pilot. Reload and report again.", http.StatusConflict)
	case errors.Is(err, ErrOutcomeFunds):
		http.Error(w, "That outcome would take the warchest below zero.", http.StatusConflict)
	case err != nil:
		http.Error(w, "could not save the outcome", http.StatusInternalServerError)
	default:
		s.recordCheckpoint(r.Context(), currentSeed(r), pt.ID, c.Slug, label, copyID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if copyID != "" {
		s.deleteHangars(r.Context(), currentSeed(r), []string{copyID}) // the completion was not saved: the staged copy is not needed
	}
}

// LedgerEntry is a keyword in the campaign's ledger: which scenario granted it.
type LedgerEntry struct {
	Keyword  string
	Scenario string
}

// Ledger returns the keywords a playthrough's campaign has been granted, oldest first. Keywords belong to the campaign as a whole, not to any pilot.
func (s *Store) Ledger(ctx context.Context, playthroughID, campaign string) ([]LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT keyword, scenario_id FROM campaign_keywords WHERE playthrough_id = ? AND campaign = ? ORDER BY granted_at, keyword`), playthroughID, campaign)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.Keyword, &e.Scenario); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// writeBackBattle, for a mission fought with the player's own force (not a provided one) that has a battle in progress, writes the battle's units back into the
// hangar before the mission is completed (see commitbattle.go), so a failure leaves the mission open to try again (writing the same state twice changes nothing). It
// answers the request itself and returns false when it fails.
func (s *Server) writeBackBattle(w http.ResponseWriter, r *http.Request, pt Playthrough, c *Campaign, sc *Scenario) bool {
	if sc.provides() {
		return true
	}
	op, had, err := s.store.MekBayOperation(r.Context(), pt.ID, c.Slug, sc.ID)
	if err != nil {
		http.Error(w, "could not load the battle", http.StatusInternalServerError)
		return false
	}
	run, started, err := s.store.Run(r.Context(), pt.ID, c.Slug)
	if !had || err != nil || !started || !run.Owned() {
		return true // no battle to write back, or a hangar the site doesn't own
	}
	rep, err := s.mekbay.CommitBattle(r.Context(), CommitBattleRequest{OwnerUID: accountUID(currentSeed(r), SlotHangar), HangarInstance: run.HangarInstance, OperationID: op.OperationID})
	if err != nil {
		http.Error(w, "Could not write the battle's damage back into your hangar: "+err.Error(), http.StatusBadGateway)
		return false
	}
	if len(rep.Skipped) > 0 {
		log.Printf("battle write-back: %d units written, no match in the hangar for %v", rep.Matched, rep.Skipped)
	}
	return true
}
