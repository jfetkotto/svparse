package ast_test

import (
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/v2/ast"
)

func null() *ast.Null { return &ast.Null{} }

func assign(op string, lhs, rhs ast.Expr) *ast.Assign {
	return &ast.Assign{Op: op, LHS: lhs, RHS: rhs}
}

func intVar(name string, init ast.Expr) *ast.Variable {
	return &ast.Variable{Name: name, Type: ast.Type{Name: "int"}, InitialExpr: init}
}

// stmtCases has at least one node of every statement and procedural-item type,
// with its expected Dump. It drives the dump golden test and the Walk
// completeness check, and TestEveryStmtTypeIsExercised keeps it complete
// against stmt.go.
var stmtCases = []struct {
	name string
	node ast.Node
	want string
}{
	{"begin block with label and declaration",
		&ast.Block{Label: "main", EndLabel: "main", Decls: []ast.Decl{intVar("t", nil)}, Stmts: []ast.Stmt{null()}},
		"(block begin label=main end=main\n  (variable t <int>)\n  (null))\n"},
	{"fork join_any", &ast.Block{Fork: true, Join: "join_any", Stmts: []ast.Stmt{null(), null()}},
		"(block fork join_any\n  (null)\n  (null))\n"},
	{"fork join", &ast.Block{Fork: true, Join: "join"}, "(block fork join)\n"},
	{"labeled", &ast.Labeled{Label: "done", Body: null()}, "(labeled done\n  (null))\n"},
	{"if unique with else",
		&ast.If{Qualifier: "unique", Cond: &ast.Binary{Op: "==", X: id("a"), Y: lit("1")}, Then: assign("=", id("x"), lit("1")), Else: assign("=", id("x"), lit("2"))},
		"(if unique cond=(== a 1)\n  (assign = x 1)\n  (assign = x 2))\n"},
	{"if without else", &ast.If{Cond: id("c"), Then: null()}, "(if cond=c\n  (null))\n"},
	{"case with items",
		&ast.Case{Qualifier: "priority", Kind: "casez", Inside: true, Subject: id("sel"), Items: []ast.CaseItem{
			{Exprs: []ast.Expr{lit("1"), lit("2")}, Body: assign("=", id("y"), lit("1"))},
			{Default: true, Body: null()},
		}},
		"(casez priority inside subject=sel\n  (item 1 2\n    (assign = y 1))\n  (item default\n    (null)))\n"},
	{"for with assignment init",
		&ast.For{Init: []ast.Stmt{assign("=", id("i"), lit("0"))}, Cond: &ast.Binary{Op: "<", X: id("i"), Y: lit("4")},
			Step: []ast.Stmt{&ast.ExprStmt{X: &ast.IncDec{Op: "++", X: id("i")}}}, Body: null()},
		"(for init=[(assign = i 0)] cond=(< i 4) step=[(expr (post++ i))]\n  (null))\n"},
	{"for with declaration init", &ast.For{InitDecls: []ast.Decl{intVar("i", lit("0"))}, Cond: id("c"), Body: null()},
		"(for cond=c\n  (variable i <int> = 0)\n  (null))\n"},
	{"foreach with a skipped dimension", &ast.Foreach{Array: id("arr"), Vars: []string{"i", "", "j"}, Body: null()},
		"(foreach arr vars=[i _ j]\n  (null))\n"},
	{"while", &ast.While{Cond: id("c"), Body: null()}, "(while cond=c\n  (null))\n"},
	{"do while", &ast.DoWhile{Body: null(), Cond: id("c")}, "(do-while cond=c\n  (null))\n"},
	{"repeat", &ast.Repeat{Count: lit("3"), Body: null()}, "(repeat count=3\n  (null))\n"},
	{"forever", &ast.Forever{Body: null()}, "(forever\n  (null))\n"},
	{"timed by event",
		&ast.TimedStmt{Control: &ast.TimingControl{Events: []ast.EventTerm{{Edge: "posedge", X: id("clk")}, {Edge: "negedge", X: id("rst")}}},
			Body: assign("<=", id("q"), id("d"))},
		"(timed @(posedge clk or negedge rst)\n  (assign <= q d))\n"},
	{"timed by delay", &ast.TimedStmt{Control: &ast.TimingControl{Delay: lit("10")}, Body: null()}, "(timed #10\n  (null))\n"},
	{"timed by cycle delay", &ast.TimedStmt{Control: &ast.TimingControl{Delay: lit("2"), CycleDelay: true}, Body: null()}, "(timed ##2\n  (null))\n"},
	{"timed by star", &ast.TimedStmt{Control: &ast.TimingControl{Star: true}, Body: null()}, "(timed @*\n  (null))\n"},
	{"event with iff", &ast.TimedStmt{Control: &ast.TimingControl{Events: []ast.EventTerm{{X: id("a"), Iff: id("en")}}}, Body: null()},
		"(timed @(a iff en)\n  (null))\n"},
	{"wait", &ast.Wait{Cond: id("ready"), Body: null()}, "(wait cond=ready\n  (null))\n"},
	{"wait fork", &ast.WaitFork{}, "(wait-fork)\n"},
	{"wait order with else", &ast.WaitOrder{Events: []ast.Expr{id("a"), id("b")}, Then: null(), Else: null()},
		"(wait-order a b\n  (null)\n  (null))\n"},
	{"event trigger", &ast.EventTrigger{Event: id("ev")}, "(trigger -> ev)\n"},
	{"nonblocking event trigger with delay",
		&ast.EventTrigger{Nonblocking: true, Control: &ast.TimingControl{Delay: lit("5")}, Event: id("ev")},
		"(trigger ->> #5 ev)\n"},
	{"blocking assign", assign("=", id("a"), &ast.Binary{Op: "+", X: id("b"), Y: lit("1")}), "(assign = a (+ b 1))\n"},
	{"compound assign", assign("+=", id("a"), lit("2")), "(assign += a 2)\n"},
	{"intra-assignment delay",
		&ast.Assign{Op: "<=", LHS: id("q"), Control: &ast.TimingControl{Delay: lit("2")}, RHS: id("d")},
		"(assign <= q d ctl=#2)\n"},
	{"intra-assignment repeat event",
		&ast.Assign{Op: "=", LHS: id("q"), Control: &ast.TimingControl{Repeat: lit("3"), Events: []ast.EventTerm{{X: id("clk")}}}, RHS: id("d")},
		"(assign = q d ctl=repeat(3) @(clk))\n"},
	{"force", &ast.ProcAssign{Kind: "force", LHS: id("a"), RHS: lit("1")}, "(proc-force a 1)\n"},
	{"release", &ast.ProcAssign{Kind: "release", LHS: id("a")}, "(proc-release a)\n"},
	{"expression statement", &ast.ExprStmt{X: &ast.Call{Func: id("f"), Args: []ast.Expr{id("x")}}}, "(expr (call f x))\n"},
	{"return with value", &ast.Return{Value: id("x")}, "(return x)\n"},
	{"return", &ast.Return{}, "(return)\n"},
	{"break", &ast.Break{}, "(break)\n"},
	{"continue", &ast.Continue{}, "(continue)\n"},
	{"disable", &ast.Disable{Target: id("blk")}, "(disable blk)\n"},
	{"disable fork", &ast.Disable{Fork: true}, "(disable-fork)\n"},
	{"deferred assert with else", &ast.ImmediateAssert{Kind: "assert", Deferred: "#0", Cond: id("a"), Then: null(), Else: null()},
		"(assert #0 cond=a\n  (null)\n  (null))\n"},
	{"cover", &ast.ImmediateAssert{Kind: "cover", Cond: id("a"), Then: null()}, "(cover cond=a\n  (null))\n"},
	{"randcase", &ast.RandCase{Items: []ast.RandCaseItem{{Weight: lit("3"), Body: null()}}},
		"(randcase\n  (item 3\n    (null)))\n"},
	{"raw statement", &ast.RawStmt{Tokens: tk("a", "matches", "b")}, "(raw \"a matches b\")\n"},
	{"bad statement", &ast.BadStmt{Tokens: tk("?")}, "(bad \"?\")\n"},
	{"initial", &ast.Initial{Body: null()}, "(initial\n  (null))\n"},
	{"final", &ast.Final{Body: null()}, "(final\n  (null))\n"},
	{"always_ff", &ast.Always{Kind: "always_ff", Body: &ast.TimedStmt{Control: &ast.TimingControl{Events: []ast.EventTerm{{Edge: "posedge", X: id("clk")}}}, Body: null()}},
		"(always_ff\n  (timed @(posedge clk)\n    (null)))\n"},
	{"continuous assign with strength and delay",
		&ast.ContinuousAssign{DriveStrength: []string{"strong0", "weak1"}, Delay: []ast.Expr{lit("2")},
			Assigns: []ast.Assign{*assign("=", id("a"), id("b")), *assign("=", id("c"), id("d"))}},
		"(continuous-assign strength=(strong0,weak1) delay=[2]\n  (assign = a b)\n  (assign = c d))\n"},
}

