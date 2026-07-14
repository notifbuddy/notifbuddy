package template

import (
	"strings"
	"testing"
)

// A hostile expression must be rejected with an error, never crash the process
// via stack overflow. Depth and length are bounded (see parser.go).
func TestParse_DepthBounded(t *testing.T) {
	// Nested parens/brackets/'!' all recurse; each must error past maxParseDepth
	// instead of recursing toward a fatal stack overflow. Kept under maxExprLen
	// so the depth guard (not the length guard) is what fires.
	cases := map[string]string{
		"parens":   strings.Repeat("(", 1000) + "1" + strings.Repeat(")", 1000),
		"brackets": "linear" + strings.Repeat("[0", 1000) + strings.Repeat("]", 1000),
		"not":      strings.Repeat("!", 1000) + "true",
	}
	for name, expr := range cases {
		if _, err := parse(expr); err == nil {
			t.Errorf("%s: expected a depth error, got nil (would risk stack overflow)", name)
		}
	}
	// A legitimately shaped expression still parses.
	if _, err := parse("linear.action == 'update' && !(linear.data.number > 3)"); err != nil {
		t.Fatalf("normal expression should parse, got: %v", err)
	}
}

func TestParse_LengthBounded(t *testing.T) {
	if _, err := parse(strings.Repeat("a", maxExprLen+1)); err == nil {
		t.Error("expected an over-length error, got nil")
	}
}
