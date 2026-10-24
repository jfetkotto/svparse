package sema

import (
	"strconv"
	"strings"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/svint"
)

// maxBindDepth bounds how deep an expression is bound, so a hostile program
// cannot overflow the stack.
const maxBindDepth = 400

// Bind types the expression e as seen from scope s, resolving its names. The
// result is the expression's self-determined form (LRM 11.6): each operator's
// operands already carry the width and signedness the operator gives them.
// The diagnostics list undeclared names and illegal operand types. An
// expression that cannot be typed becomes a *BBad with type Invalid, which
// causes no further diagnostics.
func (m *Model) Bind(s *Scope, e ast.Expr) (BoundExpr, []diag.Diagnostic) {
	b := m.newBinder(s)
	r := b.bindSelf(e)
	if r == nil {
		r = &BBad{exprInfo{Invalid, e}}
	}
	return r, b.diags()
}

// BindAssign types e as the right side of an assignment to something of type
// target (LRM 10.7, 11.8.1): its operands are widened to the larger of the two
// sides before the operators apply, so "a + b" assigned to a wider variable
// does not overflow at its own width. It also reports whether the value can be
// assigned to target at all.
func (m *Model) BindAssign(s *Scope, target Type, e ast.Expr) (BoundExpr, []diag.Diagnostic) {
	b := m.newBinder(s)
	r := b.bindAssign(target, e)
	return r, b.diags()
}

func (b *binder) bindAssign(target Type, e ast.Expr) BoundExpr {
	inner := e
	for {
		p, ok := inner.(*ast.Paren)
		if !ok {
			break
		}
		inner = p.X
	}
	if pat, ok := inner.(*ast.AssignPattern); ok {
		return b.bindPattern(target, pat)
	}
	r := b.bindSelf(e)
	if r == nil {
		return &BBad{exprInfo{Invalid, e}}
	}
	rt := r.Type()
	if IsIntegral(target) && IsIntegral(rt) {
		tw, _ := Bits(target)
		rw, _ := Bits(rt)
		propagate(r, intT(max(tw, rw), IsSigned(rt), IsFourState(rt)))
	}
	if IsInvalid(target) || IsInvalid(rt) {
		return r
	}
	switch Assignment(target, rt) {
	case Incompatible:
		b.errorf(diag.CodeIncompatibleTypes, e.Pos(), "cannot assign %s to %s", rt, target)
	case CastRequired:
		b.errorf(diag.CodeCastRequired, e.Pos(), "assigning %s to %s needs a cast", rt, target)
	}
	return r
}

func bad(e ast.Expr) *BBad { return &BBad{exprInfo{Invalid, e}} }

// bindSelf binds an expression in its self-determined form.
func (b *binder) bindSelf(e ast.Expr) BoundExpr {
	if e == nil {
		return nil
	}
	if b.depth >= maxBindDepth {
		b.errorf(diag.CodeConstantLimit, e.Pos(), "expression nested more than %d deep", maxBindDepth)
		return bad(e)
	}
	b.depth++
	defer func() { b.depth-- }()

	switch e := e.(type) {
	case *ast.Literal:
		return b.bindLiteral(e)
	case *ast.Ident:
		return b.bindIdent(e)
	case *ast.Paren:
		return b.bindSelf(e.X)
	case *ast.ScopedName:
		return b.bindScoped(e)
	case *ast.ParameterizedName:
		if e.Scope == nil {
			return b.bindIdent(&ast.Ident{Name: e.Name})
		}
		return b.bindScoped(&ast.ScopedName{Scope: e.Scope, Name: e.Name})
	case *ast.MemberSelect:
		return b.bindMember(e)
	case *ast.Index:
		return b.bindIndex(e)
	case *ast.RangeSelect:
		return b.bindPartSelect(e)
	case *ast.Unary:
		return b.bindUnary(e)
	case *ast.Binary:
		return b.bindBinary(e)
	case *ast.Conditional:
		return b.bindCond(e)
	case *ast.Concat:
		return b.bindConcat(e)
	case *ast.Replicate:
		return b.bindReplicate(e)
	case *ast.Cast:
		return b.bindCast(e)
	case *ast.Call:
		return b.bindCall(e)
	case *ast.Inside:
		return b.bindInside(e)
	case *ast.TypeOrExpr:
		if b.scope.ClassifyName(e) == NameType {
			return &BTypeRef{exprInfo{Invalid, e}, b.typeFromName(e.X)}
		}
		return b.bindSelf(e.X)
	case *ast.TypeExpr:
		return &BTypeRef{exprInfo{Invalid, e}, b.resolveType(&e.Type)}
	case *ast.AssignExpr:
		lhs, rhs := b.bindSelf(e.LHS), b.bindSelf(e.RHS)
		if lhs == nil || rhs == nil {
			return bad(e)
		}
		return &BAssign{exprInfo{lhs.Type(), e}, e.Op, lhs, rhs}
	case *ast.IncDec:
		x := b.bindSelf(e.X)
		if x == nil {
			return bad(e)
		}
		return &BIncDec{exprInfo{x.Type(), e}, e.Op, e.Prefix, x}
	case *ast.MinTypMax:
		return b.bindSelf(e.Typ)
	}
	return bad(e)
}

