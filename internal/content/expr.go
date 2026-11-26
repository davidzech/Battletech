package content

import (
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

// Expressions for scenario outcomes (Outcome.md front matter): the `when` condition of a rule and the numbers it awards, written by hand in YAML. They are
// https://expr-lang.org expressions: a small, side-effect-free language with numbers, true and false, names, `+ - * / %`, comparisons, `and`, `or`, `not`,
// `cond ? a : b`, `in`, and builtins such as `min`, `max` and `floor`. They are type-checked when the scenario loads, against the scenario's inputs (a number
// input is an int, a bool input a bool), so a typo in a name or a condition that isn't true or false is caught at once rather than when a mission is completed.
//
//	kills >= 4 and not killed        killed or kills == 0
//	kills * 150                      min(kills, 3) * 100 + (won ? 250 : 0)

// Expr is a compiled expression.
type Expr struct {
	src   string
	prog  *vm.Program
	names []string
}

// Source is the text the expression was compiled from.
func (e *Expr) Source() string { return e.src }

// Names lists the variable names the expression uses.
func (e *Expr) Names() []string { return e.names }

func compile(src string, env map[string]any, opts ...expr.Option) (*Expr, error) {
	var v nameCollector // collects the names as the expression is compiled, so it is parsed once
	prog, err := expr.Compile(src, append([]expr.Option{expr.Env(env), expr.Patch(&v)}, opts...)...)
	if err != nil {
		return nil, err
	}
	return &Expr{src: src, prog: prog, names: v.names}, nil
}

// CompileCondition compiles a condition: an expression that must be true or false, over the named inputs (env maps each name to a value of its type).
func CompileCondition(src string, env map[string]any) (*Expr, error) {
	return compile(src, env, expr.AsBool())
}

// CompileAmount compiles an expression that must be a number (a whole number result is converted), over the named inputs.
func CompileAmount(src string, env map[string]any) (*Expr, error) {
	return compile(src, env, expr.AsFloat64())
}

// Holds evaluates a condition.
func (e *Expr) Holds(vars map[string]any) (bool, error) {
	v, err := vm.Run(e.prog, vars)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%q is not true or false", e.src)
	}
	return b, nil
}

// Number evaluates an amount.
func (e *Expr) Number(vars map[string]any) (float64, error) {
	v, err := vm.Run(e.prog, vars)
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case float64:
		return n, nil
	}
	return 0, fmt.Errorf("%q is not a number", e.src)
}

type nameCollector struct {
	names []string
	seen  map[string]bool
}

func (c *nameCollector) Visit(node *ast.Node) {
	if id, ok := (*node).(*ast.IdentifierNode); ok {
		if c.seen == nil {
			c.seen = map[string]bool{}
		}
		if !c.seen[id.Value] {
			c.seen[id.Value] = true
			c.names = append(c.names, id.Value)
		}
	}
}

// roundInt rounds an amount to a whole number.
func roundInt(f float64) int {
	if f < 0 {
		return -int(-f + 0.5)
	}
	return int(f + 0.5)
}
