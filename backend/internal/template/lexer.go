package template

import (
	"fmt"
	"strings"
)

// tokenKind enumerates the lexical tokens of a GitHub Actions expression.
type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNumber
	tString
	tBool
	tNull
	tDot      // .
	tLBracket // [
	tRBracket // ]
	tLParen   // (
	tRParen   // )
	tComma    // ,
	tStar     // *
	tNot      // !
	tLt       // <
	tLe       // <=
	tGt       // >
	tGe       // >=
	tEq       // ==
	tNe       // !=
	tAnd      // &&
	tOr       // ||
)

type token struct {
	kind tokenKind
	text string  // raw text (for idents / errors)
	str  string  // decoded string value (tString)
	num  float64 // numeric value (tNumber)
	b    bool    // bool value (tBool)
	pos  int
}

// lex tokenizes a GitHub Actions expression. It is deliberately strict: any
// character it does not recognize is a hard error, so malformed templates fail
// loudly rather than silently mis-evaluating.
func lex(input string) ([]token, error) {
	var toks []token
	i := 0
	n := len(input)
	for i < n {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '.':
			toks = append(toks, token{kind: tDot, pos: i})
			i++
		case c == '[':
			toks = append(toks, token{kind: tLBracket, pos: i})
			i++
		case c == ']':
			toks = append(toks, token{kind: tRBracket, pos: i})
			i++
		case c == '(':
			toks = append(toks, token{kind: tLParen, pos: i})
			i++
		case c == ')':
			toks = append(toks, token{kind: tRParen, pos: i})
			i++
		case c == ',':
			toks = append(toks, token{kind: tComma, pos: i})
			i++
		case c == '*':
			toks = append(toks, token{kind: tStar, pos: i})
			i++
		case c == '!':
			if i+1 < n && input[i+1] == '=' {
				toks = append(toks, token{kind: tNe, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tNot, pos: i})
				i++
			}
		case c == '<':
			if i+1 < n && input[i+1] == '=' {
				toks = append(toks, token{kind: tLe, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tLt, pos: i})
				i++
			}
		case c == '>':
			if i+1 < n && input[i+1] == '=' {
				toks = append(toks, token{kind: tGe, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tGt, pos: i})
				i++
			}
		case c == '=':
			if i+1 < n && input[i+1] == '=' {
				toks = append(toks, token{kind: tEq, pos: i})
				i += 2
			} else {
				return nil, fmt.Errorf("template: unexpected '=' at %d (did you mean '==')", i)
			}
		case c == '&':
			if i+1 < n && input[i+1] == '&' {
				toks = append(toks, token{kind: tAnd, pos: i})
				i += 2
			} else {
				return nil, fmt.Errorf("template: unexpected '&' at %d (did you mean '&&')", i)
			}
		case c == '|':
			if i+1 < n && input[i+1] == '|' {
				toks = append(toks, token{kind: tOr, pos: i})
				i += 2
			} else {
				return nil, fmt.Errorf("template: unexpected '|' at %d (did you mean '||')", i)
			}
		case c == '\'':
			tok, next, err := lexString(input, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = next
		case c >= '0' && c <= '9':
			tok, next, err := lexNumber(input, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = next
		case isIdentStart(c):
			tok, next := lexIdent(input, i)
			toks = append(toks, tok)
			i = next
		default:
			return nil, fmt.Errorf("template: unexpected character %q at %d", string(c), i)
		}
	}
	toks = append(toks, token{kind: tEOF, pos: n})
	return toks, nil
}

// lexString reads a single-quoted string. A literal single quote is written as
// ” (two single quotes), per GitHub Actions syntax.
func lexString(input string, start int) (token, int, error) {
	var sb strings.Builder
	i := start + 1 // skip opening quote
	n := len(input)
	for i < n {
		c := input[i]
		if c == '\'' {
			if i+1 < n && input[i+1] == '\'' { // escaped quote
				sb.WriteByte('\'')
				i += 2
				continue
			}
			return token{kind: tString, str: sb.String(), pos: start}, i + 1, nil
		}
		sb.WriteByte(c)
		i++
	}
	return token{}, 0, fmt.Errorf("template: unterminated string starting at %d", start)
}

// lexNumber reads a number: decimal, hex (0x...), or exponential. We rely on
// strconv via parseNumber for the actual value.
func lexNumber(input string, start int) (token, int, error) {
	i := start
	n := len(input)
	// hex
	if input[i] == '0' && i+1 < n && (input[i+1] == 'x' || input[i+1] == 'X') {
		i += 2
		for i < n && isHex(input[i]) {
			i++
		}
	} else {
		for i < n && (isDigit(input[i]) || input[i] == '.' || input[i] == 'e' || input[i] == 'E' ||
			((input[i] == '+' || input[i] == '-') && i > start && (input[i-1] == 'e' || input[i-1] == 'E'))) {
			i++
		}
	}
	raw := input[start:i]
	f, err := parseNumber(raw)
	if err != nil {
		return token{}, 0, fmt.Errorf("template: invalid number %q at %d", raw, start)
	}
	return token{kind: tNumber, num: f, text: raw, pos: start}, i, nil
}

// lexIdent reads an identifier or a keyword (true/false/null become typed
// tokens; everything else is tIdent, including function names).
func lexIdent(input string, start int) (token, int) {
	i := start
	n := len(input)
	for i < n && isIdentPart(input[i]) {
		i++
	}
	text := input[start:i]
	switch text {
	case "true":
		return token{kind: tBool, b: true, text: text, pos: start}, i
	case "false":
		return token{kind: tBool, b: false, text: text, pos: start}, i
	case "null":
		return token{kind: tNull, text: text, pos: start}, i
	default:
		return token{kind: tIdent, text: text, pos: start}, i
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '-' }
func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
