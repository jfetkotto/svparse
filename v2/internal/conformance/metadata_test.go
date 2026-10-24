package conformance

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseMetadataDefaults(t *testing.T) {
	m := ParseMetadata("module m; endmodule\n")
	if want := []Stage{StageParsing, StageElaboration}; !reflect.DeepEqual(m.Types, want) {
		t.Errorf("Types = %v, want upstream default %v", m.Types, want)
	}
	if m.ShouldFail {
		t.Error("ShouldFail = true with no metadata")
	}
	if m.HighestStage() != StageElaboration {
		t.Errorf("HighestStage = %v", m.HighestStage())
	}
}

func TestParseMetadataHeaderInBlockComment(t *testing.T) {
	src := `// Copyright
/*
:name: bad_enum
:description: enum with duplicate values
:should_fail_because: duplicate enum value
:tags: 6.19
:type: simulation elaboration parsing
:top_module: top
:defines: A=1 B
*/
module m; endmodule
`
	m := ParseMetadata(src)
	if m.Name != "bad_enum" || m.TopModule != "top" {
		t.Errorf("name/top = %q/%q", m.Name, m.TopModule)
	}
	if !m.ShouldFail || m.ShouldFailBecause != "duplicate enum value" {
		t.Errorf("ShouldFail = %v because %q", m.ShouldFail, m.ShouldFailBecause)
	}
	if want := []Stage{StageSimulation, StageElaboration, StageParsing}; !reflect.DeepEqual(m.Types, want) {
		t.Errorf("Types = %v, want %v", m.Types, want)
	}
	if want := map[string]string{"A": "1", "B": ""}; !reflect.DeepEqual(m.Defines, want) {
		t.Errorf("Defines = %v, want %v", m.Defines, want)
	}
	if m.HighestStage() != StageSimulation {
		t.Errorf("HighestStage = %v", m.HighestStage())
	}
}

func TestParseMetadataShouldFailForms(t *testing.T) {
	cases := []struct {
		name, src string
		want      bool
	}{
		{"explicit 1", ":should_fail: 1\n", true},
		{"explicit 0", ":should_fail: 0\n", false},
		{"because overrides 0", ":should_fail: 0\n:should_fail_because: x\n", true},
		{"CRLF", ":should_fail_because: x\r\n", true},
	}
	for _, c := range cases {
		if got := ParseMetadata(c.src).ShouldFail; got != c.want {
			t.Errorf("%s: ShouldFail = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseMetadataUnknownType(t *testing.T) {
	m := ParseMetadata(":type: parsing synthesis\n")
	if !reflect.DeepEqual(m.Types, []Stage{StageParsing}) || !reflect.DeepEqual(m.UnknownTypes, []string{"synthesis"}) {
		t.Errorf("Types = %v, UnknownTypes = %v", m.Types, m.UnknownTypes)
	}
}

func FuzzParseMetadataNeverPanics(f *testing.F) {
	f.Add(":type: parsing\n:defines: A=1\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, src string) { ParseMetadata(src) })
}

func TestParseBaseline(t *testing.T) {
	good := "# comment\n\nchapter-6/a.sv fail # reason here\nchapter-6/b.sv pass # another\n"
	got, err := ParseBaseline(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	if got["chapter-6/a.sv"] != (BaselineEntry{Observed: OutcomeFail, Reason: "reason here"}) || len(got) != 2 {
		t.Errorf("got %+v", got)
	}

	for name, bad := range map[string]string{
		"no reason":      "a.sv fail\n",
		"empty reason":   "a.sv fail #\n",
		"bad outcome":    "a.sv maybe # r\n",
		"missing field":  "a.sv # r\n",
		"duplicate path": "a.sv fail # r\na.sv pass # r\n",
	} {
		if _, err := ParseBaseline(strings.NewReader(bad)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
