package sema

import (
	"math/rand"
	"testing"

	"github.com/jfetkotto/svparse/svint"
)

func vec(b Base, signed bool, l, r int) Type { return Vector(b, signed, Range{l, r}) }

func i(b Base) *Integral { return NewIntegral(b) }

var (
	tInt     = i(BaseInt)
	tInteger = i(BaseInteger)
	tByte    = i(BaseByte)
	tLogic   = i(BaseLogic)
	tBit     = i(BaseBit)
	tReal    = &Real{KindReal}
	tShort   = &Real{KindShortreal}
)

func TestTypeStrings(t *testing.T) {
	cases := []struct {
		t    Type
		want string
	}{
		{tInt, "int"},
		{&Integral{Base: BaseInt}, "int unsigned"},
		{&Integral{Base: BaseInt, Signed: true}, "int"},
		{i(BaseTime), "time"},
		{vec(BaseLogic, false, 7, 0), "logic [7:0]"},
		{vec(BaseBit, true, 0, 3), "bit signed [0:3]"},
		{tReal, "real"},
		{tShort, "shortreal"},
		{&Real{KindRealtime}, "realtime"},
		{String, "string"},
		{&PackedArray{Elem: vec(BaseLogic, false, 7, 0), Range: Range{3, 0}}, "logic [7:0] [3:0]"},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 7}}, "int [0:7]"},
		{&DynamicArray{Elem: tByte}, "byte []"},
		{&Queue{Elem: tInt, Bound: -1}, "int [$]"},
		{&Queue{Elem: tInt, Bound: 9}, "int [$:9]"},
		{&AssocArray{Elem: tInt, Index: String}, "int [string]"},
		{&AssocArray{Elem: tInt}, "int [*]"},
		{&Alias{Name: "word_t", Target: tInt}, "word_t"},
		{&Class{Name: "C"}, "C"},
		{&VirtualInterface{Interface: "bus", Modport: "mp"}, "virtual bus.mp"},
		{&VirtualInterface{Interface: "bus"}, "virtual bus"},
		{&Enum{Members: []EnumMember{{Name: "A"}, {Name: "B"}}}, "enum {A, B}"},
		{&Enum{Name: "color_t"}, "color_t"},
		{&Struct{Packed: true, Signed: true, Members: []Member{{"a", tBit}, {"b", tInt}}}, "struct packed signed {bit a; int b;}"},
		{&Union{Tagged: true, Members: []Member{{"a", tBit}}}, "union tagged {bit a;}"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestVectorValidation(t *testing.T) {
	if !IsInvalid(Vector(BaseInt, true, Range{7, 0})) {
		t.Error("int cannot take a packed range")
	}
	if !IsInvalid(Vector(BaseLogic, false, Range{1 << 30, 0})) {
		t.Error("an oversized vector must be rejected")
	}
	for _, b := range []Base{BaseBit, BaseLogic, BaseReg} {
		if IsInvalid(Vector(b, false, Range{3, 0})) {
			t.Errorf("%v vector rejected", b)
		}
	}
	if (Range{0, 7}).Size() != 8 || (Range{7, 0}).Size() != 8 || (Range{5, 5}).Size() != 1 {
		t.Error("Range.Size")
	}
	if b, ok := ParseBase("longint"); !ok || b != BaseLongint {
		t.Error("ParseBase")
	}
	if _, ok := ParseBase("float"); ok {
		t.Error("ParseBase accepted a non-type")
	}
}

func TestIntegralProperties(t *testing.T) {
	cases := []struct {
		t                 Type
		width             int
		signed, four      bool
		integral, numeric bool
	}{
		{tInt, 32, true, false, true, true},
		{tInteger, 32, true, true, true, true},
		{tByte, 8, true, false, true, true},
		{i(BaseTime), 64, false, true, true, true},
		{i(BaseLongint), 64, true, false, true, true},
		{i(BaseShortint), 16, true, false, true, true},
		{tBit, 1, false, false, true, true},
		{tLogic, 1, false, true, true, true},
		{vec(BaseReg, true, 15, 0), 16, true, true, true, true},
		{&PackedArray{Elem: vec(BaseBit, false, 7, 0), Range: Range{3, 0}}, 32, false, false, true, true},
		{&Struct{Packed: true, Members: []Member{{"a", tLogic}, {"b", vec(BaseBit, false, 6, 0)}}}, 8, false, true, true, true},
		{&Enum{Base: tInt}, 32, true, false, true, true},
		{tReal, 64, false, false, false, true},
		{tShort, 32, false, false, false, true},
		{String, 0, false, false, false, false},
	}
	for _, c := range cases {
		w, ok := Bits(c.t)
		if c.width > 0 && (!ok || w != c.width) {
			t.Errorf("%v: Bits = %d %v, want %d", c.t, w, ok, c.width)
		}
		if c.width == 0 && ok {
			t.Errorf("%v: Bits should fail", c.t)
		}
		if IsIntegral(c.t) != c.integral || IsNumeric(c.t) != c.numeric {
			t.Errorf("%v: integral=%v numeric=%v", c.t, IsIntegral(c.t), IsNumeric(c.t))
		}
		if c.integral && (IsSigned(c.t) != c.signed || IsFourState(c.t) != c.four) {
			t.Errorf("%v: signed=%v four=%v", c.t, IsSigned(c.t), IsFourState(c.t))
		}
	}
	if IsIntegral(&Struct{Members: []Member{{"a", tInt}}}) || IsIntegral(&Union{}) {
		t.Error("unpacked struct and union are not integral")
	}
}

func TestBitsOfAggregates(t *testing.T) {
	cases := []struct {
		t    Type
		want int
		ok   bool
	}{
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, 128, true},
		{&UnpackedArray{Elem: &UnpackedArray{Elem: tByte, Range: Range{0, 1}}, Range: Range{0, 2}}, 48, true},
		{&Struct{Members: []Member{{"a", tInt}, {"b", tReal}}}, 96, true},
		{&Union{Members: []Member{{"a", tInt}, {"b", tReal}}}, 64, true},
		{&Struct{Members: []Member{{"a", String}}}, 0, false},
		{&DynamicArray{Elem: tInt}, 0, false},
		{&Queue{Elem: tInt, Bound: -1}, 0, false},
		{&AssocArray{Elem: tInt}, 0, false},
		{&Class{Name: "C"}, 0, false},
		{Chandle, 0, false},
		{Void, 0, false},
		{Invalid, 0, false},
		{&Alias{Name: "w", Target: tShort}, 32, true},
		{&UnpackedArray{Elem: &UnpackedArray{Elem: tInt, Range: Range{0, 1 << 19}}, Range: Range{0, 1 << 19}}, 0, false}, // overflow guard
	}
	for _, c := range cases {
		got, ok := Bits(c.t)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Bits(%v) = %d, %v; want %d, %v", c.t, got, ok, c.want, c.ok)
		}
	}
	if _, ok := Bits(nil); ok {
		t.Error("Bits(nil)")
	}
}

