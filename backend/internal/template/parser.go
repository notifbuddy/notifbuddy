package template

import "fmt"

// AST node types. Every node evaluates to an `any` value (nil, bool, float64,
// string, []any, map[string]any) against a context.
type node interface {
	eval(ctx map[string]any) (any, error)
}

type litNode struct{ v any }         // null/bool/number/string literal
type identNode struct{ name string } // top-level name (linear, github, ...)
type memberNode struct {             // base.prop
	base node
	prop string
}
type indexNode struct { // base[idx]
	base node
	idx  node
}
type filterNode struct{ base node } // base.*  (object-filter)
type notNode struct{ x node }       // !x
type binNode struct {               // x <op> y
	op   tokenKind
	l, r node
}
type callNode struct { // fn(args...)
	name string
	args []node
}

// parser is a recursive-descent parser over the token slice.
type parser struct {
	toks []token
	pos  int
}

// parse turns an expression string into an AST.
func parse(expr string) (node, error) {
	toks, err := lex(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("template: unexpected trailing token %q at %d", p.peek().text, p.peek().pos)
	}
	return n, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) accept(k tokenKind) bool {
	if p.peek().kind == k {
		p.pos++
		return true
	}
	return false
}

// Precedence (low → high): || , && , ==/!= , </<=/>/>= , unary ! , postfix .[]() ,
// primary. parseExpr is the lowest precedence (||).
func (p *parser) parseExpr() (node, error) { return p.parseOr() }

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &binNode{op: tOr, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		p.next()
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = &binNode{op: tAnd, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseEquality() (node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tEq || p.peek().kind == tNe {
		op := p.next().kind
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &binNode{op: op, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseComparison() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for k := p.peek().kind; k == tLt || k == tLe || k == tGt || k == tGe; k = p.peek().kind {
		op := p.next().kind
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &binNode{op: op, l: left, r: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().kind == tNot {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &notNode{x: x}, nil
	}
	return p.parsePostfix()
}

// parsePostfix handles the chained access operators: .prop, .* , [expr], and
// function-call parens on a bare identifier.
func (p *parser) parsePostfix() (node, error) {
	n, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case tDot:
			p.next()
			if p.peek().kind == tStar {
				p.next()
				n = &filterNode{base: n}
				continue
			}
			t := p.next()
			if t.kind != tIdent && t.kind != tBool && t.kind != tNull {
				return nil, fmt.Errorf("template: expected property name after '.' at %d", t.pos)
			}
			n = &memberNode{base: n, prop: t.text}
		case tLBracket:
			p.next()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if !p.accept(tRBracket) {
				return nil, fmt.Errorf("template: expected ']' at %d", p.peek().pos)
			}
			n = &indexNode{base: n, idx: idx}
		default:
			return n, nil
		}
	}
}

func (p *parser) parsePrimary() (node, error) {
	t := p.peek()
	switch t.kind {
	case tNumber:
		p.next()
		return &litNode{v: t.num}, nil
	case tString:
		p.next()
		return &litNode{v: t.str}, nil
	case tBool:
		p.next()
		return &litNode{v: t.b}, nil
	case tNull:
		p.next()
		return &litNode{v: nil}, nil
	case tLParen:
		p.next()
		n, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.accept(tRParen) {
			return nil, fmt.Errorf("template: expected ')' at %d", p.peek().pos)
		}
		return n, nil
	case tIdent:
		p.next()
		// Function call if followed by '('.
		if p.peek().kind == tLParen {
			return p.parseCall(t.text)
		}
		return &identNode{name: t.text}, nil
	default:
		return nil, fmt.Errorf("template: unexpected token %q at %d", t.text, t.pos)
	}
}

func (p *parser) parseCall(name string) (node, error) {
	p.next() // consume '('
	var args []node
	if p.peek().kind != tRParen {
		for {
			a, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
			if p.accept(tComma) {
				continue
			}
			break
		}
	}
	if !p.accept(tRParen) {
		return nil, fmt.Errorf("template: expected ')' to close %s() at %d", name, p.peek().pos)
	}
	return &callNode{name: name, args: args}, nil
}
