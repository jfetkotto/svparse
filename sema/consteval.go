package sema

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
	"github.com/jfetkotto/svparse/svint"
)

// ConstKind says which field of a Const holds the value.
type ConstKind uint8

const (
	ConstInt ConstKind = iota
	ConstReal
	ConstString
)

// Const is the value of a constant expression (LRM 11.2.1): an integral value
// of any width, a real, or a string. A string also keeps its bytes as an
// integral Int, as a string literal is one.
type Const struct {
	Kind ConstKind
	Int  svint.Value
	Real float64
	Str  string
}

func intConst(v svint.Value) Const { return Const{Kind: ConstInt, Int: v} }
func realConst(f float64) Const    { return Const{Kind: ConstReal, Real: f} }
func strConst(s string) Const      { return Const{Kind: ConstString, Str: s, Int: svint.FromString(s)} }

// Int64 returns an integral constant as a Go integer. It fails for a real, a
// string, a value with x or z bits, or one that does not fit.
func (c Const) Int64() (int64, bool) {
	if c.Kind != ConstInt {
		return 0, false
	}
	return c.Int.Int64()
}

func (c Const) String() string {
	switch c.Kind {
	case ConstReal:
		return fmt.Sprint(c.Real)
	case ConstString:
		return fmt.Sprintf("%q", c.Str)
	}
	return c.Int.String()
}

// Env is the state a constant evaluation reads and writes: parameter values
// that override their defaults, values of loop variables and constant-function
// locals, and the step budget that guarantees it ends. The zero value is ready
// to use; use one Env per elaborated instance. Types are resolved once with the
// parameters' default values (an array dimension "[W-1:0]" does not follow an
// override of W here); per-instance types are the elaborator's job.
type Env struct {
	// Params overrides the value of a parameter, as an instantiation's
	// "#(.W(8))" does. The value is converted to the parameter's type.
	Params map[*Symbol]Const
	// Vars gives values to variables, such as an enclosing generate loop's
	// genvar.
	Vars map[*Symbol]Const
	// MaxSteps bounds the number of expression nodes evaluated; the default
	// is one million. Exceeding it is reported as a constant-limit error.
	MaxSteps int

	steps  int
	depth  int // nesting of constant function calls
	cache  map[*Symbol]Const
	active map[*Symbol]bool
	limit  bool
}

const defaultMaxSteps = 1_000_000

// Eval binds and evaluates e as a constant expression in scope s. The result
// has the expression's self-determined type. Problems come back as
// diagnostics: undeclared names (from binding), and not-constant,
// circular-parameter and constant-limit errors (from evaluation).
func (m *Model) Eval(s *Scope, e ast.Expr, env *Env) (Const, []diag.Diagnostic) {
	b := m.newBinder(s)
	b.env = env
	be := b.bindSelf(e)
	if be == nil || IsInvalid(be.Type()) {
		return Const{}, b.diags()
	}
	c, _ := b.eval(be)
	return c, b.diags()
}

// EvalAssign binds e as the right side of an assignment to target, evaluates
// it, and converts the value to target, as a parameter's default is.
func (m *Model) EvalAssign(s *Scope, target Type, e ast.Expr, env *Env) (Const, []diag.Diagnostic) {
	b := m.newBinder(s)
	b.env = env
	be := b.bindAssign(target, e)
	if be == nil || IsInvalid(be.Type()) {
		return Const{}, b.diags()
	}
	c, ok := b.eval(be)
	if ok && !IsInvalid(target) {
		c = b.conv(c, target)
	}
	return c, b.diags()
}

// EvalBound evaluates an already bound expression.
func (m *Model) EvalBound(be BoundExpr, env *Env) (Const, []diag.Diagnostic) {
	b := m.newBinder(m.rootScope())
	b.env = env
	c, _ := b.eval(be)
	return c, b.diags()
}

// ParamValue returns the value of a parameter symbol, converted to its type,
// honouring overrides in env.
func (m *Model) ParamValue(sym *Symbol, env *Env) (Const, []diag.Diagnostic) {
	b := m.newBinder(sym.Scope)
	b.env = env
	c, _ := b.paramValue(sym)
	return c, b.diags()
}

