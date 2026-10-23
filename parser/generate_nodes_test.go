package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

const generateGoldenSource = `module m #(int W = 8);
generate
  for (genvar i = 0; i < W; i++) begin : g
    if (i == 0) assign a = 1; else if (i > 3) begin : hi
      leaf u();
    end else wire w;
  end
  case (W)
    8, 16: begin end
    default: ;
  endcase
endgenerate
endmodule`

const generateGoldenDump = `(file "test.sv"
  (module m
    (parameter W <int> = 8)
    (generate
      (gen-for genvar i init=0 cond=(< i W)
        (expr (post++ i))
        (gen-block g
          (gen-if cond=(== i 0)
            (gen-block bare
              (continuous-assign
                (assign = a 1)))
            (gen-if cond=(> i 3)
              (gen-block hi
                (instantiation leaf
                  (instance u)))
              (gen-block bare
                (variable w <wire>))))))
      (gen-case W
        (item [8 16]
          (gen-block))
        (item default
          (gen-block bare))))))
`

func TestGenerateGolden(t *testing.T) {
	f, errs := parseSrc(t, generateGoldenSource)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	if got := ast.Dump(f); strings.TrimSpace(got) != strings.TrimSpace(generateGoldenDump) {
		t.Fatalf("dump mismatch:\n%s", got)
	}
}

// The else after a bare continuous assignment belongs to the generate-if, not
// to the assignment the extent scan skipped.
func TestGenerateElseAfterBareProceduralItem(t *testing.T) {
	for _, item := range []string{"assign a = 1;", "always @* x = 1;", "initial x = 1;"} {
		body := moduleBody(t, "if (c) "+item+" else "+item)
		gi, ok := body[0].(*ast.GenerateIf)
		if len(body) != 1 || !ok || gi.Else == nil {
			t.Errorf("%q: want one GenerateIf with an else, got %s", item, ast.Dump(body[0]))
		}
	}
}

func TestGenerateLabelFromEndSide(t *testing.T) {
	body := moduleBody(t, "begin leaf u(); end : tail")
	if b, ok := body[0].(*ast.GenerateBlock); !ok || b.Label != "tail" {
		t.Fatalf("got %s", ast.Dump(body[0]))
	}
}

func TestGenerateConditionErrorsAreSuppressed(t *testing.T) {
	toks, _ := preprocessor.Preprocess("t.sv", "module m;\nif (a + ) begin end\nendmodule", nil)
	p := &parser{toks: toks}
	p.parseBody("")
	if len(p.errs) != 0 {
		t.Fatalf("reported: %+v", p.errs)
	}
}

func TestGenerateDepthLimit(t *testing.T) {
	src := "module m;\n" + strings.Repeat("begin ", 3000) + strings.Repeat("end ", 3000) + "\nendmodule"
	f, errs := parseSrc(t, src)
	if len(errs) == 0 {
		t.Fatal("expected a depth error")
	}
	if len(f.Decls) != 1 {
		t.Fatalf("module lost: %d decls", len(f.Decls))
	}
}

// An unclosed generate block must not swallow the next module.
func TestGenerateUnclosedBlockStopsAtEndmodule(t *testing.T) {
	f, errs := parseSrc(t, "module a;\nif (c) begin\nwire w;\nendmodule\nmodule b;\nendmodule")
	if len(errs) == 0 {
		t.Fatal("expected an error")
	}
	if len(f.Decls) != 2 {
		t.Fatalf("want 2 modules, got %d", len(f.Decls))
	}
}