func TestResolveAliases(t *testing.T) {
	a := &Alias{Name: "a", Target: tInt}
	b := &Alias{Name: "b", Target: a}
	if Resolve(b) != Type(tInt) {
		t.Error("Resolve should follow a chain")
	}
	loop := &Alias{Name: "loop"}
	loop.Target = loop
	if Resolve(loop) != Invalid {
		t.Error("a cyclic alias resolves to Invalid")
	}
	if Resolve(nil) != Invalid {
		t.Error("Resolve(nil)")
	}
	if !Matches(b, tInt) || !Equivalent(b, tInt) {
		t.Error("an alias matches its target")
	}
}

func TestMatches(t *testing.T) {
	st := &Struct{Members: []Member{{"a", tInt}}}
	st2 := &Struct{Members: []Member{{"a", tInt}}}
	en := &Enum{Base: tInt}
	cl := &Class{Name: "C"}
	cases := []struct {
		a, b Type
		want bool
	}{
		{tInt, tInt, true},
		{tInt, &Integral{Base: BaseInt, Signed: true}, true},
		{tInt, &Integral{Base: BaseInt}, false},
		{tInt, tInteger, false},
		{tLogic, i(BaseReg), true}, // reg is a synonym for logic
		{vec(BaseLogic, false, 7, 0), vec(BaseReg, false, 7, 0), true},
		{vec(BaseLogic, false, 7, 0), vec(BaseLogic, false, 8, 1), false},
		{vec(BaseLogic, false, 7, 0), vec(BaseLogic, true, 7, 0), false},
		{vec(BaseLogic, false, 7, 0), vec(BaseBit, false, 7, 0), false},
		{tLogic, vec(BaseLogic, false, 0, 0), false}, // a scalar is not a 1-bit vector
		{tReal, &Real{KindReal}, true},
		{tReal, tShort, false},
		{String, String, true},
		{String, Chandle, false},
		{st, st, true},
		{st, st2, false}, // separate declarations
		{en, en, true},
		{en, &Enum{Base: tInt}, false},
		{cl, cl, true},
		{cl, &Class{Name: "C"}, false},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &UnpackedArray{Elem: tInt, Range: Range{0, 3}}, true},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &UnpackedArray{Elem: tInt, Range: Range{3, 0}}, false},
		{&DynamicArray{Elem: tInt}, &DynamicArray{Elem: tInt}, true},
		{&DynamicArray{Elem: tInt}, &DynamicArray{Elem: tByte}, false},
		{&Queue{Elem: tInt, Bound: -1}, &Queue{Elem: tInt, Bound: 4}, false},
		{&AssocArray{Elem: tInt}, &AssocArray{Elem: tInt}, true},
		{&AssocArray{Elem: tInt}, &AssocArray{Elem: tInt, Index: String}, false},
		{&VirtualInterface{Interface: "b"}, &VirtualInterface{Interface: "b"}, true},
		{&VirtualInterface{Interface: "b"}, &VirtualInterface{Interface: "b", Modport: "m"}, false},
		{Invalid, Invalid, false},
		{nil, tInt, false},
	}
	for _, c := range cases {
		if got := Matches(c.a, c.b); got != c.want {
			t.Errorf("Matches(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := Matches(c.b, c.a); got != c.want {
			t.Errorf("Matches(%v, %v) = %v, want %v (symmetry)", c.b, c.a, got, c.want)
		}
	}
}

