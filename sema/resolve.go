package sema

import (
	"fmt"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/svint"
)

// posKey identifies an anonymous aggregate by where it is written. "struct {int
// x;} a, b;" gives each declarator its own copy of the aggregate at the same
// position, and they must resolve to one type.
type posKey struct {
	kind string
	pos  ast.Position
}

// typeCache is the part of the Model that remembers resolved types, so a
// typedef or class is one Type however many times it is mentioned.
type typeCache struct {
	byNode   map[ast.Node]Type
	byPos    map[posKey]Type
	classes  map[*Class]*Scope
	symTypes map[*Symbol]Type
	busy     map[any]bool
}

func (m *Model) cache() *typeCache {
	if m.tc == nil {
		m.tc = &typeCache{
			byNode:   map[ast.Node]Type{},
			byPos:    map[posKey]Type{},
			classes:  map[*Class]*Scope{},
			symTypes: map[*Symbol]Type{},
			busy:     map[any]bool{},
		}
	}
	return m.tc
}

// binder carries the state of one resolve or bind call: the scope names are
// looked up in and the diagnostics found.
type binder struct {
	m     *Model
	scope *Scope
	set   diag.Set
	env   *Env
	depth int
}

func (m *Model) newBinder(s *Scope) *binder { return &binder{m: m, scope: s} }

func (b *binder) errorf(code string, at ast.Position, format string, args ...any) {
	b.set.Add(diag.Diagnostic{Code: code, Severity: diag.Error, Range: at, Message: fmt.Sprintf(format, args...)})
}

func (b *binder) diags() []diag.Diagnostic { return b.set.Sorted() }

// withScope runs f with name lookup switched to s.
func (b *binder) withScope(s *Scope, f func()) {
	saved := b.scope
	b.scope = s
	defer func() { b.scope = saved }()
	f()
}

// ResolveType turns a type as written in source into a Type, looking names up
// from scope s. The second result lists the problems: undeclared type names
// and packed dimensions that are not constant.
func (m *Model) ResolveType(s *Scope, t *ast.Type) (Type, []diag.Diagnostic) {
	b := m.newBinder(s)
	r := b.resolveType(t)
	return r, b.diags()
}

// TypeOf returns the declared type of a value symbol: a variable, net, port,
// argument, parameter, enum constant, genvar or loop variable. It is Invalid
// for symbols that are not values.
func (m *Model) TypeOf(sym *Symbol) (Type, []diag.Diagnostic) {
	b := m.newBinder(sym.Scope)
	r := b.typeOfSymbol(sym)
	return r, b.diags()
}

func intT(width int, signed, four bool) Type {
	base := BaseBit
	if four {
		base = BaseLogic
	}
	if width <= 1 {
		return &Integral{Base: base, Signed: signed}
	}
	t := Vector(base, signed, Range{width - 1, 0})
	return t
}

var (
	typeInt     Type = NewIntegral(BaseInt)
	typeInteger Type = NewIntegral(BaseInteger)
	typeLogic   Type = NewIntegral(BaseLogic)
	typeBit     Type = NewIntegral(BaseBit)
	typeReal    Type = &Real{KindReal}
	typeRealt   Type = &Real{KindRealtime}
	typeShortr  Type = &Real{KindShortreal}
	typeTime    Type = NewIntegral(BaseTime)
)

// resolveType implements ResolveType.
func (b *binder) resolveType(t *ast.Type) Type {
	if t == nil {
		return Invalid
	}
	base := b.resolveBase(t)
	if IsInvalid(base) || len(t.PackedDims) == 0 {
		return base
	}
	return b.applyPackedDims(base, t)
}