func (b *binder) bindLiteral(e *ast.Literal) BoundExpr {
	switch e.Kind {
	case ast.LitInt, ast.LitUnbasedUnsized:
		v, err := svint.ParseLiteral(e.Text)
		if err != nil {
			b.errorf(diag.CodeInvalidType, e.Pos(), "%v", err)
			return bad(e)
		}
		var t Type
		switch {
		case e.Kind == ast.LitUnbasedUnsized:
			t = typeLogic
		case !strings.Contains(e.Text, "'"):
			t = intT(v.Width(), true, true)
			if v.Width() == 32 {
				t = typeInteger
			}
		default:
			t = intT(v.Width(), v.Signed(), true)
		}
		return &BLiteral{exprInfo: exprInfo{t, e}, Kind: e.Kind, Value: v}
	case ast.LitReal, ast.LitTime:
		num := strings.ReplaceAll(e.Text, "_", "")
		unit := ""
		if e.Kind == ast.LitTime {
			i := len(num)
			for i > 0 && (num[i-1] < '0' || num[i-1] > '9') && num[i-1] != '.' {
				i--
			}
			num, unit = num[:i], num[i:]
		}
		f, err := strconv.ParseFloat(num, 64)
		if err != nil {
			b.errorf(diag.CodeInvalidType, e.Pos(), "invalid real literal %q", e.Text)
			return bad(e)
		}
		t := typeReal
		if e.Kind == ast.LitTime {
			t = typeRealt
		}
		return &BLiteral{exprInfo: exprInfo{t, e}, Kind: e.Kind, Real: f, Str: unit}
	case ast.LitString:
		s := unquote(e.Text)
		v := svint.FromString(s)
		return &BLiteral{exprInfo: exprInfo{intT(v.Width(), false, false), e}, Kind: e.Kind, Value: v, Str: s}
	case ast.LitNull:
		return &BLiteral{exprInfo: exprInfo{Null, e}, Kind: e.Kind}
	case ast.LitUnbounded:
		return &BLiteral{exprInfo: exprInfo{typeInt, e}, Kind: e.Kind}
	}
	return bad(e)
}

// unquote strips the quotes of a string literal and decodes its escapes (LRM
// 5.9).
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			sb.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case '\\':
			sb.WriteByte('\\')
		case '"':
			sb.WriteByte('"')
		case 'v':
			sb.WriteByte('\v')
		case 'f':
			sb.WriteByte('\f')
		case 'a':
			sb.WriteByte('\a')
		case 'x':
			if i+2 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					sb.WriteByte(byte(n))
					i += 2
					continue
				}
			}
			sb.WriteByte('x')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			n, _ := strconv.ParseUint(s[i:j], 8, 16)
			sb.WriteByte(byte(n))
			i = j - 1
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// isValueKind reports whether symbols of the kind can appear as operands.
func isValueKind(k SymKind) bool {
	switch k {
	case SymVariable, SymNet, SymGenvar, SymParameter, SymPort, SymArg, SymEnumMember, SymLoopVar, SymClockingSignal:
		return true
	}
	return false
}

