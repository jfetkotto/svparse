package elab

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
	"github.com/jfetkotto/svparse/v2/diag"
	"github.com/jfetkotto/svparse/v2/parser"
	"github.com/jfetkotto/svparse/v2/preprocessor"
	"github.com/jfetkotto/svparse/v2/sema"
)

func compile(t testing.TB, files map[string]string) *sema.Compilation {
	t.Helper()
	c := sema.NewCompilation(sema.Options{})
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		toks, _ := preprocessor.Preprocess(p, files[p], nil)
		f, errs := parser.Parse(p, toks)
		if len(errs) != 0 {
			t.Fatalf("%s: parse errors %+v", p, errs)
		}
		c.AddFile(f)
	}
	return c
}

func elaborate(t testing.TB, src string, opts Options) (*Design, []diag.Diagnostic) {
	t.Helper()
	return Elaborate(context.Background(), compile(t, map[string]string{"t.sv": src}), opts)
}

func mustElab(t testing.TB, src string, opts Options) *Design {
	t.Helper()
	d, ds := elaborate(t, src, opts)
	if len(ds) != 0 {
		t.Fatalf("diagnostics: %v", ds)
	}
	return d
}

// tree prints the hierarchy as indented "path kind" lines.
func tree(d *Design) string {
	var sb strings.Builder
	var rec func(n *Node, depth int)
	rec = func(n *Node, depth int) {
		kind := ""
		if n.Def != nil {
			kind = " : " + n.Def.Name
		}
		sb.WriteString(strings.Repeat("  ", depth) + n.Name + kind + "\n")
		for _, c := range n.Children {
			rec(c, depth+1)
		}
	}
	for _, t := range d.Tops {
		rec(t, 0)
	}
	return sb.String()
}

func paramStr(n *Node, name string) string {
	p, ok := n.Param(name)
	if !ok {
		return "<none>"
	}
	if p.IsType {
		if p.TypeValue == nil {
			return "<notype>"
		}
		return p.TypeValue.String()
	}
	if !p.Valid {
		return "<invalid>"
	}
	return p.Value.String()
}

func TestHierarchyAndTops(t *testing.T) {
	d := mustElab(t, `module top; leaf a(); mid m(); endmodule
module mid; leaf b(), c(); endmodule
module leaf; endmodule
module other_top; endmodule`, Options{})
	want := `top
  a : leaf
  m : mid
    b : leaf
    c : leaf
other_top
`
	want = strings.ReplaceAll(want, "top\n  a", "top : top\n  a")
	want = strings.ReplaceAll(want, "other_top\n", "other_top : other_top\n")
	want = strings.Replace(want, "    b : leaf", "    b : leaf", 1)
	got := tree(d)
	if !strings.Contains(got, "top : top\n  a : leaf\n  m : mid\n    b : leaf\n    c : leaf\n") || !strings.Contains(got, "other_top : other_top\n") {
		t.Errorf("tree:\n%s", got)
	}
	if d.Count != 6 {
		t.Errorf("Count = %d", d.Count)
	}
	if n := d.Find("top.m.c"); n == nil || n.Path() != "top.m.c" || n.Def.Name != "leaf" || n.Parent.Name != "m" {
		t.Errorf("Find: %v", n)
	}
	if d.Find("top.nope") != nil || d.Find("nope") != nil {
		t.Error("Find of a missing path")
	}
	visited := 0
	d.Walk(func(*Node) bool { visited++; return true })
	if visited != 6 {
		t.Errorf("Walk visited %d", visited)
	}
	// Explicit tops replace the detected ones.
	d2 := mustElab(t, `module top; leaf a(); endmodule module leaf; endmodule`, Options{Tops: []string{"leaf"}})
	if len(d2.Tops) != 1 || d2.Tops[0].Name != "leaf" || len(d2.Tops[0].Children) != 0 {
		t.Errorf("explicit top: %s", tree(d2))
	}
}

