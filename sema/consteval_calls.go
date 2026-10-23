package sema

import (
	"math"

	"github.com/jfetkotto/svparse/svint"
)

// evalCall evaluates a call in a constant expression: the system functions of
// LRM 20.8-20.10 (math, conversion and array queries). A user function needs
// the statement interpreter and is not constant here.
func (b *binder) evalCall(n *BCall) (Const, bool) {
	if n.Sym != nil || n.Method != "" || len(n.Name) == 0 || n.Name[0] != '$' {
		return b.notConst(n.src, "a call of %q is not a constant expression", n.Name)
	}
	switch n.Name {
	case "$bits":
		return b.evalQuery(n, func(t Type) (int64, bool) { w, ok := Bits(t); return int64(w), ok })
	case "$dimensions", "$unpacked_dimensions":
		return b.evalQuery(n, func(t Type) (int64, bool) {
			dims, unpacked, ok := dimensionsOf(t)
			if n.Name == "$unpacked_dimensions" {
				return int64(unpacked), ok
			}
			return int64(len(dims)), ok
		})
	case "$size", "$left", "$right", "$low", "$high", "$increment":
		return b.evalDimQuery(n)
	case "$typename":
		t := firstArgType(n)
		return strConst(t.String()), true
	}

	args := make([]Const, len(n.Args))
	for i, a := range n.Args {
		if _, isType := a.(*BTypeRef); isType {
			return b.notConst(n.src, "%s does not take a type", n.Name)
		}
		c, ok := b.eval(a)
		if !ok {
			return Const{}, false
		}
		args[i] = c
	}
	need := func(k int) bool {
		if len(args) < k {
			b.notConst(n.src, "%s needs %d argument(s)", n.Name, k)
			return false
		}
		return true
	}
	real1 := func(f func(float64) float64) (Const, bool) {
		if !need(1) {
			return Const{}, false
		}
		return realConst(f(b.conv(args[0], typeReal).Real)), true
	}
	real2 := func(f func(a, c float64) float64) (Const, bool) {
		if !need(2) {
			return Const{}, false
		}
		return realConst(f(b.conv(args[0], typeReal).Real, b.conv(args[1], typeReal).Real)), true
	}
	switch n.Name {
	case "$clog2":
		if !need(1) || args[0].Kind != ConstInt {
			return b.notConst(n.src, "$clog2 needs an integral argument")
		}
		return intConst(svint.Clog2(args[0].Int)), true
	case "$signed", "$unsigned":
		if !need(1) || args[0].Kind != ConstInt {
			return b.notConst(n.src, "%s needs an integral argument", n.Name)
		}
		return intConst(withSign(args[0].Int, n.Name == "$signed")), true
	case "$countones":
		if !need(1) || args[0].Kind != ConstInt {
			return Const{}, false
		}
		return intConst(svint.FromInt64(32, int64(args[0].Int.OnesCount()))), true
	case "$onehot", "$onehot0":
		if !need(1) || args[0].Kind != ConstInt {
			return Const{}, false
		}
		k := args[0].Int.OnesCount()
		return intConst(svint.FromBool(k == 1 || (n.Name == "$onehot0" && k == 0))), true
	case "$isunknown":
		if !need(1) || args[0].Kind != ConstInt {
			return Const{}, false
		}
		return intConst(svint.FromBool(!args[0].Int.IsKnown())), true
	case "$itor":
		if !need(1) {
			return Const{}, false
		}
		return realConst(b.conv(args[0], typeReal).Real), true
	case "$rtoi":
		if !need(1) {
			return Const{}, false
		}
		f := b.conv(args[0], typeReal).Real
		return intConst(svint.FromFloat64(math.Trunc(f), 32, true)), true
	case "$realtobits":
		if !need(1) {
			return Const{}, false
		}
		return intConst(svint.FromUint64(64, math.Float64bits(b.conv(args[0], typeReal).Real))), true
	case "$bitstoreal":
		if !need(1) || args[0].Kind != ConstInt {
			return Const{}, false
		}
		u, ok := args[0].Int.AsUnsigned().Resize(64).Uint64()
		if !ok {
			return Const{}, false
		}
		return realConst(math.Float64frombits(u)), true
	case "$sqrt":
		return real1(math.Sqrt)
	case "$exp":
		return real1(math.Exp)
	case "$ln":
		return real1(math.Log)
	case "$log10":
		return real1(math.Log10)
	case "$sin":
		return real1(math.Sin)
	case "$cos":
		return real1(math.Cos)
	case "$tan":
		return real1(math.Tan)
	case "$asin":
		return real1(math.Asin)
	case "$acos":
		return real1(math.Acos)
	case "$atan":
		return real1(math.Atan)
	case "$sinh":
		return real1(math.Sinh)
	case "$cosh":
		return real1(math.Cosh)
	case "$tanh":
		return real1(math.Tanh)
	case "$asinh":
		return real1(math.Asinh)
	case "$acosh":
		return real1(math.Acosh)
	case "$atanh":
		return real1(math.Atanh)
	case "$floor":
		return real1(math.Floor)
	case "$ceil":
		return real1(math.Ceil)
	case "$pow":
		return real2(math.Pow)
	case "$atan2":
		return real2(math.Atan2)
	case "$hypot":
		return real2(math.Hypot)
	}
	return b.notConst(n.src, "%s is not a constant function", n.Name)
}

