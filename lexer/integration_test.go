package lexer

import (
	"testing"

	"github.com/jfetkotto/svparse/token"
)

// A realistic multi-construct snippet exercising most lexical categories
// together: a directive, a parameterized module with an ANSI port list
// (ranges, directions), a packed struct typedef, an always_ff block with
// event control and a nonblocking assignment, an unbased-unsized
// literal, dotted member access, a line comment, an assertion with
// implication and disable-iff, and a class with a rand member and a
// constraint using a range. The point isn't to check every single token
// -- it's to prove the whole lexer coheres on real-shaped input, not just
// its parts in isolation.
const integrationSnippet = `` + "`" + `default_nettype none

module top #(
  parameter int WIDTH = 8
) (
  input  logic             clk,
  input  logic             rst_n,
  output logic [WIDTH-1:0] data_out
);

  typedef struct packed {
    logic [7:0] addr;
    logic       valid;
  } bus_t;

  bus_t bus;

  always_ff @(posedge clk or negedge rst_n) begin
    if (!rst_n) begin
      data_out <= '0;
    end else begin
      data_out <= bus.addr;
    end
  end

  // outstanding-transaction check
  assert property (@(posedge clk) disable iff (!rst_n) bus.valid |-> data_out == bus.addr);

endmodule

class packet;
  rand bit [7:0] payload;
  constraint c_payload { payload inside {[0:255]}; }
endclass
`

func TestLexIntegrationSnippetHasNoInvalidTokensOrErrors(t *testing.T) {
	toks, errs := Lex(integrationSnippet)

	if len(errs) != 0 {
		t.Fatalf("unexpected errors lexing a realistic snippet: %+v", errs)
	}
	for _, tok := range toks {
		if tok.Kind == token.KindInvalid {
			t.Errorf("unexpected KindInvalid token %+v", tok)
		}
	}
	if toks[len(toks)-1].Kind != token.KindEOF {
		t.Fatalf("expected the token stream to end with KindEOF")
	}
}

func TestLexIntegrationSnippetSpotChecks(t *testing.T) {
	toks, _ := Lex(integrationSnippet)

	mustContain := []struct {
		kind token.Kind
		text string
	}{
		{token.KindDirective, "default_nettype"},
		{token.KindKeyword, "module"},
		{token.KindIdent, "top"},
		{token.KindHash, "#"},
		{token.KindKeyword, "parameter"},
		{token.KindKeyword, "input"},
		{token.KindLBrack, "["},
		{token.KindSub, "-"},
		{token.KindKeyword, "typedef"},
		{token.KindKeyword, "packed"},
		{token.KindKeyword, "always_ff"},
		{token.KindAt, "@"},
		{token.KindKeyword, "posedge"},
		{token.KindKeyword, "or"},
		{token.KindKeyword, "negedge"},
		{token.KindBang, "!"},
		{token.KindLe, "<="},
		{token.KindUnbasedUnsizedLiteral, "'0"},
		{token.KindDot, "."},
		{token.KindKeyword, "assert"},
		{token.KindKeyword, "property"},
		{token.KindKeyword, "disable"},
		{token.KindKeyword, "iff"},
		{token.KindImplies, "|->"},
		{token.KindEq, "=="},
		{token.KindKeyword, "class"},
		{token.KindKeyword, "rand"},
		{token.KindKeyword, "constraint"},
		{token.KindKeyword, "inside"},
		{token.KindColon, ":"},
		{token.KindIntLiteral, "255"},
	}
	for _, want := range mustContain {
		found := false
		for _, tok := range toks {
			if tok.Kind == want.kind && tok.Text == want.text {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a %v token %q somewhere in the snippet, found none", want.kind, want.text)
		}
	}
}

func FuzzLexNeverPanics(f *testing.F) {
	seeds := []string{
		"", " ", "\n", "\t",
		integrationSnippet,
		"module top; endmodule",
		"`define FOO 1",
		`"unterminated`,
		"/* unterminated",
		"8'hFF 'x 1.5e-10 10ns",
		"<<<= >>>= |-> |=> ~^ ^~",
		`\escaped+ident $display`,
		"'{",
		"(* attr *)",
		"'q 8'1",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks, _ := Lex(src)
		if len(toks) == 0 {
			t.Fatalf("Lex(%q) returned no tokens; expected at least a trailing KindEOF", src)
		}
		if toks[len(toks)-1].Kind != token.KindEOF {
			t.Fatalf("Lex(%q) did not end with KindEOF", src)
		}
	})
}