func TestParameterResolution(t *testing.T) {
	d := mustElab(t, `module top;
  leaf #(4) pos();
  leaf #(.W(6), .D(3)) named();
  leaf #(.W(8)) dep();
  leaf dflt();
  typed #(.R(2), .B(300)) ty();
  untypedm #(4'd3) ut();
  hdr #(.A(1)) hd();
endmodule
module leaf #(parameter W = 2, parameter D = W * 2);
  localparam L = D + 1;
endmodule
module typed #(parameter real R = 1.5, parameter byte B = 1, parameter string S = "s"); endmodule
module untypedm #(parameter P = 8); endmodule
module hdr #(parameter A = 0); parameter BODY = 5; localparam LP = A + BODY; endmodule`, Options{})
	for path, want := range map[string]string{
		"top.pos.W": "32'sh00000004", "top.pos.D": "32'sh00000008", "top.pos.L": "32'sh00000009",
		"top.named.W": "32'sh00000006", "top.named.D": "32'sh00000003", "top.named.L": "32'sh00000004",
		"top.dep.D": "32'sh00000010", "top.dflt.W": "32'sh00000002", "top.dflt.L": "32'sh00000005",
		"top.ty.R": "2", "top.ty.B": "8'sh2c", "top.ty.S": `"s"`,
		"top.ut.P": "4'h3", // an untyped parameter takes the type of its value
		"top.hd.A": "32'sh00000001", "top.hd.BODY": "32'sh00000005", "top.hd.LP": "32'sh00000006",
	} {
		i := strings.LastIndex(path, ".")
		n := d.Find(path[:i])
		if n == nil {
			t.Fatalf("no node %s", path[:i])
		}
		if got := paramStr(n, path[i+1:]); got != want {
			t.Errorf("%s = %s, want %s", path, got, want)
		}
	}
	p, _ := d.Find("top.pos").Param("W")
	if !p.Overridden || p.Local {
		t.Errorf("W: %+v", p)
	}
	if l, _ := d.Find("top.pos").Param("L"); !l.Local {
		t.Error("L is local")
	}
	if b, _ := d.Find("top.hd").Param("BODY"); !b.Local {
		t.Error("a body parameter of a module with a parameter port list is local")
	}
	if dd, _ := d.Find("top.dflt").Param("W"); dd.Overridden {
		t.Error("default is not an override")
	}
}

func TestOverridesSeeTheirOwnScope(t *testing.T) {
	// The value is written in the parent: it uses the parent's parameters, not
	// the child's.
	d := mustElab(t, `module top #(parameter W = 3);
  leaf #(.W(W + 10), .D(W)) u();
endmodule
module leaf #(parameter W = 1, parameter D = 1); endmodule`, Options{})
	u := d.Find("top.u")
	if paramStr(u, "W") != "32'sh0000000d" || paramStr(u, "D") != "32'sh00000003" {
		t.Errorf("W=%s D=%s", paramStr(u, "W"), paramStr(u, "D"))
	}
	// Overriding a top's own parameter is not possible, but its default is its value.
	if paramStr(d.Tops[0], "W") != "32'sh00000003" {
		t.Errorf("top W = %s", paramStr(d.Tops[0], "W"))
	}
	// Parameters flow down several levels.
	d2 := mustElab(t, `module top; a #(.N(2)) u(); endmodule
module a #(parameter N = 1); b #(.M(N * 3)) v(); endmodule
module b #(parameter M = 1); c #(M + 1) w(); endmodule
module c #(parameter K = 0); endmodule`, Options{})
	if got := paramStr(d2.Find("top.u.v.w"), "K"); got != "32'sh00000007" {
		t.Errorf("K = %s", got)
	}
}

func TestDefparam(t *testing.T) {
	d := mustElab(t, `module top;
  defparam u.W = 7;
  defparam m.inner.W = 9;
  defparam m.D = 4;
  leaf u();
  leaf #(.W(1)) v();
  mid m();
  defparam v.W = 2;
endmodule
module mid #(parameter D = 1); leaf inner(); endmodule
module leaf #(parameter W = 1); endmodule`, Options{})
	for path, want := range map[string]string{
		"top.u": "32'sh00000007", "top.v": "32'sh00000002", // a defparam beats a #() override
		"top.m.inner": "32'sh00000009",
	} {
		if got := paramStr(d.Find(path), "W"); got != want {
			t.Errorf("%s.W = %s, want %s", path, got, want)
		}
	}
	if got := paramStr(d.Find("top.m"), "D"); got != "32'sh00000004" {
		t.Errorf("m.D = %s", got)
	}
	// A defparam written in a child reaches only below it, and a defparam of an
	// unknown parameter is reported.
	_, ds := elaborate(t, "module top; defparam u.NOPE = 1; leaf u(); endmodule module leaf #(parameter W = 1); endmodule", Options{})
	if len(ds) != 1 || ds[0].Code != diag.CodeUnknownParameter {
		t.Errorf("defparam to an unknown parameter: %v", ds)
	}
}

