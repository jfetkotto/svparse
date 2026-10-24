package token

import "testing"

func TestKeywordsContainsRepresentativeSample(t *testing.T) {
	want := []string{
		// module-level
		"module", "endmodule", "interface", "program", "package", "endpackage",
		// data types
		"logic", "bit", "reg", "integer", "struct", "enum", "typedef", "genvar",
		// procedural
		"always", "always_ff", "always_comb", "always_latch", "initial", "begin", "end",
		"if", "else", "case", "for", "while", "foreach",
		// OOP
		"class", "endclass", "extends", "virtual", "static", "local", "protected", "extern",
		// gate primitives
		"and", "or", "nand", "buf", "bufif0",
		// assertions/coverage
		"assert", "property", "sequence", "cover", "assume", "covergroup",
		// randomization
		"rand", "randc", "constraint", "randcase",
		// added in 1800-2012/2017
		"let", "checker", "global", "interconnect", "nettype", "soft", "unique0",
	}
	for _, kw := range want {
		if !Keywords[kw] {
			t.Errorf("Keywords[%q] = false, want true", kw)
		}
	}
}

func TestKeywordsExcludesOrdinaryIdentifiers(t *testing.T) {
	notKeywords := []string{"foo", "clk", "rst_n", "my_module", "data_in", "AXI_master", "i"}
	for _, name := range notKeywords {
		if Keywords[name] {
			t.Errorf("Keywords[%q] = true, want false", name)
		}
	}
}

// A coarse regression guard: IEEE 1800-2017 Annex B.6 has 240+ reserved
// words. This doesn't pin an exact count (transcription of a 240-entry
// list by hand is exactly the kind of thing that could gain or lose one
// or two entries over time without it being a real bug), but a sudden
// drop would indicate something got deleted by accident.
func TestKeywordsHasExpectedApproximateSize(t *testing.T) {
	if len(Keywords) < 230 {
		t.Errorf("len(Keywords) = %d, want at least 230 (IEEE 1800-2017 Annex B.6 has 240+)", len(Keywords))
	}
}