func TestEquivalent(t *testing.T) {
	st1 := &Struct{Packed: true, Members: []Member{{"a", vec(BaseBit, false, 7, 0)}, {"b", vec(BaseBit, false, 7, 0)}}}
	st2 := &Struct{Packed: true, Members: []Member{{"x", vec(BaseBit, false, 15, 0)}}}
	cases := []struct {
		a, b Type
		want bool
	}{
		{vec(BaseLogic, false, 7, 0), vec(BaseLogic, false, 8, 1), true}, // same width, sign, state
		{vec(BaseLogic, false, 7, 0), vec(BaseLogic, false, 0, 7), true},
		{vec(BaseLogic, false, 7, 0), vec(BaseBit, false, 7, 0), false}, // 2-state vs 4-state
		{vec(BaseLogic, false, 7, 0), vec(BaseLogic, true, 7, 0), false},
		{vec(BaseBit, true, 31, 0), tInt, true}, // bit signed [31:0] == int
		{vec(BaseLogic, true, 31, 0), tInteger, true},
		{tInt, tInteger, false},
		{tByte, vec(BaseBit, true, 7, 0), true},
		{&PackedArray{Elem: vec(BaseBit, false, 7, 0), Range: Range{1, 0}}, vec(BaseBit, false, 15, 0), true},
		{st1, st2, true},
		{st1, vec(BaseBit, false, 15, 0), true},
		{&Enum{Base: tInt}, tInt, false}, // enums are not in the packed rule
		{tReal, tShort, false},
		{&UnpackedArray{Elem: vec(BaseBit, false, 7, 0), Range: Range{0, 3}}, &UnpackedArray{Elem: vec(BaseBit, false, 8, 1), Range: Range{3, 0}}, true},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &UnpackedArray{Elem: tInt, Range: Range{0, 4}}, false},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &DynamicArray{Elem: tInt}, false},
		{&DynamicArray{Elem: tByte}, &DynamicArray{Elem: vec(BaseBit, true, 7, 0)}, true},
		{&Queue{Elem: tInt, Bound: 3}, &Queue{Elem: tInt, Bound: 3}, true},
		{&Queue{Elem: tInt, Bound: 3}, &Queue{Elem: tInt, Bound: 4}, false},
		{&Struct{Members: []Member{{"a", tInt}}}, &Struct{Members: []Member{{"a", tInt}}}, false},
		{Invalid, tInt, false},
	}
	for _, c := range cases {
		if got := Equivalent(c.a, c.b); got != c.want {
			t.Errorf("Equivalent(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := Equivalent(c.b, c.a); got != c.want {
			t.Errorf("Equivalent(%v, %v) = %v, want %v (symmetry)", c.b, c.a, got, c.want)
		}
	}
}