func (b *binder) bindIdent(e *ast.Ident) BoundExpr {
	if strings.HasPrefix(e.Name, "$") {
		return b.systemCall(e, e.Name, nil)
	}
	res := b.scope.Lookup(e.Name)
	if res.Symbol == nil {
		if len(res.Ambiguous) > 0 {
			b.errorf(diag.CodeUndeclared, e.Pos(), "%q is ambiguous: imported from several packages", e.Name)
		} else if _, root := b.m.comp.Lookup(e.Name); !root {
			// A design element's own name is the root of hierarchical names
			// (LRM 23.7), as in $dumpports(top, ...): not a variable, but not
			// undeclared either.
			b.errorf(diag.CodeUndeclared, e.Pos(), "%q is not declared", e.Name)
		}
		return bad(e)
	}
	return b.nameOf(e, res.Symbol)
}

// nameOf turns a resolved symbol into an operand.
func (b *binder) nameOf(e ast.Expr, sym *Symbol) BoundExpr {
	switch {
	case isValueKind(sym.Kind):
		return &BName{exprInfo{b.typeOfSymbol(sym), e}, sym}
	case sym.Kind.IsType():
		var t Type = Invalid
		b.withScope(sym.Scope, func() {
			switch sym.Kind {
			case SymTypedef:
				t = b.typedefType(sym)
			case SymClass:
				t = b.classType(sym)
			case SymTypeParam:
				t = b.typeParamType(sym)
			}
		})
		return &BTypeRef{exprInfo{Invalid, e}, t}
	case sym.Kind == SymFunction:
		return &BCall{exprInfo: exprInfo{b.returnType(sym), e}, Name: sym.Name, Sym: sym}
	}
	return &BName{exprInfo{Invalid, e}, sym}
}

func (b *binder) bindScoped(e *ast.ScopedName) BoundExpr {
	path, ok := scopedPath(e)
	if !ok {
		return bad(e)
	}
	res := b.scope.LookupQualified(path)
	if res.Symbol == nil && builtinScope(path) {
		return bad(e)
	}
	if res.Symbol == nil {
		b.errorf(diag.CodeUndeclared, e.Pos(), "%q is not declared", strings.Join(path, "::"))
		return bad(e)
	}
	return b.nameOf(e, res.Symbol)
}

// returnType resolves a function's declared return type.
func (b *binder) returnType(sym *Symbol) Type {
	fn, ok := sym.Decl.(*ast.Function)
	if !ok {
		return Invalid
	}
	var t Type = Invalid
	b.withScope(sym.Scope, func() { t = b.resolveType(&fn.ReturnType) })
	if fn.ReturnType.Name == "" {
		return typeLogic // an implicit return type is a 1-bit logic (LRM 13.4.1)
	}
	return t
}

// dottedPath returns the names of a chain of member selects over an identifier.
func dottedPath(e ast.Expr) ([]string, bool) {
	switch e := e.(type) {
	case *ast.Ident:
		return []string{e.Name}, true
	case *ast.MemberSelect:
		p, ok := dottedPath(e.X)
		if !ok {
			return nil, false
		}
		return append(p, e.Member), true
	}
	return nil, false
}

func (b *binder) bindMember(e *ast.MemberSelect) BoundExpr {
	if path, ok := dottedPath(e); ok {
		if first := b.scope.Lookup(path[0]).Symbol; first == nil || !isValueKind(first.Kind) && !first.Kind.IsType() {
			// A hierarchical name through instances and blocks.
			if sym, rest := b.scope.LookupPath(path); sym != nil {
				var x BoundExpr = b.nameOf(e, sym)
				for _, f := range rest {
					x = b.memberOf(e, x, f)
				}
				return x
			}
			if first == nil {
				b.errorf(diag.CodeUndeclared, e.Pos(), "%q is not declared", strings.Join(path, "."))
				return bad(e)
			}
		}
	}
	x := b.bindSelf(e.X)
	if x == nil {
		return bad(e)
	}
	return b.memberOf(e, x, e.Member)
}