func (b *binder) resolveBase(t *ast.Type) Type {
	switch t.Name {
	case "":
		// An implicit type, "[7:0] x" or "signed x": logic.
		return b.integral(BaseLogic, t)
	case "bit", "logic", "reg", "byte", "shortint", "int", "longint", "integer", "time":
		base, _ := ParseBase(t.Name)
		return b.integral(base, t)
	case "wire", "uwire", "tri", "triand", "trior", "tri0", "tri1", "wand", "wor", "trireg", "supply0", "supply1", "interconnect":
		return b.integral(BaseLogic, t)
	case "genvar":
		return typeInt
	case "real":
		return typeReal
	case "shortreal":
		return typeShortr
	case "realtime":
		return typeRealt
	case "string":
		return String
	case "chandle":
		return Chandle
	case "event":
		return Event
	case "void":
		return Void
	case "type":
		if t.TypeRef != nil {
			if be := b.bindSelf(t.TypeRef.X); be != nil {
				return be.Type()
			}
		}
		return Invalid
	case "struct", "union", "enum":
		return b.resolveInline(t)
	}
	if t.Virtual || t.Modport != "" {
		return &VirtualInterface{Interface: t.Name, Modport: t.Modport}
	}
	return b.resolveNamed(t)
}

// integral builds a built-in integral type. A bit, logic or reg takes its range
// from the first packed dimension here; the dimensions are applied by the
// caller for every other base.
func (b *binder) integral(base Base, t *ast.Type) Type {
	signed := base.defaultSigned()
	if t.Signed {
		signed = true
	}
	if t.Unsigned {
		signed = false
	}
	if !base.vector() || len(t.PackedDims) == 0 {
		return &Integral{Base: base, Signed: signed}
	}
	return &Integral{Base: base, Signed: signed} // dims handled by applyPackedDims
}

func (b *binder) applyPackedDims(base Type, t *ast.Type) Type {
	ranges := make([]Range, 0, len(t.PackedDims))
	for i := range t.PackedDims {
		r, ok := b.dimRange(&t.PackedDims[i], t.Position)
		if !ok {
			return Invalid
		}
		ranges = append(ranges, r)
	}
	// logic [3:0][7:0]: the last dimension is the vector, the earlier ones
	// wrap it as packed arrays, outermost first.
	cur := base
	rest := ranges
	if in, ok := base.(*Integral); ok && in.Base.vector() && !in.HasRange {
		last := ranges[len(ranges)-1]
		cur = Vector(in.Base, in.Signed, last)
		rest = ranges[:len(ranges)-1]
	}
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i].Size() > maxElems {
			return Invalid
		}
		cur = &PackedArray{Elem: cur, Range: rest[i]}
	}
	return cur
}

// dimRange evaluates a packed dimension [l:r] or [n] to a Range.
func (b *binder) dimRange(d *ast.Dim, at ast.Position) (Range, bool) {
	if d.LeftExpr == nil {
		b.errorf(diag.CodeInvalidType, at, "packed dimension has no bounds")
		return Range{}, false
	}
	l, ok := b.constInt(d.LeftExpr)
	if !ok {
		return Range{}, false
	}
	if d.RightExpr == nil {
		if l < 1 || l > maxElems {
			b.errorf(diag.CodeInvalidType, d.LeftExpr.Pos(), "array size %d is out of range", l)
			return Range{}, false
		}
		return Range{0, int(l) - 1}, true // "[N]" is [0:N-1]
	}
	r, ok := b.constInt(d.RightExpr)
	if !ok {
		return Range{}, false
	}
	if l > maxElems || l < -maxElems || r > maxElems || r < -maxElems {
		b.errorf(diag.CodeInvalidType, d.LeftExpr.Pos(), "range [%d:%d] is too large", l, r)
		return Range{}, false
	}
	return Range{int(l), int(r)}, true
}

// constInt evaluates e as a constant integer in the binder's scope.
func (b *binder) constInt(e ast.Expr) (int64, bool) {
	be := b.bindSelf(e)
	if be == nil || IsInvalid(be.Type()) {
		return 0, false
	}
	c, ok := b.eval(be)
	if !ok {
		return 0, false
	}
	n, ok := c.Int64()
	if !ok {
		b.errorf(diag.CodeNotConstant, e.Pos(), "expected a constant integer")
		return 0, false
	}
	return n, true
}