func TestAssignment(t *testing.T) {
	base := &Class{Name: "Base"}
	mid := &Class{Name: "Mid", Super: base}
	leaf := &Class{Name: "Leaf", Super: mid}
	other := &Class{Name: "Other"}
	color := &Enum{Name: "color_t", Base: tInt}
	shape := &Enum{Name: "shape_t", Base: tInt}
	pk := &Struct{Packed: true, Members: []Member{{"a", vec(BaseBit, false, 7, 0)}}}
	un := &Struct{Members: []Member{{"a", tInt}}}
	cases := []struct {
		dst, src Type
		want     Compat
	}{
		{tInt, tByte, Assignable},
		{tByte, tInt, Assignable}, // narrowing is legal, if lossy
		{tInt, tReal, Assignable},
		{tReal, tLogic, Assignable},
		{vec(BaseLogic, false, 7, 0), tInt, Assignable},
		{tInt, color, Assignable},
		{color, color, Assignable},
		{color, tInt, CastRequired},
		{color, shape, CastRequired},
		{color, tReal, Incompatible},
		{pk, vec(BaseBit, false, 7, 0), Assignable},
		{pk, tInt, Assignable},
		{tInt, pk, Assignable},
		{tInt, un, Incompatible},
		{un, un, Assignable},
		{un, &Struct{Members: []Member{{"a", tInt}}}, Incompatible},
		{String, String, Assignable},
		{String, vec(BaseBit, false, 23, 0), Assignable},
		{tInt, String, CastRequired},
		{String, tReal, Incompatible},
		{base, leaf, Assignable},
		{mid, leaf, Assignable},
		{leaf, base, CastRequired},
		{base, other, Incompatible},
		{base, Null, Assignable},
		{base, tInt, Incompatible},
		{Chandle, Null, Assignable},
		{Chandle, Chandle, Assignable},
		{Chandle, tInt, Incompatible},
		{Event, Event, Assignable},
		{Event, Null, Assignable},
		{&VirtualInterface{Interface: "b"}, Null, Assignable},
		{&VirtualInterface{Interface: "b"}, &VirtualInterface{Interface: "c"}, Incompatible},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &UnpackedArray{Elem: tInt, Range: Range{3, 0}}, Assignable},
		{&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &UnpackedArray{Elem: tInt, Range: Range{0, 4}}, Assignable}, // size checked at run time
		{&DynamicArray{Elem: tInt}, &UnpackedArray{Elem: tInt, Range: Range{0, 3}}, Assignable},
		{&Queue{Elem: tInt, Bound: -1}, &DynamicArray{Elem: tInt}, Assignable},
		{&DynamicArray{Elem: tInt}, &DynamicArray{Elem: String}, Incompatible},
		{&DynamicArray{Elem: tInt}, tInt, Incompatible},
		{Void, tInt, Incompatible},
		{tInt, Void, Incompatible},
		{tInt, Invalid, Incompatible},
		{Invalid, tInt, Incompatible},
	}
	for _, c := range cases {
		if got := Assignment(c.dst, c.src); got != c.want {
			t.Errorf("Assignment(%v <- %v) = %v, want %v", c.dst, c.src, got, c.want)
		}
	}
}