func TestInstanceArrays(t *testing.T) {
	d := mustElab(t, `module top #(parameter N = 3);
  leaf a[N-1:0] ();
  leaf b[2] ();
  leaf c[0:1][1:0] ();
  leaf s();
endmodule
module leaf; endmodule`, Options{})
	var names []string
	for _, c := range d.Tops[0].Children {
		names = append(names, c.Name)
	}
	want := "a[2],a[1],a[0],b[0],b[1],c[0][1],c[0][0],c[1][1],c[1][0],s"
	if strings.Join(names, ",") != want {
		t.Errorf("elements:\n got %s\nwant %s", strings.Join(names, ","), want)
	}
	if d.Find("top.a[1]") == nil {
		t.Error("Find with an index")
	}
	if d.Count != 11 {
		t.Errorf("Count = %d", d.Count)
	}
}

func TestGenerateIfAndCase(t *testing.T) {
	d := mustElab(t, `module top #(parameter MODE = 1, parameter W = 8);
  if (MODE == 0) begin : zero leaf a(); end
  else if (MODE == 1) begin : one leaf b(); end
  else begin : other leaf c(); end

  if (W > 4) leaf wide(); else leaf narrow();

  case (MODE)
    0: leaf z0();
    1, 2: begin : cs leaf z12(); end
    default: leaf zd();
  endcase

  generate
    if (W == 8) begin leaf unnamed1(); end
    if (W == 9) begin leaf never(); end
    if (W == 8) begin leaf unnamed2(); end
  endgenerate
endmodule
module leaf; endmodule`, Options{})
	want := `top : top
  one
    b : leaf
  genblk2
    wide : leaf
  cs
    z12 : leaf
  genblk4
    unnamed1 : leaf
  genblk6
    unnamed2 : leaf
`
	if got := tree(d); got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
	// Changing the parameter changes the structure.
	d2 := mustElab(t, strings.ReplaceAll(`module top #(parameter MODE = 7);
  if (MODE == 0) begin : zero leaf a(); end
  else if (MODE == 1) begin : one leaf b(); end
  else begin : other leaf c(); end
  case (MODE) 0: leaf z0(); default: leaf zd(); endcase
endmodule
module leaf; endmodule`, "MODE = 7", "MODE = 7"), Options{})
	if got := tree(d2); !strings.Contains(got, "other\n    c : leaf") || !strings.Contains(got, "genblk2\n    zd : leaf") {
		t.Errorf("tree:\n%s", got)
	}
}

func TestGenerateFor(t *testing.T) {
	d := mustElab(t, `module top #(parameter N = 3);
  genvar i;
  for (i = 0; i < N; i++) begin : g
    leaf #(.ID(i)) u();
    localparam SQ = i * i;
  end
  for (genvar j = N; j > 0; j -= 2) begin
    leaf #(.ID(j * 10)) v();
  end
  for (genvar a = 0; a < 2; a++) begin : outer
    for (genvar b = 0; b < 2; b++) begin : inner
      leaf #(.ID(a * 2 + b)) w();
    end
  end
  for (i = 8; i > 0; i = i / 2) begin : half
    leaf z();
  end
endmodule
module leaf #(parameter ID = 0); endmodule`, Options{})
	want := `top : top
  g[0]
    u : leaf
  g[1]
    u : leaf
  g[2]
    u : leaf
  genblk2[3]
    v : leaf
  genblk2[1]
    v : leaf
  outer[0]
    inner[0]
      w : leaf
    inner[1]
      w : leaf
  outer[1]
    inner[0]
      w : leaf
    inner[1]
      w : leaf
  half[8]
    z : leaf
  half[4]
    z : leaf
  half[2]
    z : leaf
  half[1]
    z : leaf
`
	if got := tree(d); got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
	for path, want := range map[string]string{
		"top.g[2].u": "32'sh00000002", "top.genblk2[3].v": "32'sh0000001e", "top.genblk2[1].v": "32'sh0000000a",
		"top.outer[1].inner[0].w": "32'sh00000002", "top.outer[1].inner[1].w": "32'sh00000003",
	} {
		if got := paramStr(d.Find(path), "ID"); got != want {
			t.Errorf("%s.ID = %s, want %s", path, got, want)
		}
	}
	// A block's own localparam depends on its genvar.
	if got := paramStr(d.Find("top.g[2]"), "SQ"); got != "32'sh00000004" {
		t.Errorf("SQ = %s", got)
	}
	if n := d.Find("top.g[2]"); n.Kind != KindBlock || !n.HasIndex || n.Index != 2 || n.Def != nil {
		t.Errorf("block node: %+v", n)
	}
	// The loop count follows the parameter.
	d2 := mustElab(t, strings.Replace(`module top #(parameter N = 3); genvar i; for (i = 0; i < N; i++) begin : g leaf u(); end endmodule module leaf; endmodule`, "N = 3", "N = 3", 1), Options{})
	if len(d2.Tops[0].Children) != 3 {
		t.Error("loop count")
	}
}