func TestStmtDump(t *testing.T) {
	for _, c := range stmtCases {
		if got := ast.Dump(c.node); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}

// fileHoldingAllStmts returns a File in which every case is reachable: each
// declaration case directly in a module body, each statement case as the body of
// an initial block.
func fileHoldingAllStmts() *ast.File {
	var body []ast.Decl
	for _, c := range stmtCases {
		switch n := c.node.(type) {
		case ast.Decl:
			body = append(body, n)
		case ast.Stmt:
			body = append(body, &ast.Initial{Body: n})
		}
	}
	return &ast.File{Path: "stmts.sv", Decls: []ast.Decl{&ast.Container{Name: "m", Body: body}}}
}

func TestNonblockingAccessor(t *testing.T) {
	if !assign("<=", id("a"), id("b")).Nonblocking() || assign("=", id("a"), id("b")).Nonblocking() || assign("<<=", id("a"), id("b")).Nonblocking() {
		t.Error("Nonblocking() must be true for <= only")
	}
}

func TestStmtWalkOrderIsSourceOrder(t *testing.T) {
	s := &ast.For{
		InitDecls: []ast.Decl{intVar("i", lit("0"))},
		Cond:      &ast.Binary{Op: "<", X: id("i"), Y: id("n")},
		Step:      []ast.Stmt{&ast.ExprStmt{X: &ast.IncDec{Op: "++", X: id("i")}}},
		Body:      &ast.Block{Stmts: []ast.Stmt{assign("=", id("a"), id("b"))}},
	}
	var kinds []string
	ast.Inspect(s, func(n ast.Node) bool {
		if n != nil {
			kinds = append(kinds, reflect.TypeOf(n).Elem().Name())
		}
		return true
	})
	// A Variable's Type is walked before its initializer expression.
	want := []string{"For", "Variable", "Type", "Literal", "Binary", "Ident", "Ident", "ExprStmt", "IncDec", "Ident", "Block", "Assign", "Ident", "Ident"}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("visit order\n got: %v\nwant: %v", kinds, want)
	}
}