func (m *Model) rootScope() *Scope {
	for _, s := range m.units {
		return s
	}
	return nil
}

// paramInfo remembers a parameter's bound default.
type paramInfo struct {
	bound BoundExpr
	typ   Type
}

func (b *binder) environment() *Env {
	if b.env == nil {
		b.env = &Env{}
	}
	e := b.env
	if e.cache == nil {
		e.cache = map[*Symbol]Const{}
		e.active = map[*Symbol]bool{}
	}
	if e.MaxSteps <= 0 {
		e.MaxSteps = defaultMaxSteps
	}
	return e
}

// notConst reports that an expression is not constant.
func (b *binder) notConst(at ast.Expr, format string, args ...any) (Const, bool) {
	var pos ast.Position
	if at != nil {
		pos = at.Pos()
	}
	b.errorf(diag.CodeNotConstant, pos, format, args...)
	return Const{}, false
}

// eval evaluates a bound expression and converts the result to its type.
func (b *binder) eval(be BoundExpr) (Const, bool) {
	env := b.environment()
	if env.limit {
		return Const{}, false
	}
	env.steps++
	if env.steps > env.MaxSteps {
		env.limit = true
		var pos ast.Position
		if be.Source() != nil {
			pos = be.Source().Pos()
		}
		b.errorf(diag.CodeConstantLimit, pos, "constant evaluation exceeded %d steps", env.MaxSteps)
		return Const{}, false
	}
	c, ok := b.evalNode(be)
	if !ok {
		return Const{}, false
	}
	return b.conv(c, be.Type()), true
}

func (b *binder) evalNode(be BoundExpr) (Const, bool) {
	switch n := be.(type) {
	case *BLiteral:
		return b.evalLiteral(n)
	case *BName:
		return b.evalName(n)
	case *BUnary:
		return b.evalUnary(n)
	case *BBinary:
		return b.evalBinary(n)
	case *BCond:
		return b.evalCond(n)
	case *BConcat:
		return b.evalConcat(n)
	case *BReplicate:
		return b.evalReplicate(n)
	case *BIndex:
		return b.evalIndex(n)
	case *BPartSelect:
		return b.evalPartSelect(n)
	case *BCast:
		x, ok := b.eval(n.X)
		if !ok {
			return Const{}, false
		}
		if n.Kind == CastSign {
			if x.Kind == ConstInt {
				x.Int = withSign(x.Int, IsSigned(n.typ))
			}
		}
		return x, true
	case *BInside:
		return b.evalInside(n)
	case *BCall:
		return b.evalCall(n)
	}
	return b.notConst(be.Source(), "expression is not constant")
}

func (b *binder) evalLiteral(n *BLiteral) (Const, bool) {
	switch n.Kind {
	case ast.LitInt:
		return intConst(n.Value), true
	case ast.LitUnbasedUnsized:
		w := widthOf(n.typ)
		if w == 0 {
			w = 1
		}
		return intConst(svint.Fill(n.Value.Bit(0), w, false)), true
	case ast.LitReal, ast.LitTime:
		return realConst(n.Real), true
	case ast.LitString:
		return strConst(n.Str), true
	}
	return b.notConst(n.src, "a %v literal is not a constant value", n.Kind)
}

func (b *binder) evalName(n *BName) (Const, bool) {
	env := b.environment()
	switch n.Sym.Kind {
	case SymParameter:
		return b.paramValue(n.Sym)
	case SymEnumMember:
		if en, ok := Resolve(n.typ).(*Enum); ok {
			for _, m := range en.Members {
				if m.Name == n.Sym.Name {
					return intConst(m.Value), true
				}
			}
		}
	case SymGenvar, SymLoopVar, SymVariable, SymArg:
		if c, ok := env.Vars[n.Sym]; ok {
			return c, true
		}
	}
	return b.notConst(n.src, "%q is not a constant", n.Sym.Name)
}

