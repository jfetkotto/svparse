package sema

import "testing"

func TestInterfaceClassRules(t *testing.T) {
	const cr, un = "class-rule", "undeclared-identifier"
	cases := []struct {
		name, src, want string
	}{
		{"valid", `module m;
  interface class ia; typedef int int_t; pure virtual function void hello(); endclass
  interface class ib; pure virtual function void hello(); pure virtual function int get(int a); endclass
  interface class ic extends ia, ib; endclass
  class impl implements ia, ib; virtual function void hello(); endfunction virtual function int get(int a); return a; endfunction endclass
  interface class base #(type T = logic); pure virtual function void fn(T v); endclass
  interface class l extends base#(bit); endclass
  interface class r extends base#(bit); endclass
  interface class dia extends l, r; endclass
  class box #(type T = int); typedef T elem_t; function void put(T v, elem_t w); endfunction endclass
  class sub extends box; function void more(elem_t w); endfunction endclass
endmodule`, ""},
		{"implemented typedef is not inherited", `module m; interface class ia; typedef int int_t; pure virtual function void h(int_t v); endclass
  class c implements ia; virtual function void h(int_t v); endfunction endclass endmodule`, un},
		{"implements a type parameter", "module m; interface class ia; endclass class c #(type T = ia) implements T; endclass endmodule", cr},
		{"different return types", `module m; interface class ia; pure virtual function void h(); endclass interface class ib; pure virtual function int h(); endclass
  class c implements ia, ib; virtual function void h(); endfunction endclass endmodule`, cr},
		{"different argument types", `module m; interface class ia; pure virtual function void h(int a); endclass interface class ib; pure virtual function void h(string a); endclass
  interface class ic extends ia, ib; endclass endmodule`, cr},
		{"conflicting type parameter names", `module m; interface class i1 #(type T = logic); endclass interface class i2 #(type T = logic); endclass
  interface class i3 #(type TYPE = logic) extends i1#(TYPE), i2#(TYPE); endclass endmodule`, cr},
		{"resolved by redeclaring", `module m; interface class i1 #(type T = logic); endclass interface class i2 #(type T = logic); endclass
  interface class i3 #(type T = logic) extends i1#(T), i2#(T); endclass endmodule`, ""},
		{"different specialisations of one base", `module m; interface class base #(type T = logic); pure virtual function void fn(T v); endclass
  interface class l extends base#(bit); endclass interface class r extends base#(string); endclass interface class d extends l, r; endclass endmodule`, cr},
	}
	for _, c := range cases {
		if got := codes(checkSrc(t, c.src)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