func TestElaborationErrors(t *testing.T) {
	cases := []struct {
		name, src string
		opts      Options
		want      []string
	}{
		{"unknown module", "module top; nosuch u(); endmodule", Options{}, []string{diag.CodeUnknownModule}},
		{"unknown parameter", "module top; leaf #(.NOPE(1)) u(); endmodule module leaf #(parameter W=1); endmodule", Options{}, []string{diag.CodeUnknownParameter}},
		{"too many positional", "module top; leaf #(1, 2) u(); endmodule module leaf #(parameter W=1); endmodule", Options{}, []string{diag.CodeTooManyParameters}},
		{"local override", "module top; leaf #(.L(1)) u(); endmodule module leaf; localparam L = 0; endmodule", Options{}, []string{diag.CodeLocalParameterOverride}},
		{"body param hidden by port list", "module top; leaf #(.B(1)) u(); endmodule module leaf #(parameter A = 0); parameter B = 0; endmodule", Options{}, []string{diag.CodeLocalParameterOverride}},
		{"direct recursion", "module top; top t(); endmodule", Options{Tops: []string{"top"}}, []string{diag.CodeRecursiveInstantiation}},
		{"mutual recursion", "module a; b u(); endmodule module b; a v(); endmodule", Options{Tops: []string{"a"}}, []string{diag.CodeRecursiveInstantiation}},
		{"parametrised recursion ends", `module top; rec #(.N(3)) r(); endmodule
module rec #(parameter N = 1); if (N > 0) begin : more rec #(.N(N - 1)) next(); end endmodule`, Options{}, nil},
		{"unbounded recursion", "module top; rec #(1) r(); endmodule module rec #(parameter N = 0); rec #(N + 1) next(); endmodule", Options{MaxDepth: 10}, []string{diag.CodeInstantiationDepth}},
		{"instance limit", "module top; leaf a[100] (); endmodule module leaf; endmodule", Options{MaxInstances: 50}, []string{diag.CodeInstanceLimit}},
		{"generate limit", "module top; genvar i; for (i = 0; i >= 0; i++) begin leaf u(); end endmodule module leaf; endmodule", Options{MaxGenerateIters: 20}, []string{diag.CodeGenerateLimit}},
		{"unknown top", "module top; endmodule", Options{Tops: []string{"nothing"}}, []string{diag.CodeUnknownTop}},
		{"no top", "module a; b u(); endmodule module b; a v(); endmodule", Options{}, []string{diag.CodeNoTop}},
		{"non-constant condition", "module top; logic v; if (v) begin leaf u(); end endmodule module leaf; endmodule", Options{}, []string{diag.CodeNotConstant}},
		{"bad override value", "module top; leaf #(.W(nosuch)) u(); endmodule module leaf #(parameter W = 1); endmodule", Options{}, []string{diag.CodeUndeclared}},
		{"genvar misuse", "module top; int i; for (i = 0; i < 2; i++) begin leaf u(); end endmodule module leaf; endmodule", Options{}, []string{diag.CodeUndeclared}},
	}
	for _, c := range cases {
		_, ds := elaborate(t, c.src, c.opts)
		var got []string
		for _, d := range ds {
			got = append(got, d.Code)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v, want %v\n%v", c.name, got, c.want, ds)
		}
	}
}

func TestTypeParameters(t *testing.T) {
	d := mustElab(t, `typedef int word_t;
module top; leaf #(.T(word_t), .U(logic [7:0])) a(); leaf b(); endmodule
module leaf #(parameter type T = byte, parameter type U = bit); endmodule`, Options{})
	if got := paramStr(d.Find("top.a"), "T"); got != "word_t" {
		t.Errorf("a.T = %s", got)
	}
	if got := paramStr(d.Find("top.a"), "U"); got != "logic [7:0]" {
		t.Errorf("a.U = %s", got)
	}
	if got := paramStr(d.Find("top.b"), "T"); got != "byte" {
		t.Errorf("b.T = %s", got)
	}
	if p, _ := d.Find("top.a").Param("T"); !p.IsType || !p.Overridden {
		t.Errorf("T: %+v", p)
	}
}