// paramValue evaluates a parameter: its override if env has one, else its
// default, in either case converted to the parameter's type. A parameter whose
// value depends on itself is an error.
func (b *binder) paramValue(sym *Symbol) (Const, bool) {
	env := b.environment()
	if c, ok := env.cache[sym]; ok {
		return c, true
	}
	if env.active[sym] {
		b.errorf(diag.CodeCircularParameter, sym.Pos(), "parameter %q depends on itself", sym.Name)
		return Const{}, false
	}
	env.active[sym] = true
	defer delete(env.active, sym)

	decl, ok := sym.Decl.(*ast.Parameter)
	if !ok {
		return Const{}, false
	}
	if ov, ok := env.Params[sym]; ok {
		t := b.typeOfSymbol(sym)
		c := b.conv(ov, t)
		env.cache[sym] = c
		return c, true
	}
	if decl.DefaultExpr == nil {
		b.errorf(diag.CodeNotConstant, sym.Pos(), "parameter %q has no value", sym.Name)
		return Const{}, false
	}
	info := b.paramBound(sym, decl)
	if info.bound == nil {
		return Const{}, false
	}
	c, ok := b.eval(info.bound)
	if !ok {
		return Const{}, false
	}
	c = b.conv(c, info.typ)
	env.cache[sym] = c
	return c, true
}

func (b *binder) paramBound(sym *Symbol, decl *ast.Parameter) *paramInfo {
	if b.m.params == nil {
		b.m.params = map[*Symbol]*paramInfo{}
	}
	if info, ok := b.m.params[sym]; ok {
		return info
	}
	info := &paramInfo{}
	b.m.params[sym] = info
	b.withScope(sym.Scope, func() {
		info.typ = b.typeOfSymbol(sym)
		if IsInvalid(info.typ) {
			return
		}
		if decl.Type.Name == "" && len(decl.Type.PackedDims) == 0 {
			info.bound = b.bindSelf(decl.DefaultExpr)
		} else {
			info.bound = b.bindAssign(info.typ, decl.DefaultExpr)
		}
	})
	return info
}

// conv converts a constant to type t: widening or narrowing an integer by its
// own signedness, rounding a real to an integer, turning an integer into a
// real, or reading a string as its bytes (LRM 11.8.2, 6.12.2).
func (b *binder) conv(c Const, t Type) Const {
	t = Resolve(t)
	switch {
	case IsReal(t):
		switch c.Kind {
		case ConstInt:
			f, _ := c.Int.Float64()
			return realConst(f)
		case ConstString:
			return c
		}
		return c
	case IsIntegral(t):
		w, signed, four := widthOf(t), IsSigned(t), IsFourState(t)
		var v svint.Value
		switch c.Kind {
		case ConstReal:
			v = svint.FromFloat64(c.Real, w, signed)
		default:
			v = c.Int
			if !signed {
				v = v.AsUnsigned()
			}
			v = v.Resize(w)
		}
		v = withSign(v, signed)
		if !four && !v.IsKnown() {
			v = twoState(v)
		}
		return intConst(v)
	case t == String:
		if c.Kind == ConstInt {
			s, _ := c.Int.Text()
			return strConst(s)
		}
	}
	return c
}

// twoState replaces the x and z bits of v with 0.
func twoState(v svint.Value) svint.Value {
	n := new(big.Int)
	for i := 0; i < v.Width(); i++ {
		if v.Bit(i) == svint.Bit1 {
			n.SetBit(n, i, 1)
		}
	}
	return svint.FromBig(v.Width(), v.Signed(), n)
}

func (b *binder) evalUnary(n *BUnary) (Const, bool) {
	x, ok := b.eval(n.X)
	if !ok {
		return Const{}, false
	}
	if x.Kind == ConstReal {
		switch n.Op {
		case "+":
			return x, true
		case "-":
			return realConst(-x.Real), true
		case "!":
			return intConst(svint.FromBool(x.Real == 0)), true
		}
		return b.notConst(n.src, "operator %s does not apply to a real", n.Op)
	}
	v := x.Int
	if n.Op == "+" || n.Op == "-" || n.Op == "~" {
		v = b.conv(x, n.typ).Int
	}
	switch n.Op {
	case "+":
		return intConst(v), true
	case "-":
		return intConst(svint.Neg(v)), true
	case "~":
		return intConst(svint.Not(v)), true
	case "!":
		return intConst(svint.LogicalNot(v)), true
	case "&":
		return intConst(svint.RedAnd(v)), true
	case "|":
		return intConst(svint.RedOr(v)), true
	case "^":
		return intConst(svint.RedXor(v)), true
	case "~&":
		return intConst(svint.RedNand(v)), true
	case "~|":
		return intConst(svint.RedNor(v)), true
	case "~^", "^~":
		return intConst(svint.RedXnor(v)), true
	}
	return b.notConst(n.src, "operator %s is not supported in constant expressions", n.Op)
}

