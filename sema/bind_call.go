package sema

import (
	"strings"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/diag"
)

func (b *binder) bindCall(e *ast.Call) BoundExpr {
	switch f := e.Func.(type) {
	case *ast.Ident:
		if strings.HasPrefix(f.Name, "$") {
			return b.systemCall(e, f.Name, e.Args)
		}
		res := b.scope.Lookup(f.Name)
		if res.Symbol == nil {
			b.errorf(diag.CodeUndeclared, f.Pos(), "function %q is not declared", f.Name)
			b.bindArgsSelf(e.Args)
			return bad(e)
		}
		return b.userCall(e, res.Symbol)
	case *ast.ScopedName:
		path, ok := scopedPath(f)
		if !ok {
			return bad(e)
		}
		res := b.scope.LookupQualified(path)
		if res.Symbol == nil {
			b.errorf(diag.CodeUndeclared, f.Pos(), "function %q is not declared", strings.Join(path, "::"))
			b.bindArgsSelf(e.Args)
			return bad(e)
		}
		return b.userCall(e, res.Symbol)
	case *ast.MemberSelect:
		return b.methodCall(e, f)
	}
	b.bindArgsSelf(e.Args)
	return bad(e)
}

func (b *binder) bindArgsSelf(args []ast.Expr) []BoundExpr {
	out := make([]BoundExpr, 0, len(args))
	for _, a := range args {
		switch a := a.(type) {
		case *ast.Empty:
			continue
		case *ast.NamedArg:
			if a.Value != nil {
				if be := b.bindSelf(a.Value); be != nil {
					out = append(out, be)
				}
			}
			continue
		}
		if be := b.bindSelf(a); be != nil {
			out = append(out, be)
		}
	}
	return out
}

// userCall binds a call of a declared function: each argument is assigned to
// its formal, positionally or by name.
func (b *binder) userCall(e *ast.Call, sym *Symbol) BoundExpr {
	if sym.Kind != SymFunction {
		b.bindArgsSelf(e.Args)
		if sym.Kind == SymTask {
			return &BCall{exprInfo: exprInfo{Void, e}, Name: sym.Name, Sym: sym}
		}
		return bad(e)
	}
	n := &BCall{exprInfo: exprInfo{b.returnType(sym), e}, Name: sym.Name, Sym: sym}
	n.Args, n.Formals = b.bindActuals(e.Args, sym)
	return n
}

func (b *binder) bindActuals(args []ast.Expr, sym *Symbol) ([]BoundExpr, []int) {
	var formals []ast.Arg
	switch fn := sym.Decl.(type) {
	case *ast.Function:
		formals = fn.Args
	case *ast.Task:
		formals = fn.Args
	}
	var out []BoundExpr
	var idx []int
	for i, a := range args {
		var formal *ast.Arg
		fi := -1
		value := a
		switch a := a.(type) {
		case *ast.Empty:
			continue
		case *ast.NamedArg:
			value = a.Value
			for k := range formals {
				if formals[k].Name == a.Name {
					formal, fi = &formals[k], k
				}
			}
		default:
			if i < len(formals) {
				formal, fi = &formals[i], i
			}
		}
		if value == nil {
			continue
		}
		var be BoundExpr
		if formal != nil && formal.Direction != ast.DirOutput && formal.Direction != ast.DirRef {
			var ft Type
			b.withScope(sym.Scope, func() { ft = b.varType(&formal.Type, formal.UnpackedDims) })
			be = b.bindAssign(ft, value)
		} else {
			be = b.bindSelf(value)
		}
		if be != nil {
			out = append(out, be)
			idx = append(idx, fi)
		}
	}
	return out, idx
}

// methodCall binds "obj.method(args)", "arr.size()", "u.f(x)" and the like.
func (b *binder) methodCall(e *ast.Call, f *ast.MemberSelect) BoundExpr {
	// A function reached through instances or blocks: "u0.sub.f(x)".
	if path, ok := dottedPath(f); ok {
		if first := b.scope.Lookup(path[0]).Symbol; first == nil || !isValueKind(first.Kind) && !first.Kind.IsType() {
			if sym, rest := b.scope.LookupPath(path); sym != nil && len(rest) == 0 {
				return b.userCall(e, sym)
			}
		}
	}
	recv := b.bindSelf(f.X)
	if recv == nil {
		return bad(e)
	}
	args := b.bindArgsSelf(e.Args)
	n := &BCall{exprInfo: exprInfo{Invalid, e}, Name: f.Member, Method: f.Member, Recv: recv, Args: args}
	rt := Resolve(recv.Type())
	if cl, ok := rt.(*Class); ok {
		if cs := b.m.cache().classes[cl]; cs != nil {
			sym, found := cs.syms[f.Member]
			if !found {
				sym, found = b.m.inheritedMember(cs, f.Member, 0)
			}
			if found && sym.Kind == SymFunction {
				n.Sym = sym
				n.typ = b.returnType(sym)
				n.Args, n.Formals = b.bindActuals(e.Args, sym)
				return n
			}
			if found && sym.Kind == SymTask {
				n.Sym, n.typ = sym, Void
				return n
			}
		}
	}
	n.typ = builtinMethodType(rt, f.Member)
	return n
}

