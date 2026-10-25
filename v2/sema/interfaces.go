package sema

import (
	"strings"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
)

// typeText renders a type as written, for comparing two declarations: equal
// text means the same type here, and different text is treated as different.
func typeText(t ast.Type) string {
	var sb strings.Builder
	if t.Virtual {
		sb.WriteString("virtual ")
	}
	if t.PackageQualifier != "" {
		sb.WriteString(t.PackageQualifier + "::")
	}
	sb.WriteString(t.Name)
	if len(t.ParamOverrides) > 0 {
		sb.WriteString("#(")
		for _, p := range t.ParamOverrides {
			sb.WriteString(p.Name + "=")
			if p.ValueExpr != nil {
				sb.WriteString(ast.ExprString(p.ValueExpr))
			}
			sb.WriteString(",")
		}
		sb.WriteString(")")
	}
	if t.Signed {
		sb.WriteString(" signed")
	}
	for _, d := range t.PackedDims {
		sb.WriteString("[")
		if d.LeftExpr != nil {
			sb.WriteString(ast.ExprString(d.LeftExpr))
		}
		sb.WriteString(":")
		if d.RightExpr != nil {
			sb.WriteString(ast.ExprString(d.RightExpr))
		}
		sb.WriteString("]")
	}
	return sb.String()
}

// signature is a method's return type and argument types as written.
func signature(ret ast.Type, args []ast.Arg) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = typeText(a.Type)
	}
	return typeText(ret) + "(" + strings.Join(parts, ",") + ")"
}

// ifaceMethod is a method an interface class declares, with the chain of
// specialisations it was reached through.
type ifaceMethod struct {
	name   string
	owner  *ast.Class
	sig    string
	spec   string // "ibase#(bit)": how the owner was named by the class that extends or implements it
	params bool   // the signature mentions one of the owner's parameters
}

// ifaceClass resolves a type reference to the interface class it names: the
// scope that class opens and its declaration.
func (c *checker) classRef(s *Scope, t ast.Type) (*Scope, *ast.Class) {
	if s == nil {
		return nil, nil
	}
	var res Result
	if t.PackageQualifier != "" {
		res = s.LookupQualified(append(strings.Split(t.PackageQualifier, "::"), t.Name))
	} else {
		res = s.Lookup(t.Name)
	}
	sym := c.m.classOf(res.Symbol, 0)
	if sym == nil {
		return nil, nil
	}
	cl, ok := sym.Decl.(*ast.Class)
	if !ok {
		return nil, nil
	}
	return sym.Child, cl
}

// collectMethods gathers the methods declared by the interface class cl and
// every interface class it extends.
func (c *checker) collectMethods(cs *Scope, cl *ast.Class, spec string, depth int, out *[]ifaceMethod) {
	if depth > 32 || cl == nil {
		return
	}
	paramNames := map[string]bool{}
	for _, p := range cl.Params {
		paramNames[p.Name] = true
	}
	add := func(name string, ret ast.Type, args []ast.Arg) {
		sig := signature(ret, args)
		dep := false
		for w := range paramNames {
			if strings.Contains(sig, w) {
				dep = true
			}
		}
		*out = append(*out, ifaceMethod{name: name, owner: cl, sig: sig, spec: spec, params: dep})
	}
	for _, d := range cl.Body {
		switch n := d.(type) {
		case *ast.Function:
			if n.Scope == "" {
				add(n.Name, n.ReturnType, n.Args)
			}
		case *ast.Task:
			if n.Scope == "" {
				add(n.Name, ast.Type{}, n.Args)
			}
		}
	}
	for _, ref := range cl.ExtendsTypes {
		if bs, bc := c.classRef(cs.Parent, ref); bc != nil && bs != cs {
			c.collectMethods(bs, bc, typeText(ref), depth+1, out)
		}
	}
}