func TestCastable(t *testing.T) {
	base := &Class{Name: "Base"}
	leaf := &Class{Name: "Leaf", Super: base}
	color := &Enum{Name: "color_t", Base: tInt}
	cases := []struct {
		dst, src Type
		want     bool
	}{
		{color, tInt, true},
		{color, tReal, true},
		{tInt, color, true},
		{tReal, tLogic, true},
		{String, tInt, true},
		{tInt, String, true},
		{String, tReal, false},
		{leaf, base, true},
		{base, leaf, true},
		{base, &Class{Name: "Other"}, false},
		{base, Null, true},
		{tInt, Chandle, false},
		{Invalid, tInt, false},
	}
	for _, c := range cases {
		if got := Castable(c.dst, c.src); got != c.want {
			t.Errorf("Castable(%v <- %v) = %v, want %v", c.dst, c.src, got, c.want)
		}
	}
}

func TestClassDerivationTerminatesOnCycles(t *testing.T) {
	a := &Class{Name: "A"}
	b := &Class{Name: "B", Super: a}
	a.Super = b // an illegal program
	if a.DerivesFrom(&Class{Name: "Z"}) {
		t.Error("cycle: unrelated class")
	}
	if !a.DerivesFrom(b) {
		t.Error("cycle: b is reachable")
	}
	var nilClass *Class
	if nilClass.DerivesFrom(a) {
		t.Error("nil class derives from nothing")
	}
}

func TestEnumMembersHoldValues(t *testing.T) {
	e := &Enum{Name: "e", Base: tInt, Members: []EnumMember{{"A", svint.FromInt64(32, 0)}, {"B", svint.FromInt64(32, 5)}}}
	if n, _ := e.Members[1].Value.Int64(); n != 5 {
		t.Error("enum member value")
	}
}

// Properties that must hold for any pair of types.
func TestRelationProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pool := []Type{
		tInt, tInteger, tByte, tLogic, tBit, tReal, tShort, String, Chandle, Event, Null, Void, Invalid,
		vec(BaseLogic, false, 7, 0), vec(BaseBit, true, 31, 0), vec(BaseReg, false, 7, 0),
		&PackedArray{Elem: vec(BaseBit, false, 7, 0), Range: Range{1, 0}},
		&UnpackedArray{Elem: tInt, Range: Range{0, 3}}, &DynamicArray{Elem: tInt}, &Queue{Elem: tInt, Bound: -1},
		&AssocArray{Elem: tInt}, &Struct{Packed: true, Members: []Member{{"a", tLogic}}}, &Struct{Members: []Member{{"a", tInt}}},
		&Enum{Base: tInt}, &Class{Name: "C"}, &VirtualInterface{Interface: "b"}, &Alias{Name: "t", Target: tInt},
	}
	for n := 0; n < 5000; n++ {
		a, b := pool[rng.Intn(len(pool))], pool[rng.Intn(len(pool))]
		if Matches(a, b) != Matches(b, a) {
			t.Fatalf("Matches not symmetric: %v, %v", a, b)
		}
		if Equivalent(a, b) != Equivalent(b, a) {
			t.Fatalf("Equivalent not symmetric: %v, %v", a, b)
		}
		if Matches(a, b) && !Equivalent(a, b) {
			t.Fatalf("matching types must be equivalent: %v, %v", a, b)
		}
		if a == Void || b == Void {
			continue // void is no value type: nothing converts to or from it
		}
		if Equivalent(a, b) && Assignment(a, b) != Assignable {
			t.Fatalf("equivalent types must be assignable: %v, %v", a, b)
		}
		if Assignment(a, b) == Assignable && !Castable(a, b) {
			t.Fatalf("assignable types must be castable: %v <- %v", a, b)
		}
		if !IsInvalid(a) && a != Void && !IsInvalid(Resolve(a)) && Assignment(a, a) != Assignable {
			t.Fatalf("a type must be assignable to itself: %v", a)
		}
		_ = Castable(a, b)
	}
}