func (b *binder) evalBinary(n *BBinary) (Const, bool) {
	x, ok := b.eval(n.X)
	if !ok {
		return Const{}, false
	}
	y, ok := b.eval(n.Y)
	if !ok {
		return Const{}, false
	}
	op := n.Op
	switch {
	case logicOps[op]:
		return b.evalLogic(n, x, y)
	case relOps[op]:
		return b.evalRel(n, x, y)
	case shiftOps[op] && op != "**":
		if x.Kind != ConstInt || y.Kind != ConstInt {
			return b.notConst(n.src, "shift operands must be integral")
		}
		xv := b.conv(x, n.typ).Int
		fn := map[string]func(a, c svint.Value) svint.Value{"<<": svint.Shl, ">>": svint.Shr, "<<<": svint.Sshl, ">>>": svint.Sshr}[op]
		return intConst(fn(xv, y.Int.AsUnsigned())), true
	}
	if IsReal(n.typ) {
		return b.evalRealArith(n, x, y)
	}
	xv, yv := b.conv(x, n.typ).Int, b.conv(y, n.typ).Int
	switch op {
	case "+":
		return intConst(svint.Add(xv, yv)), true
	case "-":
		return intConst(svint.Sub(xv, yv)), true
	case "*":
		return intConst(svint.Mul(xv, yv)), true
	case "/":
		return intConst(svint.Div(xv, yv)), true
	case "%":
		return intConst(svint.Mod(xv, yv)), true
	case "&":
		return intConst(svint.And(xv, yv)), true
	case "|":
		return intConst(svint.Or(xv, yv)), true
	case "^":
		return intConst(svint.Xor(xv, yv)), true
	case "~^", "^~":
		return intConst(svint.Xnor(xv, yv)), true
	case "**":
		return intConst(svint.Pow(xv, y.Int)), true
	}
	return b.notConst(n.src, "operator %s is not supported in constant expressions", op)
}

func (b *binder) evalRealArith(n *BBinary, x, y Const) (Const, bool) {
	a, c := b.conv(x, typeReal).Real, b.conv(y, typeReal).Real
	switch n.Op {
	case "+":
		return realConst(a + c), true
	case "-":
		return realConst(a - c), true
	case "*":
		return realConst(a * c), true
	case "/":
		return realConst(a / c), true
	case "**":
		return realConst(math.Pow(a, c)), true
	}
	return b.notConst(n.src, "operator %s does not apply to reals", n.Op)
}

func truth(c Const) svint.Value {
	switch c.Kind {
	case ConstReal:
		return svint.FromBool(c.Real != 0)
	case ConstString:
		return svint.FromBool(c.Str != "")
	}
	return c.Int
}

func (b *binder) evalLogic(n *BBinary, x, y Const) (Const, bool) {
	a, c := truth(x), truth(y)
	switch n.Op {
	case "&&":
		return intConst(svint.LogicalAnd(a, c)), true
	case "||":
		return intConst(svint.LogicalOr(a, c)), true
	case "->":
		return intConst(svint.Implies(a, c)), true
	}
	return intConst(svint.Equiv(a, c)), true
}