// resolveNamed looks up a user type name: a typedef, class or type parameter.
func (b *binder) resolveNamed(t *ast.Type) Type {
	var res Result
	if t.PackageQualifier != "" {
		res = b.scope.LookupQualified(append(splitQual(t.PackageQualifier), t.Name))
	} else {
		res = b.scope.Lookup(t.Name)
	}
	sym := res.Symbol
	if sym == nil && t.PackageQualifier == "" {
		// An interface used as a port type, "bus ifc" or "bus.mp ifc".
		if def, ok := b.m.comp.Lookup(t.Name); ok && def.Kind == DefInterface {
			return &VirtualInterface{Interface: def.Name, Modport: t.Modport}
		}
	}
	if sym == nil && t.PackageQualifier == "" {
		if c, ok := builtinClasses[t.Name]; ok {
			return c // the std package's classes (LRM 9.7, 15.3, 15.4)
		}
	}
	if sym == nil {
		if len(res.Ambiguous) > 0 {
			b.errorf(diag.CodeUndeclared, t.Pos(), "type name %q is ambiguous: imported from several packages", t.Name)
		} else {
			b.errorf(diag.CodeUndeclared, t.Pos(), "unknown type %q", t.Name)
		}
		return Invalid
	}
	switch sym.Kind {
	case SymTypedef:
		return b.typedefType(sym)
	case SymClass:
		return b.classType(sym)
	case SymTypeParam:
		return b.typeParamType(sym)
	case SymNettype:
		return typeLogic
	case SymInterface:
		return &VirtualInterface{Interface: sym.Name, Modport: t.Modport}
	}
	b.errorf(diag.CodeInvalidType, t.Pos(), "%q is a %s, not a type", t.Name, sym.Kind)
	return Invalid
}

// builtinClasses are the classes of the built-in std package.
var builtinClasses = map[string]*Class{
	"process":   {Name: "process"},
	"mailbox":   {Name: "mailbox"},
	"semaphore": {Name: "semaphore"},
}

func splitQual(q string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(q); i++ {
		if q[i] == ':' && q[i+1] == ':' {
			out = append(out, q[start:i])
			start = i + 2
			i++
		}
	}
	return append(out, q[start:])
}

// typedefType resolves a typedef once and wraps the result in an Alias.
func (b *binder) typedefType(sym *Symbol) Type {
	tc := b.m.cache()
	if t, ok := tc.byNode[sym.Decl]; ok {
		return t
	}
	td, ok := sym.Decl.(*ast.Typedef)
	if !ok || tc.busy[sym] {
		return Invalid
	}
	tc.busy[sym] = true
	defer delete(tc.busy, sym)
	alias := &Alias{Name: td.Name}
	tc.byNode[sym.Decl] = alias // a recursive mention (a class holding a handle to itself) finds the alias
	b.withScope(sym.Scope, func() {
		switch u := td.Underlying.(type) {
		case *ast.TypeAlias:
			alias.Target = b.resolveType(&u.Type)
		case *ast.Struct, *ast.Union, *ast.Enum:
			alias.Target = b.aggregate(u, td.Name)
		default:
			alias.Target = Invalid
		}
	})
	if alias.Target == nil {
		alias.Target = Invalid
	}
	return alias
}

// resolveInline resolves an anonymous struct, union or enum written in place.
func (b *binder) resolveInline(t *ast.Type) Type {
	if t.Inline == nil {
		return Invalid
	}
	return b.aggregate(t.Inline, "")
}

// aggregate builds the Type of a struct, union or enum declaration, once per
// source position.
func (b *binder) aggregate(d ast.Decl, name string) Type {
	tc := b.m.cache()
	key := posKey{fmt.Sprintf("%T", d), d.Pos()}
	if name != "" {
		key.kind += ":" + name
	}
	if t, ok := tc.byPos[key]; ok {
		return t
	}
	switch n := d.(type) {
	case *ast.Struct:
		st := &Struct{Name: name, Packed: n.Packed}
		tc.byPos[key] = st
		st.Members = b.members(n.Members)
		return st
	case *ast.Union:
		un := &Union{Name: name, Packed: n.Packed, Tagged: n.Tagged}
		tc.byPos[key] = un
		un.Members = b.members(n.Members)
		return un
	case *ast.Enum:
		en := &Enum{Name: name}
		tc.byPos[key] = en
		b.buildEnum(en, n)
		return en
	}
	return Invalid
}

