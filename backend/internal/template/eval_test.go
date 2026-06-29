package template

import (
	"math"
	"testing"
)

// evalCtx is a small fixed context used by the expression tests.
func evalCtx() map[string]any {
	return map[string]any{
		"event_type": "linear",
		"linear": map[string]any{
			"action": "update",
			"data": map[string]any{
				"identifier": "ENG-42",
				"number":     float64(42),
				"state":      map[string]any{"name": "In Progress", "type": "started"},
				"labels": []any{
					map[string]any{"name": "bug"},
					map[string]any{"name": "sync"},
				},
			},
		},
		"github": nil,
	}
}

// mustEval is a helper that evaluates and fails on error.
func mustEval(t *testing.T, expr string) any {
	t.Helper()
	v, err := evalExpr(expr, evalCtx())
	if err != nil {
		t.Fatalf("evalExpr(%q) error: %v", expr, err)
	}
	return v
}

func TestEvaluate_Booleans(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		// literals + truthiness
		{"true", true},
		{"false", false},
		{"!true", false},
		{"!false", true},
		{"!''", true},
		{"!'x'", false},
		{"!0", true},
		{"!1", false},
		{"!null", true},

		// equality (case-insensitive strings)
		{"'Done' == 'done'", true},
		{"'Done' != 'done'", false},
		{"linear.action == 'UPDATE'", true},
		{"linear.data.state.name == 'in progress'", true},
		{"linear.data.identifier == 'ENG-42'", true},
		{"linear.missing == null", true},
		{"github == null", true},

		// number/string/bool coercion in ==
		{"1 == '1'", true},
		{"1 == true", true},
		{"0 == false", true},
		{"0 == null", true},
		{"'' == 0", true},
		{"'' == null", true},
		{"'abc' == 0", false}, // unparseable string -> NaN, never equal

		// relational (numeric coercion)
		{"linear.data.number > 41", true},
		{"linear.data.number >= 42", true},
		{"linear.data.number < 42", false},
		{"'10' > '9'", true},    // both coerce to numbers
		{"'abc' > 1", false},    // NaN relational -> false
		{"1 < 2 == true", true}, // precedence: (1<2) == true

		// logical operators + precedence
		{"true && false", false},
		{"true && true", true},
		{"false || true", true},
		{"false || false", false},
		{"true || false && false", true}, // && binds tighter than ||
		{"(true || false) && false", false},
		{"linear.action == 'update' && linear.data.state.type == 'started'", true},
		{"linear.action == 'create' || linear.data.number == 42", true},

		// functions
		{"contains('hello world', 'WORLD')", true},
		{"contains('hello', 'z')", false},
		{"contains(linear.data.labels.*.name, 'sync')", true},
		{"contains(linear.data.labels.*.name, 'nope')", false},
		{"startsWith('NotifBuddy', 'notif')", true},
		{"endsWith('channel.go', '.GO')", true},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			v := mustEval(t, tc.expr)
			if got := truthy(v); got != tc.want {
				t.Fatalf("Evaluate(%q) = %v (val %#v), want %v", tc.expr, got, v, tc.want)
			}
		})
	}
}

// TestLooseEqualMatrix exhaustively checks the cross-type == coercion rules.
func TestLooseEqualMatrix(t *testing.T) {
	cases := []struct {
		l, r any
		want bool
	}{
		{nil, nil, true},
		{nil, float64(0), true},
		{nil, false, true},
		{nil, "", true},
		{true, float64(1), true},
		{false, float64(0), true},
		{true, false, false},
		{"1", float64(1), true},
		{"1.5", 1.5, true},
		{"", float64(0), true},
		{"abc", float64(0), false},  // NaN
		{"Abc", "aBC", true},        // case-insensitive
		{[]any{1}, []any{1}, false}, // arrays coerce to NaN
		{map[string]any{}, float64(0), false},
		{float64(2), float64(2), true},
		{float64(2), "2", true},
	}
	for _, tc := range cases {
		if got := looseEqual(tc.l, tc.r); got != tc.want {
			t.Errorf("looseEqual(%#v, %#v) = %v, want %v", tc.l, tc.r, got, tc.want)
		}
	}
}

