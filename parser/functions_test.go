package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestFunctionWithBodyAndReturnType(t *testing.T) {
	body := moduleBody(t, `function int add(int a, int b);
		some_var = a + b;
		return some_var;
	endfunction`)
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	fn := body[0].(*ast.Function)
	if fn.Name != "add" || fn.ReturnType.Name != "int" || fn.Prototype {
		t.Fatalf("unexpected function: %+v", fn)
	}
	if len(fn.Args) != 2 || fn.Args[0].Name != "a" || fn.Args[1].Name != "b" {
		t.Fatalf("unexpected args: %+v", fn.Args)
	}
}

func TestFunctionImplicitReturnType(t *testing.T) {
	body := moduleBody(t, "function foo(int x); endfunction")
	fn := body[0].(*ast.Function)
	if fn.Name != "foo" || fn.ReturnType.Name != "" {
		t.Fatalf("unexpected function: %+v", fn)
	}
}

func TestFunctionVoidReturnType(t *testing.T) {
	body := moduleBody(t, "function void proc(); endfunction")
	fn := body[0].(*ast.Function)
	if fn.ReturnType.Name != "void" {
		t.Fatalf("unexpected return type: %+v", fn.ReturnType)
	}
}

func TestFunctionEmptyBody(t *testing.T) {
	body := moduleBody(t, "function void nop(); endfunction")
	fn := body[0].(*ast.Function)
	if fn.Prototype {
		t.Fatalf("expected a real (empty) body, not a prototype: %+v", fn)
	}
}

func TestFunctionAutomaticLifetime(t *testing.T) {
	body := moduleBody(t, "function automatic int foo(); return 1; endfunction")
	fn := body[0].(*ast.Function)
	if fn.Name != "foo" || fn.ReturnType.Name != "int" {
		t.Fatalf("unexpected function: %+v", fn)
	}
}

func TestFunctionBodyWithNestedBeginEndDoesNotConfuseEndSearch(t *testing.T) {
	body := moduleBody(t, `function void foo();
		if (x) begin
			y = 1;
		end else begin
			y = 2;
		end
	endfunction
	function void bar(); endfunction`)
	if len(body) != 2 {
		t.Fatalf("expected 2 decls (foo and bar both found), got %+v", body)
	}
	if body[0].(*ast.Function).Name != "foo" || body[1].(*ast.Function).Name != "bar" {
		t.Fatalf("unexpected functions: %+v", body)
	}
}

func TestFunctionBodyWithCaseStatement(t *testing.T) {
	body := moduleBody(t, `function void foo();
		case (x)
			1: y = 1;
			2: y = 2;
		endcase
	endfunction
	function void bar(); endfunction`)
	if len(body) != 2 {
		t.Fatalf("expected foo and bar both found despite the nested case/endcase, got %+v", body)
	}
}

func TestFunctionArgDirectionsAndDefault(t *testing.T) {
	body := moduleBody(t, "function void foo(input int a, output int b, int c = 5); endfunction")
	fn := body[0].(*ast.Function)
	if len(fn.Args) != 3 {
		t.Fatalf("expected 3 args, got %+v", fn.Args)
	}
	if fn.Args[0].Direction != ast.DirInput || fn.Args[1].Direction != ast.DirOutput {
		t.Fatalf("unexpected directions: %+v", fn.Args[:2])
	}
	if fn.Args[2].Direction != ast.DirUnspecified || len(fn.Args[2].Default) != 1 || fn.Args[2].Default[0].Text != "5" {
		t.Fatalf("unexpected arg c: %+v", fn.Args[2])
	}
}

func TestFunctionNoArgs(t *testing.T) {
	body := moduleBody(t, "function void foo(); endfunction")
	fn := body[0].(*ast.Function)
	if len(fn.Args) != 0 {
		t.Fatalf("expected no args, got %+v", fn.Args)
	}
}