// memberOf selects field name of x by x's type.
func (b *binder) memberOf(e ast.Expr, x BoundExpr, name string) BoundExpr {
	if IsInvalid(x.Type()) {
		return &BMember{exprInfo{Invalid, e}, x, name, nil}
	}
	switch t := Resolve(x.Type()).(type) {
	case *Struct:
		return b.fieldOf(e, x, name, t.Members)
	case *Union:
		return b.fieldOf(e, x, name, t.Members)
	case *Class:
		if cs := b.cache().classes[t]; cs != nil {
			sym, ok := cs.syms[name]
			if !ok {
				sym, ok = b.m.inheritedMember(cs, name, 0)
			}
			if ok {
				ty := b.typeOfSymbol(sym)
				if sym.Kind == SymFunction {
					ty = b.returnType(sym)
				}
				return &BMember{exprInfo{ty, e}, x, name, sym}
			}
		}
		if builtinClasses[t.Name] == t {
			// The members of the built-in std classes are not modelled.
			return &BMember{exprInfo{Invalid, e}, x, name, nil}
		}
		b.errorf(diag.CodeUndeclared, e.Pos(), "class %s has no member %q", t.Name, name)
		return &BMember{exprInfo{Invalid, e}, x, name, nil}
	case *VirtualInterface:
		if def, ok := b.m.comp.Lookup(t.Interface); ok {
			if sc, ok := b.m.defs[def]; ok {
				if sym, ok := sc.syms[name]; ok {
					return &BMember{exprInfo{b.typeOfSymbol(sym), e}, x, name, sym}
				}
			}
		}
		return &BMember{exprInfo{Invalid, e}, x, name, nil}
	case *Enum:
		if name == "name" {
			return &BMember{exprInfo{String, e}, x, name, nil}
		}
	}
	return &BMember{exprInfo{Invalid, e}, x, name, nil}
}

func (b *binder) fieldOf(e ast.Expr, x BoundExpr, name string, members []Member) BoundExpr {
	for _, m := range members {
		if m.Name == name {
			return &BMember{exprInfo{m.Type, e}, x, name, nil}
		}
	}
	b.errorf(diag.CodeUndeclared, e.Pos(), "%s has no member %q", x.Type(), name)
	return &BMember{exprInfo{Invalid, e}, x, name, nil}
}

func (b *binder) bindIndex(e *ast.Index) BoundExpr {
	x, idx := b.bindSelf(e.X), b.bindSelf(e.Index)
	if x == nil || idx == nil {
		return bad(e)
	}
	var t Type = Invalid
	b.checkVectored(e.X, e.Pos())
	if IsReal(x.Type()) {
		b.errorf(diag.CodeInvalidExpression, e.Pos(), "a real value cannot be indexed (LRM 6.12)")
	} else if _, assoc := Resolve(x.Type()).(*AssocArray); !assoc && IsReal(idx.Type()) {
		b.errorf(diag.CodeInvalidExpression, e.Index.Pos(), "an index cannot be real (LRM 11.5.1)")
	}
	switch xt := Resolve(x.Type()).(type) {
	case *PackedArray:
		t = xt.Elem
	case *UnpackedArray:
		t = xt.Elem
	case *DynamicArray:
		t = xt.Elem
	case *Queue:
		t = xt.Elem
	case *AssocArray:
		t = xt.Elem
	case *Integral, *Enum, *Struct, *Union:
		if IsIntegral(xt) {
			t = intT(1, false, IsFourState(xt))
		}
	default:
		if xt == String {
			t = NewIntegral(BaseByte)
		}
	}
	return &BIndex{exprInfo{t, e}, x, idx}
}

// checkVectored reports a bit-select or part-select of a net declared
// "vectored", which hides its bits (LRM 6.9.2).
func (b *binder) checkVectored(x ast.Expr, at ast.Position) {
	id, ok := x.(*ast.Ident)
	if !ok {
		return
	}
	if sym := b.scope.Lookup(id.Name).Symbol; sym != nil {
		if v, ok := sym.Decl.(*ast.Variable); ok && v.NetQualifier == "vectored" {
			b.errorf(diag.CodeInvalidExpression, at, "%q is a vectored net and cannot be bit-selected or part-selected (LRM 6.9.2)", id.Name)
		}
	}
}