func TestStmtPositions(t *testing.T) {
	a := assign("=", id("a"), id("b"))
	a.Position = ast.Position{Line: 2, Character: 4, EndLine: 2, EndCharacter: 10}
	if got, want := ast.DumpWithPositions(a), "(assign = a b @2:4-2:10)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if a.Pos() != a.Position {
		t.Error("Pos() must return the embedded Position")
	}
}

// stmtNodeTypes reads stmt.go and returns every struct type that embeds
// stmtBase or declBase, i.e. every statement and procedural-item node.
func stmtNodeTypes(t *testing.T) []string {
	t.Helper()
	f, err := goparser.ParseFile(gotoken.NewFileSet(), "stmt.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing stmt.go: %v", err)
	}
	var names []string
	for _, d := range f.Decls {
		gd, ok := d.(*goast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*goast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*goast.StructType)
			if !ok {
				continue
			}
			for _, field := range st.Fields.List {
				if ident, ok := field.Type.(*goast.Ident); ok && len(field.Names) == 0 && (ident.Name == "stmtBase" || ident.Name == "declBase") {
					names = append(names, ts.Name.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// TestEveryStmtTypeIsExercised keeps stmtCases honest: adding a statement node
// to stmt.go without a dump/walk case here fails.
func TestEveryStmtTypeIsExercised(t *testing.T) {
	seen := make(map[string]bool)
	checkWalkComplete(t, fileHoldingAllStmts(), seen)
	types := stmtNodeTypes(t)
	if len(types) < 30 {
		t.Fatalf("found only %d statement types in stmt.go (%v): the scan is broken", len(types), types)
	}
	for _, name := range types {
		if !seen[name] {
			t.Errorf("statement type %s is not exercised by stmtCases", name)
		}
	}
}

func TestDumpNeverFallsBackForStmts(t *testing.T) {
	for _, c := range stmtCases {
		if got := ast.Dump(c.node); strings.Contains(got, "UNHANDLED") {
			t.Errorf("%s: dump hit the fallback: %s", c.name, got)
		}
	}
}
