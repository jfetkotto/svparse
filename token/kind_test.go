package token

import "testing"

func TestKindStringKnownKinds(t *testing.T) {
	cases := []struct {
		k    Kind
		want string
	}{
		{KindIdent, "Ident"},
		{KindKeyword, "Keyword"},
		{KindIntLiteral, "IntLiteral"},
		{KindAdd, "+"},
		{KindShl, "<<"},
		{KindAShlAssign, "<<<="},
		{KindImplies, "|->"},
		{KindEOF, "EOF"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("Kind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestKindStringUnknownKindDoesNotPanic(t *testing.T) {
	if got := Kind(99999).String(); got != "Unknown" {
		t.Errorf("Kind(99999).String() = %q, want %q", got, "Unknown")
	}
}

// Every Kind constant used in the punctuation/operator tables must have a
// distinct name registered in kindNames, or two different lexemes would
// print identically -- a real bug a lexer's tests would otherwise not
// catch (String() would silently "succeed" either way).
func TestKindNamesAreUnique(t *testing.T) {
	seen := make(map[string]Kind)
	for k, name := range kindNames {
		if prev, ok := seen[name]; ok {
			t.Errorf("kindNames[%v] and kindNames[%v] both = %q", prev, k, name)
		}
		seen[name] = k
	}
}
