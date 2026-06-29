package template

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// evalExpr parses and evaluates an expression against ctx, returning the raw value.
func evalExpr(expr string, ctx map[string]any) (any, error) {
	n, err := parse(expr)
	if err != nil {
		return nil, err
	}
	return n.eval(ctx)
}

// --- node evaluation ---------------------------------------------------------

func (n *litNode) eval(map[string]any) (any, error) { return n.v, nil }

func (n *identNode) eval(ctx map[string]any) (any, error) {
	// A bare identifier resolves against the root context. Unknown top-level
	// names resolve to null (GitHub Actions treats missing context as null).
	if v, ok := ctx[n.name]; ok {
		return v, nil
	}
	return nil, nil
}

func (n *memberNode) eval(ctx map[string]any) (any, error) {
	base, err := n.base.eval(ctx)
	if err != nil {
		return nil, err
	}
	return member(base, n.prop), nil
}

func (n *indexNode) eval(ctx map[string]any) (any, error) {
	base, err := n.base.eval(ctx)
	if err != nil {
		return nil, err
	}
	idx, err := n.idx.eval(ctx)
	if err != nil {
		return nil, err
	}
	switch b := base.(type) {
	case map[string]any:
		key, ok := idx.(string)
		if !ok {
			return nil, nil
		}
		return b[key], nil
	case []any:
		f, ok := idx.(float64)
		if !ok {
			return nil, nil
		}
		i := int(f)
		if i < 0 || i >= len(b) {
			return nil, nil
		}
		return b[i], nil
	default:
		return nil, nil
	}
}

// filterNode implements the `*` object filter: base.* yields an array of the
// values of base (if a map) or base itself (if already an array).
func (n *filterNode) eval(ctx map[string]any) (any, error) {
	base, err := n.base.eval(ctx)
	if err != nil {
		return nil, err
	}
	switch b := base.(type) {
	case []any:
		return b, nil
	case map[string]any:
		out := make([]any, 0, len(b))
		for _, v := range b {
			out = append(out, v)
		}
		return out, nil
	default:
		return []any{}, nil
	}
}

// member follows a `*`-produced array by collecting prop from each element
// (so `labels.*.name` works), or reads prop from a map.
func member(base any, prop string) any {
	switch b := base.(type) {
	case map[string]any:
		return b[prop]
	case []any: // result of a prior `.*` filter — map prop over elements
		out := make([]any, 0, len(b))
		for _, el := range b {
			if m, ok := el.(map[string]any); ok {
				out = append(out, m[prop])
			}
		}
		return out
	default:
		return nil
	}
}

func (n *notNode) eval(ctx map[string]any) (any, error) {
	v, err := n.x.eval(ctx)
	if err != nil {
		return nil, err
	}
	return !truthy(v), nil
}

func (n *binNode) eval(ctx map[string]any) (any, error) {
	// && / || short-circuit and return the operand VALUE (GitHub semantics),
	// not a coerced bool.
	switch n.op {
	case tAnd:
		l, err := n.l.eval(ctx)
		if err != nil {
			return nil, err
		}
		if !truthy(l) {
			return l, nil
		}
		return n.r.eval(ctx)
	case tOr:
		l, err := n.l.eval(ctx)
		if err != nil {
			return nil, err
		}
		if truthy(l) {
			return l, nil
		}
		return n.r.eval(ctx)
	}

	l, err := n.l.eval(ctx)
	if err != nil {
		return nil, err
	}
	r, err := n.r.eval(ctx)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case tEq:
		return looseEqual(l, r), nil
	case tNe:
		return !looseEqual(l, r), nil
	case tLt, tLe, tGt, tGe:
		return compare(n.op, l, r), nil
	default:
		return nil, fmt.Errorf("template: unknown operator")
	}
}

func (n *callNode) eval(ctx map[string]any) (any, error) {
	args := make([]any, len(n.args))
	for i, a := range n.args {
		v, err := a.eval(ctx)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	return callFunction(n.name, args)
}

// --- truthiness, coercion, comparison (GitHub Actions semantics) -------------

// truthy: falsy are false, 0, -0, "", null; everything else is true.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x) // 0, -0 and NaN are falsy
	case string:
		return x != ""
	default:
		return true // arrays/objects are truthy
	}
}

// toNumber coerces a value to a float per GitHub's == rules: null→0, false→0,
// true→1, ""→0, numeric string→its value, unparseable/array/object→NaN.
func toNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		if x == "" {
			return 0
		}
		f, err := parseNumber(strings.TrimSpace(x))
		if err != nil {
			return math.NaN()
		}
		return f
	default:
		return math.NaN()
	}
}

// looseEqual implements == : same-type string compares case-insensitively;
// otherwise both sides are coerced to number.
func looseEqual(l, r any) bool {
	ls, lok := l.(string)
	rs, rok := r.(string)
	if lok && rok {
		return strings.EqualFold(ls, rs)
	}
	// null == null, and matching bools, compare cleanly via number coercion too,
	// but handle the both-null / both-bool identity explicitly for clarity.
	if l == nil && r == nil {
		return true
	}
	lf, rf := toNumber(l), toNumber(r)
	if math.IsNaN(lf) || math.IsNaN(rf) {
		return false
	}
	return lf == rf
}

// compare implements <, <=, >, >= via number coercion. Any NaN → false.
func compare(op tokenKind, l, r any) bool {
	// String-to-string relational comparison also coerces to number in GitHub
	// Actions, so we follow the same path uniformly.
	lf, rf := toNumber(l), toNumber(r)
	if math.IsNaN(lf) || math.IsNaN(rf) {
		return false
	}
	switch op {
	case tLt:
		return lf < rf
	case tLe:
		return lf <= rf
	case tGt:
		return lf > rf
	case tGe:
		return lf >= rf
	}
	return false
}

// parseNumber parses a GitHub-Actions numeric literal: decimal, hex (0x...), or
// exponential float.
func parseNumber(s string) (float64, error) {
	if len(s) > 2 && (s[0:2] == "0x" || s[0:2] == "0X") {
		u, err := strconv.ParseInt(s[2:], 16, 64)
		if err != nil {
			return 0, err
		}
		return float64(u), nil
	}
	return strconv.ParseFloat(s, 64)
}

// --- string rendering (${{ ... }} interpolation) -----------------------------

// renderTemplate expands every ${{ expr }} occurrence in tmpl. Text outside the
// delimiters is literal. The inner expression is evaluated and stringified.
func renderTemplate(tmpl string, ctx map[string]any) (string, error) {
	var sb strings.Builder
	i := 0
	for i < len(tmpl) {
		start := strings.Index(tmpl[i:], "${{")
		if start < 0 {
			sb.WriteString(tmpl[i:])
			break
		}
		start += i
		sb.WriteString(tmpl[i:start])
		end := strings.Index(tmpl[start:], "}}")
		if end < 0 {
			return "", fmt.Errorf("template: unterminated ${{ at %d", start)
		}
		exprStr := tmpl[start+3 : start+end]
		v, err := evalExpr(exprStr, ctx)
		if err != nil {
			return "", err
		}
		sb.WriteString(stringify(v))
		i = start + end + 2
	}
	return sb.String(), nil
}

// stringify renders a value the way GitHub Actions interpolates it into a
// string: null→"", numbers without a trailing .0, booleans as true/false,
// strings as-is, arrays/objects as JSON.
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(x)
	default:
		return toJSONString(v, false)
	}
}

// formatNumber prints integers without a decimal point and floats compactly.
func formatNumber(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
