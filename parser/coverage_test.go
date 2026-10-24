package parser

import (
	"strings"
	"testing"

	"github.com/jfetkotto/svparse/ast"
	"github.com/jfetkotto/svparse/preprocessor"
)

const coverageSource = `module m;
  covergroup cg (int w, ref logic [3:0] r) with function sample (int a, bit b);
    option.per_instance = 1;
    type_option.comment = "hi";
    cp_a : coverpoint a iff (b) {
      option.auto_bin_max = 8;
      bins low = {[0:3], 5};
      bins arr[] = {[8:15]};
      bins sized[4] = {[16:31]} with (item % 2 == 0);
      wildcard bins w = {4'b1??0};
      illegal_bins bad = {13};
      ignore_bins ign = {[20:$]};
      bins t1 = (1 => 2 => 3), (4, 5 => 6);
      bins t2 = (1 [*3] => 2), (7 [->2]), (8 [= 1:3] => 9);
      bins other = default;
      bins seq = default sequence;
      bins sel = cp_other with (item > 3);
      bins st = '{1, 2, 3};
    }
    coverpoint r;
    bit [3:0] cp_b : coverpoint b;
    x : cross cp_a, cp_b iff (b) {
      bins both = binsof(cp_a) intersect {[1:2]} && !binsof(cp_b.low);
      illegal_bins no = binsof(cp_a.low) || (binsof(cp_b) with (item < 2));
      ignore_bins ig = x_sel matches 3;
      option.weight = 2;
    }
    cross cp_a, cp_b;
  endgroup : cg
  covergroup cg2 @(posedge clk); coverpoint a; endgroup
  covergroup cg3 @@(begin f); endgroup
  covergroup extends cg; endgroup
endmodule
`

const coverageDump = `(file "test.sv"
  (module m
    (covergroup cg sample
      (arg w <int>)
      (arg ref r <logic [3:0]>)
      (arg a <int>)
      (arg b <bit>)
      (option.per_instance = 1)
      (type_option.comment = "hi")
      (coverpoint cp_a a iff=b
        (option.auto_bin_max = 8)
        (bins low values {[0:3] 5})
        (bins arr[_] values {[8:15]})
        (bins sized[4] values {[16:31]} with=(== (% item 2) 0))
        (bins wildcard w values {4'b1??0})
        (illegal_bins bad values {13})
        (ignore_bins ign values {[20:$]})
        (bins t1 trans
          (trans
            (step [1])
            (step [2])
            (step [3]))
          (trans
            (step [4 5])
            (step [6])))
        (bins t2 trans
          (trans
            (step [1] repeat* 3)
            (step [2]))
          (trans
            (step [7] repeat-> 2))
          (trans
            (step [8] repeat= 1:3)
            (step [9])))
        (bins other default)
        (bins seq default sequence)
        (bins sel with cp_other with=(> item 3))
        (bins st set (pattern 1 2 3)))
      (coverpoint r)
      (coverpoint cp_b <bit [3:0]> b)
      (cross x [cp_a cp_b] iff=b
        (bins both select (&& (intersect (call binsof cp_a) (concat [1:2])) (unary ! (call binsof (. cp_b low)))))
        (illegal_bins no select (|| (call binsof (. cp_a low)) (paren (with (call binsof cp_b) (< item 2)))))
        (ignore_bins ig select (matches x_sel 3))
        (option.weight = 2))
      (cross [cp_a cp_b]))
    (covergroup cg2 event=@(posedge clk)
      (coverpoint a))
    (covergroup cg3 block-event)
    (covergroup cg extends=cg)))
`

func TestCovergroupGolden(t *testing.T) {
	f, errs := parseSrc(t, coverageSource)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	if got := ast.Dump(f); strings.TrimSpace(got) != strings.TrimSpace(coverageDump) {
		t.Fatalf("dump mismatch:\n%s", got)
	}
}

func TestCovergroupInClassAndFields(t *testing.T) {
	f, errs := parseSrc(t, `class C;
  covergroup cg @(posedge clk);
    coverpoint x { bins a = {1}; }
  endgroup
  int after;
endclass`)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	var cg *ast.Covergroup
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.Covergroup); ok {
			cg = c
		}
		return true
	})
	if cg == nil || cg.Name != "cg" || cg.Event == nil || len(cg.Items) != 1 {
		t.Fatalf("covergroup: %v", cg)
	}
	if !strings.Contains(ast.Dump(f), "after") {
		t.Fatalf("member after the covergroup lost:\n%s", ast.Dump(f))
	}
}

func TestCovergroupExtendsRedefines(t *testing.T) {
	body := moduleBody(t, "covergroup extends cg; endgroup")
	cg, ok := body[0].(*ast.Covergroup)
	if !ok || cg.Name != "cg" || cg.Extends != "cg" {
		t.Fatalf("got %s", ast.Dump(body[0]))
	}
}

func TestCovergroupErrorsAreReported(t *testing.T) {
	f, errs := parseSrc(t, "module m;\ncovergroup cg; coverpoint ; cp: coverpoint x { bins b = ; bogus; } cross ; endgroup\nlogic after;\nendmodule")
	if len(errs) == 0 {
		t.Fatal("expected the malformed items to be reported")
	}
	body := f.Decls[0].(*ast.Container).Body
	if len(body) != 2 {
		t.Fatalf("items:\n%s", ast.Dump(f))
	}
}

func TestCovergroupMissingHeaderSemicolonStopsAtItems(t *testing.T) {
	f, errs := parseSrc(t, "module m;\ncovergroup cg\n coverpoint a;\nendgroup\nlogic after;\nendmodule")
	if len(errs) == 0 {
		t.Fatal("expected an error for the missing ';'")
	}
	if !strings.Contains(ast.Dump(f), "after") {
		t.Fatalf("declaration after the covergroup lost:\n%s", ast.Dump(f))
	}
}

func TestUnterminatedCovergroupItemsStopAtEndmodule(t *testing.T) {
	f, errs := parseSrc(t, "module a; covergroup cg; coverpoint x;\nendmodule\nmodule b; endmodule")
	if len(errs) == 0 || len(f.Decls) != 2 {
		t.Fatalf("errs=%v decls=%d", errs, len(f.Decls))
	}
}

func FuzzParseCovergroup(f *testing.F) {
	f.Add(coverageSource)
	f.Add("covergroup g; c: coverpoint a { bins b = (1 => 2 [*3]), (4); } cross c, d { bins s = binsof(c) intersect {1}; } endgroup")
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := preprocessor.Preprocess("f.sv", src, nil)
		Parse("f.sv", toks)
	})
}
