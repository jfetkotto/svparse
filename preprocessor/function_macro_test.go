package preprocessor

import "testing"

func TestFunctionLikeMacroBasic(t *testing.T) {
	toks, errs := pp(t, "`define ADD(a,b) ((a)+(b))\n`ADD(1,2)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "(", "(", "1", ")", "+", "(", "2", ")", ")")
}

func TestFunctionLikeMacroZeroParams(t *testing.T) {
	toks, errs := pp(t, "`define FOO() bar\n`FOO()")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "bar")
}

func TestObjectLikeMacroBodyStartingWithParenIsNotFunctionLike(t *testing.T) {
	// A space between the name and '(' means object-like: FOO's body is
	// literally "(a,b)", not a parameter list.
	toks, errs := pp(t, "`define FOO (a,b)\n`FOO")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "(", "a", ",", "b", ")")
}

func TestFunctionLikeMacroDefaultParameter(t *testing.T) {
	toks, errs := pp(t, "`define FOO(a, b=99) a+b\n`FOO(1)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "1", "+", "99")
}

func TestFunctionLikeMacroDefaultOverriddenWhenProvided(t *testing.T) {
	toks, _ := pp(t, "`define FOO(a, b=99) a+b\n`FOO(1,2)")
	assertTexts(t, toks, "1", "+", "2")
}

func TestFunctionLikeMacroMissingRequiredArgumentRecordsError(t *testing.T) {
	toks, errs := pp(t, "`define FOO(a, b) a+b\n`FOO(1)")
	if len(errs) != 1 {
		t.Fatalf("expected one error for the missing required argument, got %+v", errs)
	}
	// b substitutes to nothing (tolerant), leaving just "1" "+".
	assertTexts(t, toks, "1", "+")
}

func TestFunctionLikeMacroArgumentWithNestedParensNotSplitEarly(t *testing.T) {
	toks, errs := pp(t, "`define ID(a) a\n`ID(foo(1,2))")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	// The nested call's own comma must not be treated as an argument
	// separator for ID -- ID receives exactly one argument, foo(1,2).
	assertTexts(t, toks, "foo", "(", "1", ",", "2", ")")
}

func TestFunctionLikeMacroArgumentWithAssignmentPattern(t *testing.T) {
	toks, errs := pp(t, "`define ID(a) a\n`ID('{1,2,3})")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "'{", "1", ",", "2", ",", "3", "}")
}

func TestFunctionLikeMacroWithoutInvocationParensLeftLiteral(t *testing.T) {
	toks, errs := pp(t, "`define FOO(a) a\n`FOO after")
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %+v", errs)
	}
	assertTexts(t, toks, "FOO", "after")
}

func TestSelfReferentialFunctionLikeMacroDoesNotInfiniteLoop(t *testing.T) {
	toks, _ := pp(t, "`define FOO(x) `FOO(x)\n`FOO(1)")
	assertTexts(t, toks, "FOO", "(", "1", ")")
}

func TestFunctionLikeMacroArgumentItselfMacroExpands(t *testing.T) {
	// Deferred/lazy expansion: an unexpanded macro reference substituted
	// into an argument position still gets expanded, since it flows back
	// through the same main loop once pushed as part of the expansion.
	toks, errs := pp(t, "`define WIDTH 8\n`define ID(a) a\n`ID(`WIDTH)")
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	assertTexts(t, toks, "8")
	if toks[0].MacroName != "WIDTH" {
		t.Fatalf("expected the argument's own macro attribution to survive substitution, got %+v", toks[0])
	}
}

func TestFunctionLikeMacroExpansionTaggedWithInvocation(t *testing.T) {
	toks, _ := pp(t, "`define ADD(a,b) (a+b)\n`ADD(1,2)")
	assertTexts(t, toks, "(", "1", "+", "2", ")")
	// The literal body structure ("(", "+", ")") is tagged with the
	// invocation; the substituted arguments ("1", "2") keep their own
	// (empty, since they're plain literals from the top-level call site)
	// attribution rather than being relabeled as coming from ADD.
	wantMacroName := []string{"ADD", "", "ADD", "", "ADD"}
	for i, want := range wantMacroName {
		if toks[i].MacroName != want {
			t.Errorf("token %d (%q): MacroName = %q, want %q", i, toks[i].Text, toks[i].MacroName, want)
		}
	}
}