func TestCancellation(t *testing.T) {
	c := compile(t, map[string]string{"t.sv": "module top; genvar i; for (i = 0; i < 100000; i++) begin leaf u(); end endmodule module leaf; endmodule"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, _ := Elaborate(ctx, c, Options{})
	if d.Count > 5 {
		t.Errorf("a cancelled elaboration kept going: %d nodes", d.Count)
	}
	if ctx.Err() == nil {
		t.Error("ctx should report cancellation")
	}
	// A nil context is tolerated.
	if d, _ := Elaborate(nil, compile(t, map[string]string{"t.sv": "module top; endmodule"}), Options{}); len(d.Tops) != 1 {
		t.Error("nil context")
	}
}

func TestSignatureAndPaths(t *testing.T) {
	d := mustElab(t, `module top; genvar i; for (i = 0; i < 2; i++) begin : g leaf u[2] (); end endmodule module leaf; endmodule`, Options{})
	if d.Find("top.g[1].u[0]").Path() != "top.g[1].u[0]" {
		t.Error("path with indices")
	}
	var nilNode *Node
	if nilNode.Path() != "" {
		t.Error("nil path")
	}
	if got := splitPath("a[1].b[2][3].c"); strings.Join(got, "|") != "a[1]|b[2][3]|c" {
		t.Errorf("splitPath = %v", got)
	}
	_ = ast.Position{}
}

// Elaborate each vendored file as its own design: nothing may panic or run
// away, and the many small designs there elaborate cleanly.
func TestCorpusElaborates(t *testing.T) {
	total, nodes, withErrors := 0, 0, 0
	codes := map[string]int{}
	err := filepath.WalkDir("../testdata/sv-tests", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".sv" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		toks, _ := preprocessor.Preprocess(path, string(data), nil)
		f, _ := parser.Parse(path, toks)
		c := sema.NewCompilation(sema.Options{})
		c.AddFile(f)
		des, ds := Elaborate(context.Background(), c, Options{MaxInstances: 10000, MaxSteps: 100000})
		total++
		nodes += des.Count
		if len(ds) > 0 {
			withErrors++
		}
		for _, d := range ds {
			codes[d.Code]++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d files, %d nodes, %d with diagnostics: %v", total, nodes, withErrors, codes)
	if total < 900 || nodes < 500 {
		t.Fatalf("too little elaborated: %d files, %d nodes", total, nodes)
	}
}

func FuzzElaborate(f *testing.F) {
	f.Add("module top #(parameter N = 2); genvar i; for (i = 0; i < N; i++) begin : g leaf #(i) u(); end defparam g[0].u.W = 3; endmodule module leaf #(parameter W = 1); endmodule")
	f.Add("module a #(parameter N = 1); if (N < 4) a #(N + 1) next(); endmodule")
	f.Add("interface i; modport m(); endinterface module top; i b(); c u(.x(b.m), .*, .y); wire w; assign q = w; endmodule module c(i x, input y); endmodule bind c chk k(.*);")
	f.Add("module top; leaf u(1, 2, .p(3)); leaf v[2:0](.*); endmodule module leaf(input [3:0] p, q); endmodule bind top.u : x leaf z();")
	f.Add("module top; leaf u[3:0] (); case (1) 1: leaf v(); default: ; endcase endmodule module leaf #(parameter type T = int); endmodule")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		file, _ := parser.Parse("f.sv", toks)
		c := sema.NewCompilation(sema.Options{})
		c.AddFile(file)
		Elaborate(context.Background(), c, Options{MaxDepth: 12, MaxInstances: 2000, MaxGenerateIters: 200, MaxSteps: 20000})
	})
}

func TestIndexedPartSelectWidthMustBePositive(t *testing.T) {
	src := `module leaf #(parameter W = 4) (input [15:0] a, output [15:0] y);
  assign y[3 +: W] = a[8 -: W];
endmodule
module top; wire [15:0] a, y1, y2;
  leaf #(.W(4)) good (a, y1);
  leaf #(.W(0)) bad (a, y2);
endmodule`
	_, ds := elaborate(t, src, Options{Tops: []string{"top"}})
	var got []string
	for _, d := range ds {
		if d.Code == diag.CodeInvalidExpression {
			got = append(got, d.Message)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want an error for each of the two selects of the W=0 instance only, got %v", ds)
	}
}