// firstArgType is the type of a call's first argument: the type itself for a
// type operand, else the expression's type.
func firstArgType(n *BCall) Type {
	if len(n.Args) == 0 {
		return Invalid
	}
	if tr, ok := n.Args[0].(*BTypeRef); ok {
		return tr.T
	}
	return n.Args[0].Type()
}

func (b *binder) evalQuery(n *BCall, f func(Type) (int64, bool)) (Const, bool) {
	t := firstArgType(n)
	if IsInvalid(t) {
		return Const{}, false
	}
	v, ok := f(t)
	if !ok {
		return b.notConst(n.src, "%s of %s is not known at compile time", n.Name, t)
	}
	return intConst(svint.FromInt64(32, v)), true
}

// dimensionsOf lists the fixed dimensions of a type, unpacked ones first and
// then packed ones, each outermost first (LRM 20.7). The second result is how
// many are unpacked. It fails at a dynamic dimension.
func dimensionsOf(t Type) ([]Range, int, bool) {
	var dims []Range
	unpacked := 0
	for i := 0; i < 64; i++ {
		switch u := Resolve(t).(type) {
		case *UnpackedArray:
			dims = append(dims, u.Range)
			unpacked++
			t = u.Elem
			continue
		case *DynamicArray, *Queue, *AssocArray:
			return dims, unpacked, false
		case *PackedArray:
			dims = append(dims, u.Range)
			t = u.Elem
			continue
		case *Integral:
			if u.HasRange {
				dims = append(dims, u.Range)
			} else if w := u.Width(); w > 1 {
				dims = append(dims, Range{w - 1, 0})
			}
		case *Enum:
			t = u.Base
			continue
		case *Struct, *Union:
			if w, ok := Bits(u); ok && IsIntegral(u) && w > 0 {
				dims = append(dims, Range{w - 1, 0})
			}
		}
		return dims, unpacked, true
	}
	return dims, unpacked, false
}

// evalDimQuery handles $size, $left, $right, $low, $high and $increment, whose
// optional second argument picks the dimension (default 1).
func (b *binder) evalDimQuery(n *BCall) (Const, bool) {
	t := firstArgType(n)
	if IsInvalid(t) {
		return Const{}, false
	}
	dim := int64(1)
	if len(n.Args) > 1 {
		c, ok := b.eval(n.Args[1])
		if !ok {
			return Const{}, false
		}
		if dim, ok = c.Int64(); !ok {
			return b.notConst(n.src, "the dimension must be a constant integer")
		}
	}
	dims, _, ok := dimensionsOf(t)
	if dim < 1 || dim > int64(len(dims)) || (!ok && dim > int64(len(dims))) {
		if !ok {
			return b.notConst(n.src, "%s of %s is not known at compile time", n.Name, t)
		}
		return intConst(svint.X(32, true)), true
	}
	r := dims[dim-1]
	var v int64
	switch n.Name {
	case "$size":
		v = int64(r.Size())
	case "$left":
		v = int64(r.Left)
	case "$right":
		v = int64(r.Right)
	case "$low":
		v = int64(min(r.Left, r.Right))
	case "$high":
		v = int64(max(r.Left, r.Right))
	case "$increment":
		v = 1
		if r.Left < r.Right {
			v = -1
		}
	}
	return intConst(svint.FromInt64(32, v)), true
}