func (b *binder) members(vars []ast.Variable) []Member {
	out := make([]Member, 0, len(vars))
	for i := range vars {
		out = append(out, Member{Name: vars[i].Name, Type: b.varType(&vars[i].Type, vars[i].UnpackedDims)})
	}
	return out
}

// buildEnum fills in an enum's base type and members. A member without a value
// is one more than the previous (LRM 6.19.2).
func (b *binder) buildEnum(en *Enum, n *ast.Enum) {
	en.Base = typeInt
	if n.BaseType.Name != "" || len(n.BaseType.PackedDims) > 0 {
		en.Base = b.resolveType(&n.BaseType)
	}
	w, signed, four := 32, true, false
	if bits, ok := Bits(en.Base); ok {
		w, signed, four = bits, IsSigned(en.Base), IsFourState(en.Base)
	}
	_ = four
	next := svint.FromInt64(w, 0).Resize(w)
	next = withSign(next, signed)
	for i := range n.Members {
		mem := &n.Members[i]
		val := next
		if mem.ValueExpr != nil {
			if be := b.bindSelf(mem.ValueExpr); be != nil && !IsInvalid(be.Type()) {
				if c, ok := b.eval(be); ok && c.Kind == ConstInt {
					val = withSign(c.Int.AsUnsigned().Resize(w), signed)
				}
			}
		}
		en.Members = append(en.Members, EnumMember{Name: mem.Name, Value: val})
		next = svint.Add(val, withSign(svint.FromUint64(w, 1), signed))
	}
}

func withSign(v svint.Value, signed bool) svint.Value {
	if signed {
		return v.AsSigned()
	}
	return v.AsUnsigned()
}

// classType resolves a class declaration to its Type once, with its base class.
func (b *binder) classType(sym *Symbol) Type {
	tc := b.m.cache()
	if t, ok := tc.byNode[sym.Decl]; ok {
		return t
	}
	cl := &Class{Name: sym.Name}
	tc.byNode[sym.Decl] = cl
	tc.classes[cl] = sym.Child
	if sc := b.m.superScope(sym.Child); sc != nil {
		if sup, ok := sc.Node.(*ast.Class); ok {
			if ssym, ok := sc.Parent.Own(sup.Name); ok {
				if st, ok := b.classType(ssym).(*Class); ok {
					cl.Super = st
				}
			}
		}
	}
	return cl
}

func (b *binder) typeParamType(sym *Symbol) Type {
	tc := b.m.cache()
	if t, ok := tc.byNode[sym.Decl]; ok {
		return t
	}
	p := &TypeParam{Name: sym.Name}
	tc.byNode[sym.Decl] = p
	return p
}

// varType resolves a declaration's type together with its unpacked dimensions.
func (b *binder) varType(t *ast.Type, dims []ast.Dim) Type {
	elem := b.resolveType(t)
	if IsInvalid(elem) {
		return elem
	}
	for i := len(dims) - 1; i >= 0; i-- {
		elem = b.unpacked(elem, &dims[i], t.Position)
		if IsInvalid(elem) {
			return elem
		}
	}
	return elem
}

// unpacked wraps elem in the unpacked dimension d: fixed [n] or [l:r], dynamic
// [], queue [$], associative [type] or [*].
func (b *binder) unpacked(elem Type, d *ast.Dim, at ast.Position) Type {
	switch {
	case d.Wildcard:
		return &AssocArray{Elem: elem}
	case d.LeftExpr == nil && len(d.Left) == 0:
		return &DynamicArray{Elem: elem}
	}
	if lit, ok := d.LeftExpr.(*ast.Literal); ok && lit.Kind == ast.LitUnbounded {
		bound := -1
		if d.RightExpr != nil {
			if n, ok := b.constInt(d.RightExpr); ok {
				bound = int(n)
			}
		}
		return &Queue{Elem: elem, Bound: bound}
	}
	if d.RightExpr == nil {
		// "[N]" is a size, but "[T]" is an associative array indexed by T.
		if toe, ok := d.LeftExpr.(*ast.TypeOrExpr); ok && b.scope.ClassifyName(toe) == NameType {
			return &AssocArray{Elem: elem, Index: b.typeFromName(toe.X)}
		}
		if te, ok := d.LeftExpr.(*ast.TypeExpr); ok {
			return &AssocArray{Elem: elem, Index: b.resolveType(&te.Type)}
		}
	}
	r, ok := b.dimRange(d, at)
	if !ok {
		return Invalid
	}
	return &UnpackedArray{Elem: elem, Range: r}
}