// builtinMethodType is the result type of a built-in method (LRM 7.12, 7.10,
// 7.9, 6.16, 6.19.5), or Invalid for one this layer does not know.
func builtinMethodType(recv Type, name string) Type {
	switch name {
	case "size", "num", "len", "exists", "randomize":
		return typeInt
	case "delete", "push_back", "push_front", "insert", "clear":
		return Void
	}
	switch t := recv.(type) {
	case *Enum:
		switch name {
		case "name":
			return String
		case "first", "last", "next", "prev":
			return t
		}
	case *DynamicArray:
		return arrayMethod(t.Elem, name)
	case *UnpackedArray:
		return arrayMethod(t.Elem, name)
	case *Queue:
		switch name {
		case "pop_front", "pop_back":
			return t.Elem
		}
		return arrayMethod(t.Elem, name)
	case *AssocArray:
		switch name {
		case "first", "last", "next", "prev":
			return typeInt
		}
		return arrayMethod(t.Elem, name)
	default:
		if recv == String {
			switch name {
			case "toupper", "tolower", "substr", "itoa", "hextoa", "octtoa", "bintoa", "realtoa":
				return String
			case "getc":
				return NewIntegral(BaseByte)
			case "compare", "icompare", "atoi", "atohex", "atooct", "atobin":
				return typeInt
			case "atoreal":
				return typeReal
			}
		}
	}
	return Invalid
}

func arrayMethod(elem Type, name string) Type {
	switch name {
	case "sum", "product", "and", "or", "xor":
		return elem
	case "min", "max", "unique", "reverse", "sort", "rsort", "shuffle", "find", "find_first", "find_last":
		return &Queue{Elem: elem, Bound: -1}
	case "find_index", "find_first_index", "find_last_index", "unique_index":
		return &Queue{Elem: typeInt, Bound: -1}
	}
	return Invalid
}

// systemCall binds a call of a system function (LRM 20, 21). Arguments are
// self-determined; some, like $bits, accept a type.
func (b *binder) systemCall(e ast.Expr, name string, args []ast.Expr) BoundExpr {
	n := &BCall{exprInfo: exprInfo{Invalid, e}, Name: name}
	n.Args = b.bindArgsSelf(args)
	first := Type(Invalid)
	if len(n.Args) > 0 {
		first = n.Args[0].Type()
		if tr, ok := n.Args[0].(*BTypeRef); ok {
			first = tr.T
		}
	}
	switch name {
	case "$signed", "$unsigned":
		if IsIntegral(first) {
			w, _ := Bits(first)
			n.typ = intT(w, name == "$signed", IsFourState(first))
		}
	case "$clog2", "$bits", "$size", "$left", "$right", "$low", "$high", "$increment", "$dimensions",
		"$unpacked_dimensions", "$countones", "$countbits", "$onehot", "$onehot0", "$isunknown", "$cast",
		"$test$plusargs", "$value$plusargs", "$rtoi", "$fopen", "$feof", "$fgetc", "$sscanf", "$fscanf", "$ferror":
		n.typ = typeInt
		if name == "$onehot" || name == "$onehot0" || name == "$isunknown" {
			n.typ = intT(1, false, false)
		}
	case "$random":
		n.typ = typeInt
	case "$urandom", "$urandom_range":
		n.typ = intT(32, false, false)
	case "$time":
		n.typ = typeTime
	case "$stime":
		n.typ = intT(32, false, false)
	case "$realtime", "$itor", "$bitstoreal", "$sqrt", "$pow", "$exp", "$ln", "$log10", "$sin", "$cos", "$tan",
		"$asin", "$acos", "$atan", "$atan2", "$sinh", "$cosh", "$tanh", "$asinh", "$acosh", "$atanh",
		"$floor", "$ceil", "$hypot":
		n.typ = typeReal
	case "$realtobits":
		n.typ = intT(64, false, false)
	case "$shortrealtobits":
		n.typ = intT(32, false, false)
	case "$bitstoshortreal":
		n.typ = typeShortr
	case "$typename", "$sformatf", "$psprintf":
		n.typ = String
	}
	return n
}
