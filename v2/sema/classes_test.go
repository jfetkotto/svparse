package sema

import "testing"

func TestClassRules(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"valid", `module m;
  virtual class base; pure virtual function void p(); pure constraint k; endclass
  class impl extends base; virtual function void p(); endfunction constraint k { 1; } endclass
  interface class ic; pure virtual function void q(); endclass
  class c2 implements ic; virtual function void q(); endfunction endclass
  class ext; rand int b; extern constraint e; endclass
  constraint ext::e { b > 0; }
  class imp; constraint i; endclass
  base h; impl o;
  initial begin o = new; h = o; end
endmodule`, ""},
		{"abstract new", "module m; virtual class b; endclass b h; initial h = new; endmodule", "class-rule"},
		{"interface new", "module m; interface class i; endclass i h; initial h = new; endmodule", "class-rule"},
		{"pure in concrete class", "module m; class c; pure virtual task t; endclass endmodule", "class-rule"},
		{"unimplemented pure method", "module m; virtual class b; pure virtual function void p(); endclass class c extends b; endclass endmodule", "class-rule"},
		{"unimplemented pure constraint", "module m; virtual class b; pure constraint k; endclass class c extends b; endclass endmodule", "class-rule"},
		{"implemented in the middle", "module m; virtual class b; pure virtual function void p(); endclass virtual class mid extends b; virtual function void p(); endfunction endclass class c extends mid; endclass endmodule", ""},
		{"undefined extern constraint", "module m; class c; extern constraint k; endclass endmodule", "class-rule"},
		{"built-in method", "class c; function void randomize(); endfunction function int rand_mode(); return 1; endfunction endclass", "class-rule,class-rule"},
		{"randc rules", "class c; randc int b; rand int d; constraint k { soft b > 1; b dist {1, 2}; solve d before b; } endclass", "class-rule,class-rule,class-rule"},
		{"rand is fine", "class c; rand int b; constraint k { soft b > 1; b dist {1, 2}; } endclass", ""},
	}
	for _, c := range cases {
		if got := codes(checkSrc(t, c.src)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