func (b *binder) evalRel(n *BBinary, x, y Const) (Const, bool) {
	ot := Resolve(n.OperandType)
	if ot == String || x.Kind == ConstString && y.Kind == ConstString && IsInvalid(ot) {
		var r bool
		switch n.Op {
		case "==", "===", "==?":
			r = x.Str == y.Str
		case "!=", "!==", "!=?":
			r = x.Str != y.Str
		case "<":
			r = x.Str < y.Str
		case "<=":
			r = x.Str <= y.Str
		case ">":
			r = x.Str > y.Str
		default:
			r = x.Str >= y.Str
		}
		return intConst(svint.FromBool(r)), true
	}
	if IsReal(ot) {
		a, c := b.conv(x, typeReal).Real, b.conv(y, typeReal).Real
		var r bool
		switch n.Op {
		case "==", "===", "==?":
			r = a == c
		case "!=", "!==", "!=?":
			r = a != c
		case "<":
			r = a < c
		case "<=":
			r = a <= c
		case ">":
			r = a > c
		default:
			r = a >= c
		}
		return intConst(svint.FromBool(r)), true
	}
	if IsInvalid(ot) || x.Kind != ConstInt || y.Kind != ConstInt {
		return b.notConst(n.src, "operands of %s are not constant values", n.Op)
	}
	xv, yv := b.conv(x, ot).Int, b.conv(y, ot).Int
	fn := map[string]func(a, c svint.Value) svint.Value{
		"==": svint.Eq, "!=": svint.Ne, "===": svint.CaseEq, "!==": svint.CaseNe, "==?": svint.WildEq, "!=?": svint.WildNe,
		"<": svint.Lt, "<=": svint.Le, ">": svint.Gt, ">=": svint.Ge,
	}[n.Op]
	return intConst(fn(xv, yv)), true
}

func (b *binder) evalCond(n *BCond) (Const, bool) {
	c, ok := b.eval(n.Cond)
	if !ok {
		return Const{}, false
	}
	switch svint.Truth(truth(c)) {
	case svint.Bit1:
		return b.eval(n.Then)
	case svint.Bit0:
		return b.eval(n.Else)
	}
	t, ok1 := b.eval(n.Then)
	f, ok2 := b.eval(n.Else)
	if !ok1 || !ok2 || t.Kind != ConstInt || f.Kind != ConstInt {
		return Const{}, false
	}
	return intConst(svint.Cond(svint.FromBit(svint.BitX), t.Int, f.Int)), true
}

func (b *binder) evalConcat(n *BConcat) (Const, bool) {
	parts := make([]svint.Value, 0, len(n.Elems))
	var sb strings.Builder
	allString := Resolve(n.typ) == String
	for _, el := range n.Elems {
		c, ok := b.eval(el)
		if !ok {
			return Const{}, false
		}
		if c.Kind == ConstReal {
			return b.notConst(n.src, "a real cannot be concatenated")
		}
		sb.WriteString(c.Str)
		if allString && c.Kind == ConstInt {
			s, _ := c.Int.Text()
			sb.WriteString(s)
		}
		parts = append(parts, c.Int)
	}
	if allString {
		return strConst(sb.String()), true
	}
	v := svint.Concat(parts...)
	if !v.Valid() {
		return b.notConst(n.src, "concatenation is too wide")
	}
	return intConst(v), true
}

func (b *binder) evalReplicate(n *BReplicate) (Const, bool) {
	cnt, ok := b.eval(n.Count)
	if !ok {
		return Const{}, false
	}
	k, ok := cnt.Int64()
	if !ok || k < 1 {
		return b.notConst(n.src, "replication count must be a positive constant")
	}
	parts := make([]svint.Value, 0, len(n.Elems))
	for _, el := range n.Elems {
		c, ok := b.eval(el)
		if !ok || c.Kind == ConstReal {
			return Const{}, false
		}
		parts = append(parts, c.Int)
	}
	v := svint.Replicate(int(k), svint.Concat(parts...))
	if !v.Valid() {
		return b.notConst(n.src, "replication is too wide")
	}
	return intConst(v), true
}

// packedLayout gives the declared range of a packed type and the width of one
// element of its outermost dimension.
func packedLayout(t Type) (Range, int, bool) {
	switch t := Resolve(t).(type) {
	case *Integral:
		if t.HasRange {
			return t.Range, 1, true
		}
		if w := t.Width(); w > 1 {
			return Range{w - 1, 0}, 1, true
		}
	case *PackedArray:
		ew, _ := Bits(t.Elem)
		return t.Range, ew, true
	case *Enum:
		return packedLayout(t.Base)
	case *Struct, *Union:
		if w, ok := Bits(t); ok && IsIntegral(t) {
			return Range{w - 1, 0}, 1, true
		}
	}
	return Range{}, 0, false
}