func TestToNumber(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		nan  bool
	}{
		{nil, 0, false},
		{true, 1, false},
		{false, 0, false},
		{"", 0, false},
		{"42", 42, false},
		{"0x10", 16, false},
		{"-2.5e1", -25, false},
		{"abc", 0, true},
		{[]any{}, 0, true},
		{map[string]any{}, 0, true},
	}
	for _, tc := range cases {
		got := toNumber(tc.in)
		if tc.nan {
			if !math.IsNaN(got) {
				t.Errorf("toNumber(%#v) = %v, want NaN", tc.in, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("toNumber(%#v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestRender(t *testing.T) {
	cases := []struct {
		tmpl string
		want string
	}{
		{"tkt-${{ linear.data.identifier }}", "tkt-ENG-42"},
		{"pr-${{ linear.data.number }}", "pr-42"},
		{"${{ linear.action }}-${{ linear.data.state.type }}", "update-started"},
		{"no-interpolation", "no-interpolation"},
		{"${{ linear.missing }}", ""}, // null -> empty
		{"${{ 'literal' }}", "literal"},
		{"${{ format('{0}/{1}', 'a', 'b') }}", "a/b"},
		{"${{ join(linear.data.labels.*.name, '-') }}", "bug-sync"},
		{"flag-${{ linear.data.number > 40 }}", "flag-true"},
	}
	for _, tc := range cases {
		t.Run(tc.tmpl, func(t *testing.T) {
			got, err := renderTemplate(tc.tmpl, evalCtx())
			if err != nil {
				t.Fatalf("renderTemplate(%q) error: %v", tc.tmpl, err)
			}
			if got != tc.want {
				t.Fatalf("renderTemplate(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestFunctions(t *testing.T) {
	cases := []struct {
		expr string
		want any
	}{
		{"format('{0}-{1}-{0}', 'x', 'y')", "x-y-x"},
		{"format('{{literal}} {0}', 'v')", "{literal} v"},
		{"join(fromJSON('[1,2,3]'), '+')", "1+2+3"},
		{"join(fromJSON('[\"a\",\"b\"]'))", "a,b"},
		{"toJSON(fromJSON('{\"k\":1}'))", "{\n  \"k\": 1\n}"},
		{"fromJSON('true')", true},
		{"fromJSON('42')", float64(42)},
		{"startsWith('hello', 'he')", true},
		{"endsWith('hello', 'lo')", true},
		{"contains('hello', 'ell')", true},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			got := mustEval(t, tc.expr)
			if !looseDeepEqual(got, tc.want) {
				t.Fatalf("eval(%q) = %#v, want %#v", tc.expr, got, tc.want)
			}
		})
	}
}

func looseDeepEqual(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return a == b
	}
}

func TestParseAndEvalErrors(t *testing.T) {
	bad := []string{
		"1 +",            // unexpected end
		"(1",             // unterminated paren
		"a[",             // unterminated index
		"'unterminated",  // unterminated string
		"1 = 1",          // single =
		"true & false",   // single &
		"true | false",   // single |
		"@",              // bad char
		"contains('a')",  // wrong arity
		"success()",      // CI-only function
		"hashFiles('x')", // CI-only function
		"nope('a','b')",  // unknown function
		"1 2",            // trailing token
	}
	for _, expr := range bad {
		t.Run(expr, func(t *testing.T) {
			if _, err := evalExpr(expr, evalCtx()); err == nil {
				t.Fatalf("evalExpr(%q) expected error, got nil", expr)
			}
		})
	}
}

func TestRenderError(t *testing.T) {
	if _, err := renderTemplate("x-${{ 1 +", evalCtx()); err == nil {
		t.Fatal("expected error for unterminated ${{")
	}
	if _, err := renderTemplate("x-${{ success() }}", evalCtx()); err == nil {
		t.Fatal("expected error for CI-only function in render")
	}
}
