package parser

import (
	"testing"

	"github.com/jfetkotto/svparse/ast"
)

func TestGenerateForWithInstantiation(t *testing.T) {
	body := moduleBody(t, `genvar i;
		generate
			for (i = 0; i < 4; i = i + 1) begin : gen_leaf
				leaf u_leaf (.clk(clk));
			end
		endgenerate`)

	var gv *ast.Variable
	var inst *ast.Instantiation
	for _, d := range body {
		switch n := d.(type) {
		case *ast.Variable:
			gv = n
		case *ast.Instantiation:
			inst = n
		}
	}
	if gv == nil || gv.Type.Name != "genvar" || gv.Name != "i" {
		t.Fatalf("expected a genvar declaration named i, got %+v", body)
	}
	if inst == nil || inst.ModuleType != "leaf" || len(inst.Instances) != 1 || inst.Instances[0].Name != "u_leaf" {
		t.Fatalf("expected the leaf instantiation inside the generate-for to be parsed, got %+v", body)
	}
}

func TestGenerateIfElse(t *testing.T) {
	body := moduleBody(t, `generate
		if (WIDTH > 8) begin : wide
			leaf u_wide (.clk(clk));
		end else begin : narrow
			leaf u_narrow (.clk(clk));
		end
	endgenerate`)

	var insts []*ast.Instantiation
	for _, d := range body {
		if inst, ok := d.(*ast.Instantiation); ok {
			insts = append(insts, inst)
		}
	}
	if len(insts) != 2 {
		t.Fatalf("expected both branches' instantiations to parse, got %+v", body)
	}
	if insts[0].Instances[0].Name != "u_wide" || insts[1].Instances[0].Name != "u_narrow" {
		t.Fatalf("unexpected instance names: %+v", insts)
	}
}

func TestGenerateIfWithoutBeginEnd(t *testing.T) {
	// LRM allows a single bare item with no begin/end wrapping.
	body := moduleBody(t, `generate
		if (WIDTH > 8)
			leaf u_leaf (.clk(clk));
	endgenerate`)
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	inst := body[0].(*ast.Instantiation)
	if inst.Instances[0].Name != "u_leaf" {
		t.Fatalf("unexpected instantiation: %+v", inst)
	}
}

func TestGenerateNestedBeginEnd(t *testing.T) {
	body := moduleBody(t, `generate
		begin : outer
			begin : inner
				leaf u_leaf (.clk(clk));
			end
		end
	endgenerate`)
	if len(body) != 1 {
		t.Fatalf("expected 1 decl, got %+v", body)
	}
	inst := body[0].(*ast.Instantiation)
	if inst.Instances[0].Name != "u_leaf" {
		t.Fatalf("unexpected instantiation: %+v", inst)
	}
}

func TestGenerateCaseSkipsToEndcase(t *testing.T) {
	body := moduleBody(t, `logic before_marker;
		generate
			case (WIDTH)
				8: begin : narrow
					leaf u_leaf (.clk(clk));
				end
				default: begin : wide
					leaf u_leaf2 (.clk(clk));
				end
			endcase
		endgenerate
		logic after_marker;`)

	var names []string
	for _, d := range body {
		if v, ok := d.(*ast.Variable); ok {
			names = append(names, v.Name)
		}
	}
	if len(names) != 2 || names[0] != "before_marker" || names[1] != "after_marker" {
		t.Fatalf("expected the surrounding declarations to survive the skipped generate-case, got %+v", names)
	}
}

func TestGenerateWithoutGenerateKeywords(t *testing.T) {
	// "generate"/"endgenerate" are themselves optional per the LRM in some
	// tool dialects -- for/if/begin/end/genvar must all work the same way
	// without them wrapping the construct.
	body := moduleBody(t, `genvar i;
		for (i = 0; i < 2; i = i + 1) begin : gen_leaf
			leaf u_leaf (.clk(clk));
		end`)
	var inst *ast.Instantiation
	for _, d := range body {
		if n, ok := d.(*ast.Instantiation); ok {
			inst = n
		}
	}
	if inst == nil || inst.Instances[0].Name != "u_leaf" {
		t.Fatalf("expected the leaf instantiation to be parsed, got %+v", body)
	}
}

// A generate case's body routinely contains a procedural case. Before
// skipToKeyword tracked its own openers, the scan stopped at the INNER
// "endcase", resuming the parse mid-generate-case: the declarations after
// it were then misdispatched and swallowed by error recovery.
func TestGenerateCaseWithNestedCaseKeepsFollowingDecls(t *testing.T) {
	f, errs := parseSrc(t, `module top;
generate
case (WIDTH)
  8: begin
       always_comb begin
         case (sel) 1'b0: y = a; default: y = b; endcase
       end
     end
  default: begin end
endcase
endgenerate
endmodule

module after;
endmodule
`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	names := containerNames(f)
	if len(names) != 2 || names[0] != "top" || names[1] != "after" {
		t.Fatalf("expected modules top and after, got %v", names)
	}
}
