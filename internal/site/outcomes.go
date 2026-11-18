package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// The outcome engine. A scenario's Outcome.md says, in its front matter, what the players report when the mission is over (`inputs`) and what follows from it
// (`outcomes`, a list of rules). Scenario 000 is the model: each named pilot reports how many kills they got and whether they were killed, and the rules turn that
// into SP, a removed pilot and a free replacement.
//
//	inputs:
//	  - id: kills            # a number (the default type), asked for each named pilot (the default scope)
//	    label: Kills
//	    max: 4
//	  - id: killed
//	    label: Killed in action
//	    type: bool
//	outcomes:
//	  - when: kills == 2 and not killed
//	    sp: 400              # SP the pilot earns: a number or an expression (see expr.go)
//	  - when: killed or kills == 0
//	    kia: true            # the pilot is killed in action
//	    free_recruits: 1     # one free recruit (no SP cost) for the replacement
//
// An input has a `scope`: `pilot` (one value for each named pilot that took part) or `mission` (one value for the whole mission). A rule is a pilot rule when it gives
// SP or kills the pilot or reads a pilot input: it is tried for each pilot that took part. Any other rule is a mission rule and is tried once. `when` is optional
// (always true). The effects are `sp` and `kia` (pilot rules only), and `warchest` (SP added to the warchest) and `free_recruits` (recruits that cost no SP), which
// add up over every rule and pilot that matches. A `label` replaces the rule's automatic note in the report.
//
// The players enter the inputs in the completion dialog, which previews the result by asking the server (POST /api/campaign/scenario-outcome); committing it
// (POST /api/campaign/scenario-complete) applies every effect and records the report in one transaction, so a mission can only be completed once.

// OutcomeInput is something the players report when a mission is completed.
type OutcomeInput struct {
	ID    string `yaml:"id" json:"id"`
	Label string `yaml:"label" json:"label"`
	Scope string `yaml:"scope" json:"scope"` // "pilot" (the default) or "mission"
	Type  string `yaml:"type" json:"type"`   // "number" (the default) or "bool"
	Min   int    `yaml:"min" json:"min"`
	Max   int    `yaml:"max" json:"max"` // for a number: 99 when omitted
	Help  string `yaml:"help" json:"help,omitempty"`
}

// OutcomeRule is one rule of a scenario's outcomes.
type OutcomeRule struct {
	When         string `yaml:"when"`
	Label        string `yaml:"label"`
	SP           any    `yaml:"sp"`
	Warchest     any    `yaml:"warchest"`
	KIA          bool   `yaml:"kia"`
	FreeRecruits int    `yaml:"free_recruits"`

	when, sp, warchest *Expr
	pilot              bool
}

// HasOutcome reports whether the scenario has an outcome engine (inputs to report and rules to apply).
func (sc *Scenario) HasOutcome() bool {
	return sc != nil && len(sc.OutcomeRules) > 0 && sc.OutcomeError == ""
}