func (b *binder) bindPartSelect(e *ast.RangeSelect) BoundExpr {
	x, l, r := b.bindSelf(e.X), b.bindSelf(e.Left), b.bindSelf(e.Right)
	if x == nil || l == nil || r == nil {
		return bad(e)
	}
	var t Type = Invalid
	b.checkVectored(e.X, e.Pos())
	if IsReal(x.Type()) {
		b.errorf(diag.CodeInvalidExpression, e.Pos(), "a real value cannot be part-selected (LRM 6.12)")
	}
	// w is the number of elements selected: bits of a vector, elements of an
	// array (LRM 11.5.1, 7.4.3).
	w := 0
	switch e.Kind {
	case ast.RangeColon:
		lv, ok1 := b.constInt(e.Left)
		rv, ok2 := b.constInt(e.Right)
		if ok1 && ok2 {
			w = int(abs64(lv-rv)) + 1
		}
	default:
		if n, ok := b.constInt(e.Right); ok && n > 0 {
			w = int(n)
		}
	}
	if w > 0 && w <= maxElems {
		switch xt := Resolve(x.Type()).(type) {
		case *PackedArray:
			// A slice of "logic [N-1:0][7:0]" is still an array of bytes, so
			// its width is the element count times the element width.
			t = &PackedArray{Elem: xt.Elem, Range: Range{Left: w - 1, Right: 0}}
		case *UnpackedArray:
			t = &UnpackedArray{Elem: xt.Elem, Range: Range{Left: w - 1, Right: 0}}
		default:
			if IsIntegral(x.Type()) {
				t = intT(w, false, IsFourState(x.Type()))
			}
		}
	}
	return &BPartSelect{exprInfo{t, e}, x, e.Kind, l, r}
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func (b *binder) bindUnary(e *ast.Unary) BoundExpr {
	x := b.bindSelf(e.X)
	if x == nil {
		return bad(e)
	}
	xt := x.Type()
	var t Type = Invalid
	switch e.Op {
	case "+", "-":
		if IsNumeric(xt) {
			t = xt
		}
	case "~":
		if IsIntegral(xt) {
			t = xt
		}
	case "!":
		t = intT(1, false, IsFourState(xt))
	case "&", "|", "^", "~&", "~|", "~^", "^~":
		if IsIntegral(xt) {
			t = intT(1, false, IsFourState(xt))
		}
	}
	if IsInvalid(t) && !IsInvalid(xt) {
		b.errorf(diag.CodeIncompatibleTypes, e.Pos(), "operator %s does not apply to %s", e.Op, xt)
	}
	return &BUnary{exprInfo{t, e}, e.Op, x}
}

var (
	arithOps = map[string]bool{"+": true, "-": true, "*": true, "/": true, "%": true, "&": true, "|": true, "^": true, "~^": true, "^~": true}
	shiftOps = map[string]bool{"<<": true, ">>": true, "<<<": true, ">>>": true, "**": true}
	relOps   = map[string]bool{"<": true, "<=": true, ">": true, ">=": true, "==": true, "!=": true, "===": true, "!==": true, "==?": true, "!=?": true}
	logicOps = map[string]bool{"&&": true, "||": true, "->": true, "<->": true}
)

// unify is the type two operands are brought to (LRM 11.8.1): real if either
// is real, else integral with the larger width, signed only if both are.
func unify(a, b Type) Type {
	a, b = Resolve(a), Resolve(b)
	if IsInvalid(a) || IsInvalid(b) {
		return Invalid
	}
	if IsReal(a) || IsReal(b) {
		if IsNumeric(a) && IsNumeric(b) {
			ra, okA := a.(*Real)
			rb, okB := b.(*Real)
			if okA && okB && ra.Kind == KindShortreal && rb.Kind == KindShortreal {
				return typeShortr
			}
			return typeReal
		}
		return Invalid
	}
	if IsIntegral(a) && IsIntegral(b) {
		wa, _ := Bits(a)
		wb, _ := Bits(b)
		return intT(max(wa, wb), IsSigned(a) && IsSigned(b), IsFourState(a) || IsFourState(b))
	}
	if a == String && b == String {
		return String
	}
	return Invalid
}

func (b *binder) bindBinary(e *ast.Binary) BoundExpr {
	x, y := b.bindSelf(e.X), b.bindSelf(e.Y)
	if x == nil || y == nil {
		return bad(e)
	}
	n := &BBinary{exprInfo: exprInfo{Invalid, e}, Op: e.Op, X: x, Y: y, OperandType: Invalid}
	xt, yt := x.Type(), y.Type()
	switch {
	case arithOps[e.Op]:
		u := unify(xt, yt)
		if u == String {
			u = Invalid // strings have no arithmetic
		}
		n.typ, n.OperandType = u, u
		propagate(x, u)
		propagate(y, u)
	case relOps[e.Op]:
		u := unify(xt, yt)
		n.OperandType = u
		four := IsFourState(xt) || IsFourState(yt)
		if e.Op == "===" || e.Op == "!==" {
			four = false
		}
		n.typ = intT(1, false, four)
		if IsInvalid(u) && (e.Op == "==" || e.Op == "!=" || e.Op == "===" || e.Op == "!==") && comparableOperands(xt, yt) {
			u = xt // handles, strings, enums of one type: compared as they are
			n.OperandType = u
		}
		propagate(x, u)
		propagate(y, u)
		if IsInvalid(u) && !IsInvalid(xt) && !IsInvalid(yt) {
			b.errorf(diag.CodeIncompatibleTypes, e.Pos(), "cannot compare %s with %s", xt, yt)
		}
		return n
	case shiftOps[e.Op]:
		if IsNumeric(xt) && IsNumeric(yt) {
			n.typ = xt
			n.OperandType = xt
		}
	case logicOps[e.Op]:
		n.typ = intT(1, false, IsFourState(xt) || IsFourState(yt))
		return n
	default:
		return n
	}
	if IsInvalid(n.typ) && !IsInvalid(xt) && !IsInvalid(yt) {
		b.errorf(diag.CodeIncompatibleTypes, e.Pos(), "operator %s does not apply to %s and %s", e.Op, xt, yt)
	}
	return n
}

// comparableOperands reports whether two non-numeric operands can be compared
// with == because they have the same type: class handles, strings, events.
func comparableOperands(a, b Type) bool {
	a, b = Resolve(a), Resolve(b)
	if a == Null || b == Null {
		return true
	}
	return Matches(a, b) || (a == String && b == String)
}

func (b *binder) bindCond(e *ast.Conditional) BoundExpr {
	c, t, f := b.bindSelf(e.Cond), b.bindSelf(e.Then), b.bindSelf(e.Else)
	if c == nil || t == nil || f == nil {
		return bad(e)
	}
	u := unify(t.Type(), f.Type())
	if _, ok := Resolve(t.Type()).(*Enum); ok && Matches(t.Type(), f.Type()) {
		u = t.Type() // both branches are the same enum: the result is that enum (LRM 6.19.3)
	}
	if IsInvalid(u) && comparableOperands(t.Type(), f.Type()) {
		u = t.Type()
		if Resolve(u) == Null {
			u = f.Type()
		}
	}
	if IsInvalid(u) && !IsInvalid(t.Type()) && !IsInvalid(f.Type()) {
		b.errorf(diag.CodeIncompatibleTypes, e.Pos(), "the branches of ?: have types %s and %s", t.Type(), f.Type())
	}
	propagate(t, u)
	propagate(f, u)
	return &BCond{exprInfo{u, e}, c, t, f}
}

func (b *binder) bindList(list []ast.Expr) ([]BoundExpr, bool) {
	out := make([]BoundExpr, 0, len(list))
	ok := true
	for _, x := range list {
		be := b.bindSelf(x)
		if be == nil {
			ok = false
			continue
		}
		out = append(out, be)
	}
	return out, ok
}

// concatType is the type of a concatenation of the given operands: a string if
// any is one, else an unsigned vector as wide as they are together.
func concatType(elems []BoundExpr) Type {
	total, four := 0, false
	for _, el := range elems {
		t := Resolve(el.Type())
		if t == String {
			return String
		}
		w, ok := Bits(t)
		if !ok || !IsIntegral(t) {
			return Invalid
		}
		total += w
		four = four || IsFourState(t)
		if total > maxElems {
			return Invalid
		}
	}
	if total == 0 {
		return Invalid
	}
	return intT(total, false, four)
}

func (b *binder) bindConcat(e *ast.Concat) BoundExpr {
	elems, _ := b.bindList(e.Elems)
	return &BConcat{exprInfo{concatType(elems), e}, elems}
}

func (b *binder) bindReplicate(e *ast.Replicate) BoundExpr {
	elems, _ := b.bindList(e.Elems)
	count := b.bindSelf(e.Count)
	n := &BReplicate{exprInfo{Invalid, e}, count, elems}
	cnt, ok := b.constInt(e.Count)
	if !ok || cnt < 0 {
		return n
	}
	one := concatType(elems)
	if w, ok := Bits(one); ok && cnt >= 1 && cnt <= int64(maxElems/w) && one != String {
		n.typ = intT(int(cnt)*w, false, IsFourState(one))
	}
	return n
}

func (b *binder) bindCast(e *ast.Cast) BoundExpr {
	x := b.bindSelf(e.X)
	if x == nil {
		return bad(e)
	}
	n := &BCast{exprInfo{Invalid, e}, CastType, x}
	switch t := e.Type.(type) {
	case *ast.Literal:
		n.Kind = CastSize
		if w, ok := b.constInt(t); ok && w >= 1 && w <= maxElems && IsIntegral(x.Type()) {
			n.typ = intT(int(w), IsSigned(x.Type()), IsFourState(x.Type()))
		}
		return n
	case *ast.Paren, *ast.Binary, *ast.Ident, *ast.ScopedName:
		if id, ok := t.(*ast.Ident); ok && (id.Name == "signed" || id.Name == "unsigned") {
			return b.signCast(n, id.Name == "signed")
		}
		if b.scope.ClassifyName(t) == NameType {
			n.typ = b.typeFromName(t)
		} else if w, ok := b.constInt(t); ok && w >= 1 && w <= maxElems && IsIntegral(x.Type()) {
			n.Kind = CastSize // "N'(x)" with a constant expression N
			n.typ = intT(int(w), IsSigned(x.Type()), IsFourState(x.Type()))
			return n
		}
	case *ast.TypeExpr:
		if t.Type.Name == "" && len(t.Type.PackedDims) == 0 && (t.Type.Signed || t.Type.Unsigned) {
			return b.signCast(n, t.Type.Signed)
		}
		n.typ = b.resolveType(&t.Type)
	case *ast.TypeOrExpr:
		n.typ = b.typeFromName(t.X)
	default:
		return n
	}
	if !IsInvalid(n.typ) && !IsInvalid(x.Type()) && !Castable(n.typ, x.Type()) {
		b.errorf(diag.CodeIncompatibleTypes, e.Pos(), "cannot cast %s to %s", x.Type(), n.typ)
	}
	return n
}

func (b *binder) signCast(n *BCast, signed bool) BoundExpr {
	n.Kind = CastSign
	if xt := n.X.Type(); IsIntegral(xt) {
		w, _ := Bits(xt)
		n.typ = intT(w, signed, IsFourState(xt))
	}
	return n
}

func (b *binder) bindInside(e *ast.Inside) BoundExpr {
	x := b.bindSelf(e.X)
	if x == nil {
		return bad(e)
	}
	n := &BInside{exprInfo: exprInfo{intT(1, false, true), e}, X: x}
	for _, r := range e.Ranges {
		if vr, ok := r.(*ast.ValueRange); ok {
			lo, hi := b.bindSelf(vr.Lo), b.bindSelf(vr.Hi)
			if lo != nil && hi != nil {
				n.Ranges = append(n.Ranges, &BRange{exprInfo{Invalid, r}, lo, hi})
			}
			continue
		}
		if be := b.bindSelf(r); be != nil {
			n.Ranges = append(n.Ranges, be)
		}
	}
	return n
}

// ---- width and sign propagation (LRM 11.8) ----

// propagate applies a context type t to a context-determined operand: the
// operators that take their type from the surrounding expression pass it on to
// their own context-determined operands. Everything else (names, literals of
// fixed size, selects, concatenations, calls, comparisons) keeps its own type
// and is converted at the use. Real contexts do not change integral operands.
func propagate(n BoundExpr, t Type) {
	if IsInvalid(t) || !IsIntegral(t) {
		return
	}
	setType := func(info *exprInfo) bool {
		if IsInvalid(info.typ) || !IsIntegral(info.typ) {
			return false
		}
		info.typ = intT(widthOf(t), IsSigned(t), IsFourState(info.typ) || IsFourState(t))
		return true
	}
	switch n := n.(type) {
	case *BLiteral:
		if n.Kind == ast.LitUnbasedUnsized {
			setType(&n.exprInfo)
		}
	case *BUnary:
		if n.Op == "+" || n.Op == "-" || n.Op == "~" {
			if setType(&n.exprInfo) {
				propagate(n.X, n.typ)
			}
		}
	case *BBinary:
		switch {
		case arithOps[n.Op]:
			if setType(&n.exprInfo) {
				n.OperandType = n.typ
				propagate(n.X, n.typ)
				propagate(n.Y, n.typ)
			}
		case shiftOps[n.Op]:
			if setType(&n.exprInfo) {
				n.OperandType = n.typ
				propagate(n.X, n.typ)
			}
		}
	case *BCond:
		if setType(&n.exprInfo) {
			propagate(n.Then, n.typ)
			propagate(n.Else, n.typ)
		}
	}
}

func widthOf(t Type) int { w, _ := Bits(t); return w }

// bindPattern binds an assignment pattern against the type it is assigned to
// (LRM 10.9.1): the items of an unpacked array or the members of an unpacked
// struct, matched up by position, by member name or by "default".
func (b *binder) bindPattern(target Type, p *ast.AssignPattern) BoundExpr {
	t := Resolve(target)
	if p.Type != nil {
		t = Resolve(b.typeFromName(p.Type))
	}
	n := &BAssignPattern{exprInfo: exprInfo{Invalid, p}}
	fail := func(format string, args ...any) BoundExpr {
		if !IsInvalid(t) {
			b.errorf(diag.CodeIncompatibleTypes, p.Pos(), format, args...)
		}
		return n
	}
	var positional []ast.Expr
	var defaultValue ast.Expr
	named := map[string]ast.Expr{}
	for _, it := range p.Items {
		switch it := it.(type) {
		case *ast.PatternItem:
			switch {
			case it.IsDefault:
				defaultValue = it.Value
			default:
				id, ok := it.Key.(*ast.Ident)
				if !ok {
					return fail("only a member name or default can key an assignment pattern here")
				}
				named[id.Name] = it.Value
			}
		case *ast.Replicate:
			count, ok := b.constInt(it.Count)
			if !ok || count < 0 || count > maxElems {
				return n
			}
			for k := int64(0); k < count; k++ {
				positional = append(positional, it.Elems...)
				if len(positional) > maxElems {
					return n
				}
			}
		default:
			positional = append(positional, it)
		}
	}

	var elemTypes []Type
	var names []string
	switch t := t.(type) {
	case *UnpackedArray:
		for i := 0; i < t.Range.Size(); i++ {
			elemTypes = append(elemTypes, t.Elem)
		}
	case *DynamicArray:
		for range positional {
			elemTypes = append(elemTypes, t.Elem)
		}
	case *Queue:
		for range positional {
			elemTypes = append(elemTypes, t.Elem)
		}
	case *Struct:
		for _, m := range t.Members {
			elemTypes = append(elemTypes, m.Type)
			names = append(names, m.Name)
		}
	default:
		return fail("an assignment pattern needs an unpacked array or struct, not %s", t)
	}
	if len(positional) > len(elemTypes) {
		return fail("the pattern has %d items for %d elements", len(positional), len(elemTypes))
	}
	if len(named) > 0 && len(names) == 0 {
		return fail("a named pattern item needs a struct")
	}
	for i, et := range elemTypes {
		var value ast.Expr
		switch {
		case i < len(positional):
			value = positional[i]
		case names != nil && named[names[i]] != nil:
			value = named[names[i]]
			delete(named, names[i])
		case defaultValue != nil:
			value = defaultValue
		default:
			return fail("the pattern gives no value for element %d", i)
		}
		item := b.bindAssign(et, value)
		n.Items = append(n.Items, item)
		n.ElemTypes = append(n.ElemTypes, et)
	}
	for name := range named {
		return fail("the struct has no member %q", name)
	}
	n.typ = t
	return n
}