func TestTaskWithBody(t *testing.T) {
	body := moduleBody(t, "task automatic do_thing(int x); y = x; endtask")
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	task := body[0].(*ast.Task)
	if task.Name != "do_thing" || task.Prototype || len(task.Args) != 1 {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestTaskNoArgsNoParens(t *testing.T) {
	body := moduleBody(t, "task reset; x = 0; endtask")
	task := body[0].(*ast.Task)
	if task.Name != "reset" || len(task.Args) != 0 {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestExternFunctionIsPrototype(t *testing.T) {
	body := moduleBody(t, "extern function int foo(int x);")
	fn := body[0].(*ast.Function)
	if !fn.Prototype || fn.Name != "foo" {
		t.Fatalf("expected a prototype named foo, got %+v", fn)
	}
}

func TestExternTaskIsPrototype(t *testing.T) {
	body := moduleBody(t, "extern task bar(int x);")
	task := body[0].(*ast.Task)
	if !task.Prototype || task.Name != "bar" {
		t.Fatalf("expected a prototype named bar, got %+v", task)
	}
}

func TestExternVirtualFunctionIsPrototype(t *testing.T) {
	f, errs := parseSrc(t, `class c;
		extern virtual function void f();
		int x;
	endclass`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	cls := f.Decls[0].(*ast.Class)
	if len(cls.Body) != 2 {
		t.Fatalf("expected 2 decls (prototype + trailing variable), got %+v", cls.Body)
	}
	fn := cls.Body[0].(*ast.Function)
	if !fn.Prototype || fn.Name != "f" {
		t.Fatalf("expected a prototype named f, got %+v", fn)
	}
	if _, ok := cls.Body[1].(*ast.Variable); !ok {
		t.Fatalf("expected the trailing variable declaration to still parse, got %+v", cls.Body[1])
	}
}

func TestExternStaticFunctionIsPrototype(t *testing.T) {
	f, errs := parseSrc(t, `class c;
		extern static function int g(int x);
	endclass`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	cls := f.Decls[0].(*ast.Class)
	fn := cls.Body[0].(*ast.Function)
	if !fn.Prototype || fn.Name != "g" {
		t.Fatalf("expected a prototype named g, got %+v", fn)
	}
}

func TestExternVirtualTaskIsPrototype(t *testing.T) {
	f, errs := parseSrc(t, `class c;
		extern virtual task t(int x);
	endclass`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	cls := f.Decls[0].(*ast.Class)
	task := cls.Body[0].(*ast.Task)
	if !task.Prototype || task.Name != "t" {
		t.Fatalf("expected a prototype named t, got %+v", task)
	}
}

func TestDPIImportFunction(t *testing.T) {
	body := moduleBody(t, `import "DPI-C" function int c_func(int x);`)
	fn := body[0].(*ast.Function)
	if !fn.Prototype || fn.Name != "c_func" || fn.ReturnType.Name != "int" {
		t.Fatalf("unexpected DPI import: %+v", fn)
	}
}

func TestDPIImportWithContextQualifier(t *testing.T) {
	body := moduleBody(t, `import "DPI-C" context function void c_proc();`)
	fn := body[0].(*ast.Function)
	if !fn.Prototype || fn.Name != "c_proc" {
		t.Fatalf("unexpected DPI import: %+v", fn)
	}
}

func TestDPIImportTask(t *testing.T) {
	body := moduleBody(t, `import "DPI-C" task c_task(int x);`)
	task := body[0].(*ast.Task)
	if !task.Prototype || task.Name != "c_task" {
		t.Fatalf("unexpected DPI import: %+v", task)
	}
}

func TestFunctionAndExternPrototypeBothFound(t *testing.T) {
	// Mirrors sigils's own TestScanDeclarationsExternFunctionHasNoBody --
	// both the extern prototype (inside the class -- parseBody/parseDecl
	// are shared machinery, not container-specific, so this already works
	// even though class-specific features like extends/constraints land
	// in a later commit) and the out-of-line qualified body are found as
	// separate declarations of the same name; the parser doesn't try to
	// unify them (that's a future phase's job).
	f, errs := parseSrc(t, "class foo;\nextern function void bar();\nendclass\nfunction void foo::bar();\nendfunction")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("expected 2 top-level decls, got %+v", f.Decls)
	}
	cls, ok := f.Decls[0].(*ast.Class)
	if !ok || cls.Name != "foo" {
		t.Fatalf("expected class foo, got %+v", f.Decls[0])
	}
	if len(cls.Body) != 1 {
		t.Fatalf("expected 1 member in foo, got %+v", cls.Body)
	}
	proto, ok := cls.Body[0].(*ast.Function)
	if !ok || !proto.Prototype || proto.Name != "bar" {
		t.Fatalf("expected an extern prototype named bar, got %+v", cls.Body[0])
	}
	def, ok := f.Decls[1].(*ast.Function)
	if !ok || def.Prototype || def.Name != "bar" {
		t.Fatalf("expected a real (qualified) definition named bar, got %+v", f.Decls[1])
	}
}

func TestProceduralBlockSingleStatementIsSkipped(t *testing.T) {
	body := moduleBody(t, `initial $display("hi");`)
	if len(body) != 0 {
		t.Fatalf("expected the initial statement to be skipped with no declarations, got %+v", body)
	}
}

func TestProceduralBlockBeginEndIsSkipped(t *testing.T) {
	body := moduleBody(t, `initial begin
		$display("one");
		$display("two");
	end`)
	if len(body) != 0 {
		t.Fatalf("expected the initial begin/end block to be skipped with no declarations, got %+v", body)
	}
}

func TestProceduralBlockKeywordsAllRecognized(t *testing.T) {
	for _, kw := range []string{"initial", "always", "always_comb", "always_ff", "always_latch", "final"} {
		t.Run(kw, func(t *testing.T) {
			src := kw + " begin\n$display(\"x\");\nend"
			if kw == "always_comb" || kw == "final" {
				src = kw + " $display(\"x\");" // always_comb/final take a single statement or block either way; exercise the single-statement form for variety
			}
			body := moduleBody(t, src)
			if len(body) != 0 {
				t.Fatalf("expected %q to be skipped with no declarations, got %+v", kw, body)
			}
		})
	}
}

func TestProceduralBlockDoesNotConsumeFollowingDeclaration(t *testing.T) {
	body := moduleBody(t, `initial $display("hi");
logic [7:0] data;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (data) after the skipped initial statement, got %+v", body)
	}
	v, ok := body[0].(*ast.Variable)
	if !ok || v.Name != "data" {
		t.Fatalf("expected variable data, got %+v", body[0])
	}
}

func TestProceduralConstructMissingSemicolonRecordsErrorAndRecoversNextDecl(t *testing.T) {
	f, errs := parseSrc(t, `module top;
assign sig_a = sig_b
logic swallowed_signal;
assign sig_c = sig_a;
endmodule`)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' after \"assign sig_a = sig_b\"")
	}
	c := f.Decls[0].(*ast.Container)
	if len(c.Body) != 1 {
		t.Fatalf("expected exactly 1 surviving declaration (swallowed_signal), got %+v", c.Body)
	}
	v, ok := c.Body[0].(*ast.Variable)
	if !ok || v.Name != "swallowed_signal" {
		t.Fatalf("expected variable swallowed_signal to survive, got %+v", c.Body[0])
	}
}

func TestConcurrentAssertionMissingSemicolonRecordsErrorAndRecoversFollowingDecl(t *testing.T) {
	// Two consecutive missing-';' assertions -- skipProceduralConstruct is
	// reached for "assert"/"assume"/"cover"/"restrict" the same way it is
	// for "assign", per parseDecl's dispatch (containers.go).
	f, errs := parseSrc(t, `module top;
assert property (a)
assert property (b);
logic done;
endmodule`)
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' after the first assert property")
	}
	c := f.Decls[0].(*ast.Container)
	if len(c.Body) != 1 {
		t.Fatalf("expected exactly 1 surviving declaration (done), got %+v", c.Body)
	}
	if v, ok := c.Body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", c.Body[0])
	}
}

func TestProceduralConstructUnterminatedAtEOFRecordsError(t *testing.T) {
	f, errs := parseSrc(t, "module top;\nassign sig_a = sig_b")
	if len(errs) == 0 {
		t.Fatalf("expected an error for the missing ';' with no more input")
	}
	if len(f.Decls) != 1 {
		t.Fatalf("expected the module itself still recorded despite the unterminated body, got %+v", f.Decls)
	}
}

func TestProceduralBlockCastExpressionIsNotMistakenForNextDeclaration(t *testing.T) {
	// "int'(y)" (LRM 6.24.1) reuses the "int" keyword text
	// isDeclBoundaryKeyword otherwise treats as a new declaration's type
	// -- it must not be misread as one here.
	body := moduleBody(t, "initial x = int'(y);\nlogic done;")
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done), got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockAssignAsSingleStatementIsNotMistakenForNextDeclaration(t *testing.T) {
	// "initial assign x = y;" (LRM 10.4.1, a procedural continuous
	// assignment) as a construct's own single, un-begin/end-wrapped
	// statement -- reuses the same "assign" keyword text
	// isDeclBoundaryKeyword otherwise treats as the start of a new
	// module-scope continuous assignment; it must not be mistaken for one
	// as the very first token of the construct being skipped.
	body := moduleBody(t, "initial assign x = y;\nlogic done;")
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done), got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockWithNestedCaseStatement(t *testing.T) {
	// case/endcase is itself a blockOpen/blockCloseKeywords pair -- confirm
	// nested block-keyword depth is tracked correctly inside an always
	// block, not just a single begin/end.
	body := moduleBody(t, `always @(posedge clk) begin
		case (state)
			IDLE: next = RUN;
			RUN: next = IDLE;
		endcase
	end
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped always block, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockWithNamedBeginEndBlock(t *testing.T) {
	// "begin : label ... end : label" -- a named block (LRM 9.3.1); the
	// label on either side must not be mistaken for the start of a new
	// declaration.
	body := moduleBody(t, `initial begin : blk
		$display("hi");
	end : blk
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the named block, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockWithNamedForkJoinBlock(t *testing.T) {
	body := moduleBody(t, `initial fork : blk
		$display("hi");
	join : blk
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the named fork/join, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockDisableFork(t *testing.T) {
	// "disable fork;" (LRM 9.6.2) is a single statement -- its "fork" must
	// NOT be mistaken for a fork/join block opener with no matching join,
	// which would otherwise run the scan to end of file.
	body := moduleBody(t, `initial begin
		disable fork;
	end
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped initial block, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestTaskWithWaitFork(t *testing.T) {
	// "wait fork;" (LRM 9.6.1), same "fork" is not a block opener here as
	// TestProceduralBlockDisableFork -- confirmed inside a task body too,
	// followed by a second member that must survive.
	cls := classDecl(t, `class c;
  task run();
    wait fork;
  endtask
  function int after_it();
    return 1;
  endfunction
endclass`)
	if len(cls.Body) != 2 {
		t.Fatalf("expected 2 members (task + function), got %+v", cls.Body)
	}
	if _, ok := cls.Body[1].(*ast.Function); !ok {
		t.Fatalf("expected the function after the task to survive, got %T", cls.Body[1])
	}
}

func TestContinuousAssignmentIsSkipped(t *testing.T) {
	body := moduleBody(t, `assign out = a & b;
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped assign, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestConcurrentAssertionWithActionBlockIsSkipped(t *testing.T) {
	body := moduleBody(t, `assert property (@(posedge clk) req |-> ##1 ack) else $error("no ack");
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped assertion, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestLabeledConcurrentAssertionIsSkipped(t *testing.T) {
	body := moduleBody(t, `a1: assert property (@(posedge clk) req |-> ack);
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped labeled assertion, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestCoverPropertyIsSkipped(t *testing.T) {
	body := moduleBody(t, `cover property (@(posedge clk) req);
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped cover, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestContinuousAssignmentWithConcatenationLHS(t *testing.T) {
	// A brace-delimited concatenation on the LHS must not be mistaken for
	// a procedural block's begin/end -- braces are tracked via the
	// general paren/brace/bracket depth counter, not blockOpen/CloseKeywords.
	body := moduleBody(t, `assign {carry, sum} = a + b;
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped assign, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockWithParenSemicolonsInForLoop(t *testing.T) {
	// A for-loop header's own ';'s (inside the parens) must not be
	// mistaken for the statement's terminator.
	body := moduleBody(t, `initial begin
		for (int i = 0; i < 10; i++) begin
			$display("%d", i);
		end
	end
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped initial block, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockWithRandcase(t *testing.T) {
	// randcase/endcase (LRM 18.16) is its own blockOpen/blockCloseKeywords
	// pair; without "randcase" tracked as an opener, "endcase" alone would
	// prematurely close the enclosing begin/end and leave "x = 1;" dangling
	// at container scope.
	body := moduleBody(t, `initial begin
		randcase
			1: x = 0;
		endcase
		x = 1;
	end
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped initial block, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockDoWhile(t *testing.T) {
	body := moduleBody(t, `initial do begin
		x = x + 1;
	end while (x < 10);
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped do-while, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockDoWhileSingleStatement(t *testing.T) {
	body := moduleBody(t, `initial do x = x + 1; while (x < 10);
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the skipped do-while, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockDanglingElse(t *testing.T) {
	body := moduleBody(t, `always_ff @(posedge clk)
		if (q) q <= 0;
		else q <= 1;
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the if/else, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockElseIfChain(t *testing.T) {
	body := moduleBody(t, `always_ff @(posedge clk)
		if (a) q <= 0;
		else if (b) q <= 1;
		else if (c) q <= 2;
		else q <= 3;
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the else-if chain, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

func TestProceduralBlockDanglingElseWithBeginEnd(t *testing.T) {
	body := moduleBody(t, `always_ff @(posedge clk) begin
		if (q) begin
			q <= 0;
		end else begin
			q <= 1;
		end
	end
logic done;`)
	if len(body) != 1 {
		t.Fatalf("expected exactly 1 declaration (done) after the if/else with begin/end, got %+v", body)
	}
	if v, ok := body[0].(*ast.Variable); !ok || v.Name != "done" {
		t.Fatalf("expected variable done, got %+v", body[0])
	}
}

// A prototype has no body to span, but its End must still cover its own
// name rather than collapsing to zero width -- a zero-width span contains
// no position at all, including the name's first column, which silently
// breaks every "is the cursor inside this declaration" consumer built on
// Position/End (sigils' hover and goto-definition on a file-scope DPI
// import, an LSP documentSymbol's selectionRange-inside-range rule).
func TestPrototypeSpansItsOwnName(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"extern function", "class c;\nextern function void bar();\nendclass", "bar"},
		{"extern task", "class c;\nextern task run();\nendclass", "run"},
		{"pure virtual function", "virtual class c;\npure virtual function int f();\nendclass", "f"},
		{"dpi import function", "module m;\nimport \"DPI-C\" function void c_proc(int x);\nendmodule", "c_proc"},
		{"dpi import task", "module m;\nimport \"DPI-C\" task c_task(int x);\nendmodule", "c_task"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, errs := parseSrc(t, tc.src)
			if len(errs) != 0 {
				t.Fatalf("unexpected errors: %+v", errs)
			}
			line, char, endLine, endChar, ok := findPrototypeSpan(f.Decls, tc.want)
			if !ok {
				t.Fatalf("no prototype named %q found in %+v", tc.want, f.Decls)
			}
			if endLine != line {
				t.Errorf("EndLine = %d, want %d (a prototype occupies one line)", endLine, line)
			}
			if wantEnd := char + len(tc.want); endChar != wantEnd {
				t.Errorf("EndCharacter = %d, want %d (name %q starts at %d)", endChar, wantEnd, tc.want, char)
			}
		})
	}
}

// findPrototypeSpan locates a prototype function/task named want, at top
// level or one container deep, and returns its start/end position.
func findPrototypeSpan(decls []ast.Decl, want string) (line, char, endLine, endChar int, ok bool) {
	for _, d := range decls {
		var body []ast.Decl
		switch n := d.(type) {
		case *ast.Function:
			if n.Prototype && n.Name == want {
				return n.Line, n.Character, n.EndLine, n.EndCharacter, true
			}
		case *ast.Task:
			if n.Prototype && n.Name == want {
				return n.Line, n.Character, n.EndLine, n.EndCharacter, true
			}
		case *ast.Class:
			body = n.Body
		case *ast.Container:
			body = n.Body
		}
		if line, char, endLine, endChar, ok := findPrototypeSpan(body, want); ok {
			return line, char, endLine, endChar, ok
		}
	}
	return 0, 0, 0, 0, false
}