// compileOutcomes fills in the input defaults and parses every rule; a mistake in the front matter is kept in OutcomeError (and the scenario then has no engine).
func (sc *Scenario) compileOutcomes() {
	fail := func(format string, args ...any) { sc.OutcomeError = fmt.Sprintf(format, args...) }
	scope := map[string]string{}
	for i := range sc.OutcomeInputs {
		in := &sc.OutcomeInputs[i]
		if in.ID == "" {
			fail("an input has no id")
			return
		}
		if _, dup := scope[in.ID]; dup {
			fail("input %q is listed twice", in.ID)
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
	env := map[string]any{}
	for _, in := range sc.OutcomeInputs {
		if in.Type == "bool" {
			env[in.ID] = false
		} else {
			env[in.ID] = 0
		}
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
		if r.SP != nil {
			if r.sp, err = CompileAmount(fmt.Sprint(r.SP), env); err != nil {
				fail("outcome %d: sp: %v", i+1, err)
				return
			}
		}
		if r.Warchest != nil {
			if r.warchest, err = CompileAmount(fmt.Sprint(r.Warchest), env); err != nil {
				fail("outcome %d: warchest: %v", i+1, err)
				return
			}
		}
		r.pilot = r.sp != nil || r.KIA
		for _, e := range []*Expr{r.when, r.sp, r.warchest} {
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
	}
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
	TookPart bool           `json:"tookPart"`
	Values   map[string]any `json:"values"` // numbers, or true/false for a bool input
}

// OutcomeInputs is everything the players reported.
type OutcomeInputs struct {
	Pilots  []OutcomeEntry `json:"pilots"`
	Mission map[string]any `json:"mission"`
}

// PilotOutcome is what a mission's outcome does to one named pilot who took part.
type PilotOutcome struct {
	PilotID string   `json:"id"`
	Name    string   `json:"name"`
	Entered []string `json:"entered"` // what was reported, as text ("Kills: 2")
	SP      int      `json:"sp"`      // SP the pilot earns
	KIA     bool     `json:"kia"`     // killed in action: removed from the campaign
	Notes   []string `json:"notes"`
}

// OutcomeResult is the full outcome of a mission: what it does to each pilot and to the force.
type OutcomeResult struct {
	Pilots       []PilotOutcome `json:"pilots"`
	Warchest     int            `json:"warchest"`     // SP added to the warchest
	FreeRecruits int            `json:"freeRecruits"` // recruits that cost no SP
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
// refused. Pilots who did not take part are left out of the result.
func (sc *Scenario) EvaluateOutcome(in OutcomeInputs, names map[string]string) (OutcomeResult, error) {
	res := OutcomeResult{Pilots: []PilotOutcome{}, Notes: []string{}}
	mission, err := sc.inputValues("mission", in.Mission)
	if err != nil {
		return res, err
	}
	seen := map[string]bool{}
	type taken struct {
		out  *PilotOutcome
		vars map[string]any
	}
	var took []taken
	for _, e := range in.Pilots {
		name, ok := names[e.PilotID]
		if !ok {
			return res, errors.New("one of the pilots is not a living named pilot of this campaign")
		}
		if seen[e.PilotID] {
			return res, fmt.Errorf("%s is listed twice", name)
		}
		seen[e.PilotID] = true
		if !e.TookPart {
			continue
		}
		vals, err := sc.inputValues("pilot", e.Values)
		if err != nil {
			return res, fmt.Errorf("%s: %v", name, err)
		}
		vars := map[string]any{}
		for k, v := range mission {
			vars[k] = v
		}
		for k, v := range vals {
			vars[k] = v
		}
		res.Pilots = append(res.Pilots, PilotOutcome{PilotID: e.PilotID, Name: name, Entered: sc.entered(vals, "pilot"), Notes: []string{}})
		took = append(took, taken{vars: vars})
	}
	for i := range took {
		took[i].out = &res.Pilots[i]
	}
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
			notes = append(notes, "Killed in action: removed from the campaign")
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
		if r.Label != "" {
			notes = []string{r.Label}
		}
		if p != nil {
			p.Notes = append(p.Notes, notes...)
		} else {
			res.Notes = append(res.Notes, notes...)
		}
		return nil
	}
	for i := range sc.OutcomeRules {
		r := &sc.OutcomeRules[i]
		if r.pilot {
			for _, t := range took {
				if err := add(r, t.vars, t.out); err != nil {
					return res, fmt.Errorf("outcome %d: %v", i+1, err)
				}
			}
		} else if err := add(r, mission, nil); err != nil {
			return res, fmt.Errorf("outcome %d: %v", i+1, err)
		}
	}
	if len(res.Notes) == 0 {
		res.Notes = []string{}
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
// action, the warchest and the free recruits, and the completion row holding the report. It fails with ErrAlreadyCompleted if the mission was already completed, so
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
	if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO scenario_completions (playthrough_id, campaign, scenario_id, outcome) VALUES (?, ?, ?, ?)`), playthroughID, campaign, scenario, string(record)); err != nil {
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
		w.WriteHeader(http.StatusNoContent)
	}
}
