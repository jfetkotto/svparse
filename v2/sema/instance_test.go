package sema

import "testing"

func TestInstanceBindsLikeBindIn(t *testing.T) {
	m, c := model(t, "module top #(parameter W = 4); logic [W-1:0] v; logic [W:0] w; endmodule")
	sc := defScope(t, m, c, "top")
	widthOf := func(env *Env) []int {
		in := m.NewInstance(env)
		var out []int
		// The same Instance serves several expressions, and a second binding of
		// the same name finds its type cached.
		for _, src := range []string{"v", "w", "v + w", "v"} {
			be, ds := in.Bind(sc, parseExprText(t, src))
			if len(ds) != 0 {
				t.Fatalf("%s: %v", src, ds)
			}
			ref, _ := m.BindIn(sc, parseExprText(t, src), env)
			bw, _ := Bits(be.Type())
			rw, _ := Bits(ref.Type())
			if bw != rw {
				t.Errorf("%s: Instance gives %d bits, BindIn %d", src, bw, rw)
			}
			out = append(out, bw)
		}
		return out
	}
	if got := widthOf(nil); got[0] != 4 || got[1] != 5 || got[2] != 5 {
		t.Errorf("defaults: %v", got)
	}
	param := sc.Lookup("W").Symbol
	env := &Env{Params: map[*Symbol]Const{param: mustEval(t, m, sc, "12")}}
	if got := widthOf(env); got[0] != 12 || got[1] != 13 || got[3] != 12 {
		t.Errorf("W=12: %v", got)
	}
}

// Two instances of one module may differ: the cache must not leak between
// Instances.
func TestInstancesDoNotShareTypes(t *testing.T) {
	m, c := model(t, "module top #(parameter W = 4); logic [W-1:0] v; endmodule")
	sc := defScope(t, m, c, "top")
	param := sc.Lookup("W").Symbol
	a := m.NewInstance(&Env{Params: map[*Symbol]Const{param: mustEval(t, m, sc, "3")}})
	b := m.NewInstance(&Env{Params: map[*Symbol]Const{param: mustEval(t, m, sc, "9")}})
	ba, _ := a.Bind(sc, parseExprText(t, "v"))
	bb, _ := b.Bind(sc, parseExprText(t, "v"))
	wa, _ := Bits(ba.Type())
	wb, _ := Bits(bb.Type())
	if wa != 3 || wb != 9 {
		t.Fatalf("widths %d and %d, want 3 and 9", wa, wb)
	}
}
