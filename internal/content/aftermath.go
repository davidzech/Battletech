package content

import (
	"fmt"
	"html/template"
	"strings"
)

// A scenario's aftermath is what the players read once the mission is completed, in an "Aftermath" transmission on its page: the front matter's `aftermath`
// list in Outcome.md (in-world transmissions about what the players did, each shown when its `when` held), then the body's `## Aftermath` section (what became of
// the war). A `when` is a condition over the mission's inputs (its objectives and mission-scope inputs, as in the outcome rules), checked against the recorded
// report; an entry without one is always shown. A scenario completed without a report (Mark completed, or a report from before the inputs were recorded) shows
// only the entries without a condition.

// AftermathText is one transmission of a scenario's aftermath (Outcome.md front matter `aftermath`).
type AftermathText struct {
	When string `yaml:"when"`
	Text string `yaml:"text"`

	when *Expr
	html template.HTML
}

// compileAftermath renders the aftermath texts and compiles their conditions against the scenario's mission inputs.
func (sc *Scenario) compileAftermath() error {
	env := map[string]any{}
	for _, in := range sc.OutcomeInputs {
		if in.Scope != "mission" {
			continue
		}
		if in.Type == "bool" {
			env[in.ID] = false
		} else {
			env[in.ID] = 0
		}
	}
	for i := range sc.Aftermath {
		a := &sc.Aftermath[i]
		if strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("aftermath %d has no text", i+1)
		}
		a.html = renderBriefing(strings.TrimSpace(a.Text))
		if strings.TrimSpace(a.When) == "" {
			continue
		}
		if !sc.HasOutcome() {
			return fmt.Errorf("aftermath %d: a when needs the outcome engine's inputs, and this scenario has none", i+1)
		}
		e, err := CompileCondition(a.When, env)
		if err != nil {
			return fmt.Errorf("aftermath %d: when %q: %v", i+1, a.When, err)
		}
		a.when = e
	}
	return nil
}

// AftermathFor is the scenario's aftermath for a completed mission whose recorded report is res (nil when it has none): the transmissions whose condition held,
// then the `## Aftermath` section. It is empty when the scenario has no aftermath.
func (sc *Scenario) AftermathFor(res *OutcomeResult) template.HTML {
	var b strings.Builder
	vars := sc.recordedMission(res)
	for _, a := range sc.Aftermath {
		if a.when != nil {
			if vars == nil {
				continue
			}
			if ok, err := a.when.Holds(vars); err != nil || !ok {
				continue
			}
		}
		b.WriteString(string(a.html))
	}
	b.WriteString(string(sc.AftermathHTML))
	return template.HTML(b.String())
}

// recordedMission turns a recorded report's mission inputs back into the types the conditions were compiled with (JSON brings every number back as a float). It
// is nil when the report holds no value for one of the inputs, since a condition can't then be judged.
func (sc *Scenario) recordedMission(res *OutcomeResult) map[string]any {
	if res == nil || res.Mission == nil {
		return nil
	}
	vars := map[string]any{}
	for _, in := range sc.OutcomeInputs {
		if in.Scope != "mission" {
			continue
		}
		switch v := res.Mission[in.ID].(type) {
		case bool:
			vars[in.ID] = v
		case float64:
			vars[in.ID] = int(v)
		case int:
			vars[in.ID] = v
		default:
			return nil
		}
	}
	return vars
}
