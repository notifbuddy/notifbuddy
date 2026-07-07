package template

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ciOnlyFunctions are GitHub Actions functions that only make sense inside a
// workflow run (they need a CI context we don't have). Calling them is an error
// rather than a silent false, so misuse is caught loudly.
var ciOnlyFunctions = map[string]bool{
	"hashfiles": true,
	"success":   true,
	"always":    true,
	"cancelled": true,
	"failure":   true,
}

// callFunction dispatches a built-in function call. Names are case-insensitive
// (GitHub Actions functions are), matching the spec.
func callFunction(name string, args []any) (any, error) {
	lname := strings.ToLower(name)
	if ciOnlyFunctions[lname] {
		return nil, fmt.Errorf("template: function %s() is not supported (CI-only)", name)
	}
	switch lname {
	case "contains":
		if err := wantArgs(name, args, 2); err != nil {
			return nil, err
		}
		return fnContains(args[0], args[1]), nil
	case "startswith":
		if err := wantArgs(name, args, 2); err != nil {
			return nil, err
		}
		return strings.HasPrefix(strings.ToLower(coerceString(args[0])), strings.ToLower(coerceString(args[1]))), nil
	case "endswith":
		if err := wantArgs(name, args, 2); err != nil {
			return nil, err
		}
		return strings.HasSuffix(strings.ToLower(coerceString(args[0])), strings.ToLower(coerceString(args[1]))), nil
	case "format":
		if len(args) < 1 {
			return nil, fmt.Errorf("template: format() needs at least 1 argument")
		}
		return fnFormat(coerceString(args[0]), args[1:])
	case "join":
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("template: join() takes 1 or 2 arguments")
		}
		sep := ","
		if len(args) == 2 {
			sep = coerceString(args[1])
		}
		return fnJoin(args[0], sep), nil
	case "tojson":
		if err := wantArgs(name, args, 1); err != nil {
			return nil, err
		}
		return toJSONString(args[0], true), nil
	case "fromjson":
		if err := wantArgs(name, args, 1); err != nil {
			return nil, err
		}
		return fnFromJSON(coerceString(args[0]))
	// Handlebars-style extension helpers: not part of the GitHub Actions
	// dialect. New string helpers (uppercase, capitalize, …) go here.
	case "lowercase":
		if err := wantArgs(name, args, 1); err != nil {
			return nil, err
		}
		return strings.ToLower(coerceString(args[0])), nil
	default:
		return nil, fmt.Errorf("template: unknown function %s()", name)
	}
}

func wantArgs(name string, args []any, n int) error {
	if len(args) != n {
		return fmt.Errorf("template: %s() takes %d arguments, got %d", name, n, len(args))
	}
	return nil
}

// fnContains: substring match for strings; element match for arrays. Both are
// case-insensitive and coerce to string, per GitHub Actions.
func fnContains(search, item any) bool {
	if arr, ok := search.([]any); ok {
		for _, el := range arr {
			if looseEqual(el, item) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(coerceString(search)), strings.ToLower(coerceString(item)))
}

// fnFormat replaces {0},{1},... with the given args; {{ and }} are literal
// braces.
func fnFormat(format string, args []any) (string, error) {
	var sb strings.Builder
	i := 0
	for i < len(format) {
		c := format[i]
		switch c {
		case '{':
			if i+1 < len(format) && format[i+1] == '{' {
				sb.WriteByte('{')
				i += 2
				continue
			}
			end := strings.IndexByte(format[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("template: format() unmatched '{'")
			}
			idxStr := format[i+1 : i+end]
			var idx int
			if _, err := fmt.Sscanf(idxStr, "%d", &idx); err != nil {
				return "", fmt.Errorf("template: format() bad placeholder {%s}", idxStr)
			}
			if idx < 0 || idx >= len(args) {
				return "", fmt.Errorf("template: format() placeholder {%d} out of range", idx)
			}
			sb.WriteString(stringify(args[idx]))
			i += end + 1
		case '}':
			if i+1 < len(format) && format[i+1] == '}' {
				sb.WriteByte('}')
				i += 2
				continue
			}
			return "", fmt.Errorf("template: format() unmatched '}'")
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String(), nil
}

// fnJoin concatenates array elements (stringified) with sep. A non-array value
// is stringified on its own.
func fnJoin(v any, sep string) string {
	arr, ok := v.([]any)
	if !ok {
		return stringify(v)
	}
	parts := make([]string, len(arr))
	for i, el := range arr {
		parts[i] = stringify(el)
	}
	return strings.Join(parts, sep)
}

// fnFromJSON parses a JSON string into a value (numbers become float64).
func fnFromJSON(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("template: fromJSON() invalid JSON: %w", err)
	}
	return v, nil
}

// coerceString turns any value into its string form for the string functions.
func coerceString(v any) string { return stringify(v) }

// toJSONString serializes v to JSON. pretty=true matches toJSON()'s
// human-readable output.
func toJSONString(v any, pretty bool) string {
	var b []byte
	var err error
	if pretty {
		b, err = json.MarshalIndent(v, "", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return ""
	}
	return string(b)
}