// bitPosition maps a source index to a bit offset from the least significant
// end, given the dimension's range and element width.
func bitPosition(r Range, ew int, idx int64) int64 {
	if r.Left >= r.Right {
		return (idx - int64(r.Right)) * int64(ew)
	}
	return (int64(r.Right) - idx) * int64(ew)
}

func (b *binder) evalIndex(n *BIndex) (Const, bool) {
	x, ok := b.eval(n.X)
	if !ok || x.Kind != ConstInt {
		return b.notConst(n.src, "only a constant vector can be indexed in a constant expression")
	}
	i, ok := b.eval(n.Index)
	if !ok {
		return Const{}, false
	}
	idx, ok := i.Int64()
	if !ok {
		return intConst(svint.X(max(widthOf(n.typ), 1), false)), true
	}
	r, ew, ok := packedLayout(n.X.Type())
	if !ok {
		return b.notConst(n.src, "cannot index this type in a constant expression")
	}
	lo := bitPosition(r, ew, idx)
	if lo < 0 || lo >= int64(x.Int.Width()) {
		return intConst(svint.X(ew, false)), true
	}
	return intConst(svint.Slice(x.Int, int(lo)+ew-1, int(lo))), true
}

func (b *binder) evalPartSelect(n *BPartSelect) (Const, bool) {
	x, ok := b.eval(n.X)
	if !ok || x.Kind != ConstInt {
		return b.notConst(n.src, "only a constant vector can be selected in a constant expression")
	}
	l, ok1 := b.evalInt(n.Left)
	rr, ok2 := b.evalInt(n.Right)
	r, ew, ok3 := packedLayout(n.X.Type())
	if !ok1 || !ok2 || !ok3 {
		return Const{}, false
	}
	var first, last int64 // source indices of the two ends
	switch n.Kind {
	case ast.RangeColon:
		first, last = l, rr
	case ast.RangePlus:
		first, last = l, l+rr-1
		if r.Left >= r.Right {
			first, last = l+rr-1, l
		}
	default:
		first, last = l, l-rr+1
		if r.Left < r.Right {
			first, last = l-rr+1, l
		}
	}
	p1, p2 := bitPosition(r, ew, first), bitPosition(r, ew, last)
	hi, lo := max(p1, p2)+int64(ew)-1, min(p1, p2)
	if hi-lo+1 != int64(widthOf(n.typ)) {
		return intConst(svint.X(max(widthOf(n.typ), 1), false)), true
	}
	return intConst(svint.Slice(x.Int, int(hi), int(lo))), true
}

func (b *binder) evalInt(be BoundExpr) (int64, bool) {
	c, ok := b.eval(be)
	if !ok {
		return 0, false
	}
	v, ok := c.Int64()
	if !ok {
		b.notConst(be.Source(), "expected a constant integer")
	}
	return v, ok
}

func (b *binder) evalInside(n *BInside) (Const, bool) {
	x, ok := b.eval(n.X)
	if !ok || x.Kind != ConstInt {
		return Const{}, false
	}
	result := svint.FromBit(svint.Bit0)
	for _, r := range n.Ranges {
		var m svint.Value
		if rg, ok := r.(*BRange); ok {
			lo, ok1 := b.eval(rg.Lo)
			hi, ok2 := b.eval(rg.Hi)
			if !ok1 || !ok2 || lo.Kind != ConstInt || hi.Kind != ConstInt {
				return Const{}, false
			}
			m = svint.LogicalAnd(svint.Ge(x.Int, lo.Int), svint.Le(x.Int, hi.Int))
		} else {
			v, ok := b.eval(r)
			if !ok || v.Kind != ConstInt {
				return Const{}, false
			}
			m = svint.WildEq(x.Int, v.Int)
		}
		result = svint.LogicalOr(result, m)
	}
	return intConst(result), true
}