// typeFromName resolves a name-shaped expression that is known to be a type.
func (b *binder) typeFromName(e ast.Expr) Type {
	switch e := e.(type) {
	case *ast.Ident:
		return b.resolveType(&ast.Type{Position: e.Pos(), Name: e.Name})
	case *ast.ScopedName:
		if path, ok := scopedPath(e); ok && len(path) >= 2 {
			t := &ast.Type{Position: e.Pos(), Name: path[len(path)-1]}
			for i, p := range path[:len(path)-1] {
				if i > 0 {
					t.PackageQualifier += "::"
				}
				t.PackageQualifier += p
			}
			return b.resolveType(t)
		}
	case *ast.ParameterizedName:
		if e.Scope == nil {
			return b.resolveType(&ast.Type{Position: e.Pos(), Name: e.Name})
		}
	case *ast.TypeOrExpr:
		return b.typeFromName(e.X)
	case *ast.TypeExpr:
		return b.resolveType(&e.Type)
	}
	return Invalid
}

// typeOfSymbol returns the declared type of a value symbol.
func (b *binder) typeOfSymbol(sym *Symbol) Type {
	tc := b.m.cache()
	if t, ok := tc.symTypes[sym]; ok {
		return t
	}
	if tc.busy[sym] {
		if sym.Kind == SymParameter {
			b.errorf(diag.CodeCircularParameter, sym.Pos(), "parameter %q depends on itself", sym.Name)
		}
		return Invalid
	}
	tc.busy[sym] = true
	defer delete(tc.busy, sym)

	var t Type = Invalid
	b.withScope(sym.Scope, func() {
		switch d := sym.Decl.(type) {
		case *ast.Variable:
			t = b.varType(&d.Type, d.UnpackedDims)
		case *ast.Port:
			if rv, ok := sym.Redeclared.(*ast.Variable); ok && d.Type.Name == "" {
				t = b.varType(&rv.Type, append(append([]ast.Dim(nil), rv.UnpackedDims...), d.UnpackedDims...))
			} else {
				t = b.varType(&d.Type, d.UnpackedDims)
			}
		case *ast.Arg:
			t = b.varType(&d.Type, d.UnpackedDims)
		case *ast.Parameter:
			t = b.parameterType(sym, d)
		case *ast.EnumMember:
			if e, ok := sym.Owner.(*ast.Enum); ok {
				t = b.aggregate(e, "")
				if td := typedefOwning(sym.Scope, e); td != nil {
					t = b.typedefType(td)
				}
			}
		case *ast.Foreach:
			t = typeInt
		case *ast.GenerateFor:
			t = typeInt
		case *ast.ClockingSignal:
			t = typeLogic
		}
	})
	tc.symTypes[sym] = t
	return t
}

// typedefOwning finds the typedef in scope s whose body is the enum e.
func typedefOwning(s *Scope, e *ast.Enum) *Symbol {
	for _, sym := range s.order {
		if td, ok := sym.Decl.(*ast.Typedef); ok && td.Underlying == ast.Decl(e) {
			return sym
		}
	}
	return nil
}

// parameterType is the type of a parameter: the declared one, or for an untyped
// parameter the type of its value (LRM 6.20.2).
func (b *binder) parameterType(sym *Symbol, p *ast.Parameter) Type {
	if p.Type.Name != "" || len(p.Type.PackedDims) > 0 {
		if p.Type.Name == "type" {
			return Invalid
		}
		return b.varType(&p.Type, p.UnpackedDims)
	}
	if p.DefaultExpr == nil {
		return typeInteger
	}
	be := b.bindSelf(p.DefaultExpr)
	if be == nil {
		return Invalid
	}
	return be.Type()
}