// interfaceRules checks a class against the rules for interface classes of
// LRM 8.26: a class cannot implement a type parameter, the interface classes
// it implements (or an interface class extends) must not give one method name
// two different prototypes (8.26.6.1, 8.26.6.3), and an interface class that
// extends several must resolve a parameter or type name more than one of them
// declares (8.26.6.2).
func (c *checker) interfaceRules(cs *Scope, cl *ast.Class) {
	if cs == nil {
		return
	}
	for _, t := range cl.ImplementsTypes {
		if t.PackageQualifier == "" {
			if sym := cs.Lookup(t.Name).Symbol; sym != nil && sym.Kind == SymTypeParam {
				c.errorf(diag.CodeClassRule, t.Pos(), "class %q cannot implement the type parameter %q (LRM 8.26)", cl.Name, t.Name)
			}
		}
	}
	var bases []ast.Type
	bases = append(bases, cl.ImplementsTypes...)
	if cl.IsInterfaceClass {
		bases = append(bases, cl.ExtendsTypes...)
	}
	if len(bases) < 2 {
		return
	}
	var all []ifaceMethod
	for _, ref := range bases {
		if bs, bc := c.classRef(cs.Parent, ref); bc != nil && bc.IsInterfaceClass && bs != cs {
			c.collectMethods(bs, bc, typeText(ref), 0, &all)
		}
	}
	reported := map[string]bool{}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			a, b := all[i], all[j]
			if a.name != b.name || reported[a.name] {
				continue
			}
			conflict := false
			if a.owner == b.owner {
				conflict = a.spec != b.spec && (a.params || b.params)
			} else {
				conflict = a.sig != b.sig
			}
			if conflict {
				reported[a.name] = true
				c.errorf(diag.CodeClassRule, cl.Pos(), "class %q inherits two different prototypes of method %q (%s, %s) (LRM 8.26.6)", cl.Name, a.name, a.owner.Name, b.owner.Name)
			}
		}
	}
	if cl.IsInterfaceClass && len(cl.ExtendsTypes) > 1 {
		c.typeConflicts(cs, cl)
	}
}

// typeConflicts reports a parameter or type name that two of the interface
// classes cl extends declare and cl itself does not (LRM 8.26.6.2).
func (c *checker) typeConflicts(cs *Scope, cl *ast.Class) {
	own := map[string]bool{}
	for i := range cl.Params {
		own[cl.Params[i].Name] = true
	}
	for _, d := range cl.Body {
		switch n := d.(type) {
		case *ast.Typedef:
			own[n.Name] = true
		case *ast.Parameter:
			own[n.Name] = true
		}
	}
	seen := map[string]ast.Node{}
	reported := map[string]bool{}
	for _, ref := range cl.ExtendsTypes {
		bs, bc := c.classRef(cs.Parent, ref)
		if bc == nil || bs == cs {
			continue
		}
		decls := map[string]ast.Node{}
		var walk func(k *ast.Class, ks *Scope, depth int)
		walk = func(k *ast.Class, ks *Scope, depth int) {
			if depth > 32 || k == nil {
				return
			}
			for i := range k.Params {
				decls[k.Params[i].Name] = &k.Params[i]
			}
			for _, d := range k.Body {
				switch n := d.(type) {
				case *ast.Typedef:
					decls[n.Name] = n
				case *ast.Parameter:
					decls[n.Name] = n
				}
			}
			for _, r := range k.ExtendsTypes {
				if s2, c2 := c.classRef(ks.Parent, r); c2 != nil && s2 != ks {
					walk(c2, s2, depth+1)
				}
			}
		}
		walk(bc, bs, 0)
		for name, node := range decls {
			if prev, ok := seen[name]; ok && prev != node && !own[name] && !reported[name] {
				reported[name] = true
				c.errorf(diag.CodeClassRule, cl.Pos(), "class %q inherits a different %q from each base; declare %q in the class to resolve it (LRM 8.26.6.2)", cl.Name, name, name)
			}
			seen[name] = node
		}
	}
}

// memberTypes reports a type used in a class member's declaration that nothing
// in scope declares. A class sees its own parameters and typedefs and its base
// classes', not those of the interface classes it implements (LRM 8.26.3).
func (c *checker) memberTypes(cs *Scope, cl *ast.Class) {
	check := func(s *Scope, t *ast.Type) {
		if s == nil || t.Name == "" || t.Inline != nil {
			return
		}
		_, ds := c.m.ResolveType(s, t)
		for _, d := range ds {
			if d.Code == diag.CodeUndeclared {
				c.set.Add(d)
			}
		}
	}
	for _, d := range cl.Body {
		switch n := d.(type) {
		case *ast.Function:
			fs := c.scopeOf(n, cs)
			check(fs, &n.ReturnType)
			for i := range n.Args {
				check(fs, &n.Args[i].Type)
			}
		case *ast.Task:
			fs := c.scopeOf(n, cs)
			for i := range n.Args {
				check(fs, &n.Args[i].Type)
			}
		case *ast.Variable:
			check(cs, &n.Type)
		}
	}
}
