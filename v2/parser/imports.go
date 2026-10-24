package parser

import (
	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/token"
)

// parseImport parses "import pkg::name;" / "import pkg::*;", which can
// name several imports at once, comma-separated ("import pkg1::name1,
// pkg2::*;").
func (p *parser) parseImport() ([]ast.Decl, bool) {
	p.advance() // "import"

	var decls []ast.Decl
	for {
		imp, ok := p.parseImportItem()
		if ok {
			decls = append(decls, imp)
		}
		if p.peek().Kind != token.KindComma {
			break
		}
		p.advance()
	}

	if p.peek().Kind == token.KindSemi {
		p.advance()
	} else {
		p.errorf(p.peek(), "expected ';' after import")
	}
	if len(decls) == 0 {
		return nil, false
	}
	return decls, true
}

func (p *parser) parseImportItem() (*ast.Import, bool) {
	pkgTok, ok := p.expectIdent()
	if !ok {
		return nil, false
	}
	imp := &ast.Import{Package: pkgTok.Text}
	imp.Position = namePosition(pkgTok)

	if p.peek().Kind != token.KindColonColon {
		p.errorf(p.peek(), "expected '::' after package name in import")
		return imp, true
	}
	p.advance() // "::"
	if p.peek().Kind == token.KindMul {
		p.advance()
		imp.Member = "*"
	} else if memberTok, ok := p.expectIdent(); ok {
		imp.Member = memberTok.Text
	}
	return imp, true
}
