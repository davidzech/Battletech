package site

import "testing"

// The outcome expressions are expr-lang expressions (https://expr-lang.org), compiled against the scenario's inputs; these tests pin down what the engine relies on.
func TestOutcomeExpressions(t *testing.T) {
	env := map[string]any{"kills": 0, "killed": false, "won": false, "salvage": 0}
	vars := map[string]any{"kills": 2, "killed": false, "won": true, "salvage": 120}
	cond := func(src string) bool {
		t.Helper()
		e, err := CompileCondition(src, env)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		got, err := e.Holds(vars)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		return got
	}
	for src, want := range map[string]bool{
		"kills == 2":                        true,
		"kills != 2":                        false,
		"kills >= 2 and not killed":         true,
		"kills >= 3 or won":                 true,
		"killed or kills == 0":              false,
		"not (kills > 1 and kills < 3)":     false,
		"kills > 1 and kills < 3 or killed": true,
		"won":                               true,
		"kills in [1, 2, 3]":                true,
		"kills + salvage / 60 == 4":         true,
	} {
		if got := cond(src); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
	amount := func(src string) float64 {
		t.Helper()
		e, err := CompileAmount(src, env)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		got, err := e.Number(vars)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		return got
	}
	for src, want := range map[string]float64{
		"150":                             150,
		"kills * 150":                     300,
		"(kills + 1) * 100":               300,
		"min(kills, 1) * 100":             100,
		"max(kills, 5)":                   5,
		"won ? 250 : 0":                   250,
		"salvage / 3":                     40,
		"kills * 150 + (killed ? 0 : 50)": 350,
		"10 / 4":                          2.5,
		"-kills + 5":                      3,
		"floor(salvage / 7)":              17,
	} {
		if got := amount(src); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
	// Mistakes are caught when the scenario loads, not when a mission is completed.
	for src, kind := range map[string]string{
		"":                 "condition",
		"kills ==":         "condition",
		"(kills":           "condition",
		"killz == 2":       "condition", // a name that is no input
		"kills":            "condition", // a number is not true or false
		"kills and killed": "condition", // and needs true/false on both sides
		"kills == true":    "condition",
		"kills +":          "amount",
		"won":              "amount", // a bool is not a number
		"nope * 2":         "amount",
		"kills * 'x'":      "amount",
	} {
		var err error
		if kind == "condition" {
			_, err = CompileCondition(src, env)
		} else {
			_, err = CompileAmount(src, env)
		}
		if err == nil {
			t.Errorf("%s %q should not compile", kind, src)
		}
	}
	// The names an expression reads (used to tell a pilot rule from a mission rule).
	e, _ := CompileAmount("kills * 150 + (won ? salvage : kills)", env)
	if got := e.Names(); len(got) != 3 || got[0] != "kills" || got[1] != "won" || got[2] != "salvage" {
		t.Errorf("names: %v", got)
	}
	if e.Source() != "kills * 150 + (won ? salvage : kills)" {
		t.Error("source")
	}
	if roundInt(2.5) != 3 || roundInt(-2.5) != -3 || roundInt(-0.4) != 0 || roundInt(149.6) != 150 {
		t.Error("roundInt")
	}
	// A runtime failure is an error, not a panic.
	d, err := CompileAmount("10 / kills", env)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := d.Number(map[string]any{"kills": 0, "killed": false, "won": false, "salvage": 0}); err == nil && v == 0 {
		t.Log("expr divides by zero without error, giving", v)
	}
}
