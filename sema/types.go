// Package sema is the semantic model of a SystemVerilog design: the types of
// LRM 6, and (in later files) the symbol tables, scopes and bound expressions
// built from the syntax tree. This file is the type system.
//
// A Type is immutable once built. Types declared with a name or a body (struct,
// union, enum, class) have identity: two separate declarations are different
// types even if they look alike (LRM 6.22.1), so they are compared by pointer.
package sema

import (
	"fmt"
	"strings"

	"github.com/jfetkotto/svparse/svint"
)

// Type is implemented by every type in this file. It is sealed.
type Type interface {
	// String spells the type as SystemVerilog source would.
	String() string
	isType()
}

// Base is the keyword of an integral built-in type.
type Base uint8

const (
	BaseBit Base = iota
	BaseLogic
	BaseReg
	BaseByte
	BaseShortint
	BaseInt
	BaseLongint
	BaseInteger
	BaseTime
)

var baseNames = [...]string{"bit", "logic", "reg", "byte", "shortint", "int", "longint", "integer", "time"}

func (b Base) String() string {
	if int(b) >= len(baseNames) {
		return "?"
	}
	return baseNames[b]
}

// ParseBase maps a keyword to its Base.
func ParseBase(name string) (Base, bool) {
	for i, n := range baseNames {
		if n == name {
			return Base(i), true
		}
	}
	return 0, false
}

// vector reports whether b is one of the types that take a packed range.
func (b Base) vector() bool { return b == BaseBit || b == BaseLogic || b == BaseReg }

// fourState reports whether the type holds x and z (LRM 6.11.2).
func (b Base) fourState() bool {
	return b == BaseLogic || b == BaseReg || b == BaseInteger || b == BaseTime
}

// defaultSigned is the signedness when the source says nothing (LRM 6.11.3).
func (b Base) defaultSigned() bool {
	switch b {
	case BaseByte, BaseShortint, BaseInt, BaseLongint, BaseInteger:
		return true
	}
	return false
}

func (b Base) atomWidth() int {
	switch b {
	case BaseByte:
		return 8
	case BaseShortint:
		return 16
	case BaseInt, BaseInteger:
		return 32
	case BaseLongint, BaseTime:
		return 64
	}
	return 1
}

// Range is a dimension [Left:Right]. Left may be below Right (an ascending
// range), which matters for how bit and element indices map to positions.
type Range struct{ Left, Right int }

// Size is the number of elements the range spans.
func (r Range) Size() int {
	if r.Left >= r.Right {
		return r.Left - r.Right + 1
	}
	return r.Right - r.Left + 1
}

func (r Range) String() string { return fmt.Sprintf("[%d:%d]", r.Left, r.Right) }

// maxElems bounds any array or vector size, so that widths stay within what
// svint can represent and arithmetic on them cannot overflow.
const maxElems = svint.MaxWidth

// Integral is a built-in integral type: bit, logic, reg, byte, shortint, int,
// longint, integer or time (LRM 6.11). A bit, logic or reg may carry a packed
// range, making it a vector; the other types never do.
type Integral struct {
	Base     Base
	Signed   bool
	HasRange bool
	Range    Range
}

// NewIntegral returns the built-in type with the signedness the keyword implies.
func NewIntegral(b Base) *Integral { return &Integral{Base: b, Signed: b.defaultSigned()} }

// Vector returns a bit, logic or reg vector. It returns Invalid for any other
// base or for an absurd size.
func Vector(b Base, signed bool, r Range) Type {
	if !b.vector() || r.Size() > maxElems || r.Size() < 1 {
		return Invalid
	}
	return &Integral{Base: b, Signed: signed, HasRange: true, Range: r}
}

// Width is the number of bits.
func (t *Integral) Width() int {
	if t.HasRange {
		return t.Range.Size()
	}
	return t.Base.atomWidth()
}

// FourState reports whether the type can hold x and z.
func (t *Integral) FourState() bool { return t.Base.fourState() }

func (t *Integral) isType() {}

func (t *Integral) String() string {
	s := t.Base.String()
	if t.Signed != t.Base.defaultSigned() {
		if t.Signed {
			s += " signed"
		} else {
			s += " unsigned"
		}
	}
	if t.HasRange {
		s += " " + t.Range.String()
	}
	return s
}

// RealKind picks among the real types.
type RealKind uint8

const (
	KindReal RealKind = iota
	KindShortreal
	KindRealtime
)

// Real is real, shortreal or realtime (LRM 6.12).
type Real struct{ Kind RealKind }

func (t *Real) isType() {}

// Bits is 64 for real and realtime and 32 for shortreal.
func (t *Real) Bits() int {
	if t.Kind == KindShortreal {
		return 32
	}
	return 64
}

func (t *Real) String() string { return [...]string{"real", "shortreal", "realtime"}[t.Kind%3] }

type simple struct{ name string }

func (t *simple) isType()        {}
func (t *simple) String() string { return t.name }

// The types with no parameters. Each is a singleton, compared by identity.
var (
	String  Type = &simple{"string"}
	Chandle Type = &simple{"chandle"}
	Event   Type = &simple{"event"}
	Void    Type = &simple{"void"}
	// Null is the type of the null literal (LRM 6.13, 8.4): assignable to a
	// class handle, chandle, event or virtual interface.
	Null Type = &simple{"null"}
	// Invalid stands for a type that could not be resolved. Every relation
	// involving it answers "no", so one error does not cascade.
	Invalid Type = &simple{"<invalid>"}
)

// IsInvalid reports whether t is Invalid (or nil).
func IsInvalid(t Type) bool { return t == nil || t == Invalid }

// Alias is a typedef: a name for another type that matches it (LRM 6.18).
type Alias struct {
	Name   string
	Target Type
}

func (t *Alias) isType()        {}
func (t *Alias) String() string { return t.Name }

// Resolve strips typedef aliases. A chain of aliases ends at a real type; a
// cycle (which a bad program can produce) ends at Invalid.
func Resolve(t Type) Type {
	for i := 0; i < 1000; i++ {
		a, ok := t.(*Alias)
		if !ok {
			if t == nil {
				return Invalid
			}
			return t
		}
		t = a.Target
	}
	return Invalid
}

// PackedArray is a packed dimension over a packed element type, such as the
// outer [3:0] of "logic [3:0][7:0]" (LRM 7.4.1).
type PackedArray struct {
	Elem  Type
	Range Range
}

func (t *PackedArray) isType() {}

func (t *PackedArray) String() string { return t.Elem.String() + " " + t.Range.String() }

// UnpackedArray is a fixed-size unpacked array (LRM 7.4.2).
type UnpackedArray struct {
	Elem  Type
	Range Range
}

func (t *UnpackedArray) isType()        {}
func (t *UnpackedArray) String() string { return t.Elem.String() + " " + t.Range.String() }

// DynamicArray is "T name[]" (LRM 7.5).
type DynamicArray struct{ Elem Type }

func (t *DynamicArray) isType()        {}
func (t *DynamicArray) String() string { return t.Elem.String() + " []" }

// Queue is "T name[$]" or "T name[$:N]" (LRM 7.10). Bound is -1 when unbounded.
type Queue struct {
	Elem  Type
	Bound int
}

func (t *Queue) isType() {}
func (t *Queue) String() string {
	if t.Bound >= 0 {
		return fmt.Sprintf("%s [$:%d]", t.Elem, t.Bound)
	}
	return t.Elem.String() + " [$]"
}

// AssocArray is "T name[K]" (LRM 7.8). Index is nil for the wildcard "[*]".
type AssocArray struct {
	Elem  Type
	Index Type
}

func (t *AssocArray) isType() {}
func (t *AssocArray) String() string {
	if t.Index == nil {
		return t.Elem.String() + " [*]"
	}
	return fmt.Sprintf("%s [%s]", t.Elem, t.Index)
}

// Member is a field of a struct or union.
type Member struct {
	Name string
	Type Type
}

// Struct is a struct type (LRM 7.2). A packed struct is an integral type whose
// members are packed.
type Struct struct {
	Name    string // typedef name, if any, for printing
	Packed  bool
	Signed  bool
	Members []Member
}

func (t *Struct) isType() {}
func (t *Struct) String() string {
	return aggregateString("struct", t.Name, t.Packed, t.Signed, false, t.Members)
}

// Union is a union type (LRM 7.3).
type Union struct {
	Name    string
	Packed  bool
	Signed  bool
	Tagged  bool
	Members []Member
}

func (t *Union) isType() {}
func (t *Union) String() string {
	return aggregateString("union", t.Name, t.Packed, t.Signed, t.Tagged, t.Members)
}

func aggregateString(kw, name string, packed, signed, tagged bool, members []Member) string {
	if name != "" {
		return name
	}
	var sb strings.Builder
	sb.WriteString(kw)
	if tagged {
		sb.WriteString(" tagged")
	}
	if packed {
		sb.WriteString(" packed")
	}
	if signed {
		sb.WriteString(" signed")
	}
	sb.WriteString(" {")
	for i, m := range members {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(m.Type.String() + " " + m.Name + ";")
	}
	sb.WriteString("}")
	return sb.String()
}

// EnumMember is a named constant of an enum.
type EnumMember struct {
	Name  string
	Value svint.Value
}

// Enum is an enumerated type (LRM 6.19). Base is its integral base type.
type Enum struct {
	Name    string
	Base    Type
	Members []EnumMember
}

func (t *Enum) isType() {}
func (t *Enum) String() string {
	if t.Name != "" {
		return t.Name
	}
	names := make([]string, len(t.Members))
	for i, m := range t.Members {
		names[i] = m.Name
	}
	return "enum {" + strings.Join(names, ", ") + "}"
}

// Class is a class type (LRM 8). Super is the base class, or nil.
type Class struct {
	Name  string
	Super *Class
}

func (t *Class) isType()        {}
func (t *Class) String() string { return t.Name }

// DerivesFrom reports whether t is base or inherits from it, directly or not.
func (t *Class) DerivesFrom(base *Class) bool {
	for i := 0; t != nil && i < 10000; i++ {
		if t == base {
			return true
		}
		t = t.Super
	}
	return false
}

// VirtualInterface is "virtual Interface[.Modport]" (LRM 25.9).
type VirtualInterface struct {
	Interface string
	Modport   string
}

func (t *VirtualInterface) isType() {}
func (t *VirtualInterface) String() string {
	if t.Modport != "" {
		return "virtual " + t.Interface + "." + t.Modport
	}
	return "virtual " + t.Interface
}

// ---- classification (LRM 6.11) ----

// IsIntegral reports whether t is an integral type: a built-in integral type,
// a packed array or packed struct or union, or an enum (LRM 6.11.1).
func IsIntegral(t Type) bool {
	switch t := Resolve(t).(type) {
	case *Integral, *Enum:
		return true
	case *PackedArray:
		return true
	case *Struct:
		return t.Packed
	case *Union:
		return t.Packed
	}
	return false
}

// IsNumeric reports whether t is integral or real.
func IsNumeric(t Type) bool {
	if _, ok := Resolve(t).(*Real); ok {
		return true
	}
	return IsIntegral(t)
}

// IsReal reports whether t is real, shortreal or realtime.
func IsReal(t Type) bool { _, ok := Resolve(t).(*Real); return ok }

// IsSigned reports whether an integral type is signed. An enum takes its base
// type's signedness; a packed array its element's.
func IsSigned(t Type) bool {
	switch t := Resolve(t).(type) {
	case *Integral:
		return t.Signed
	case *Enum:
		return IsSigned(t.Base)
	case *PackedArray:
		return IsSigned(t.Elem)
	case *Struct:
		return t.Packed && t.Signed
	case *Union:
		return t.Packed && t.Signed
	}
	return false
}

// IsFourState reports whether an integral type can hold x and z.
func IsFourState(t Type) bool {
	switch t := Resolve(t).(type) {
	case *Integral:
		return t.FourState()
	case *Enum:
		return IsFourState(t.Base)
	case *PackedArray:
		return IsFourState(t.Elem)
	case *Struct:
		if !t.Packed {
			return false
		}
		for _, m := range t.Members {
			if IsFourState(m.Type) {
				return true
			}
		}
	case *Union:
		if !t.Packed {
			return false
		}
		for _, m := range t.Members {
			if IsFourState(m.Type) {
				return true
			}
		}
	}
	return false
}

// IsAggregate reports whether t is an unpacked struct, union or array.
func IsAggregate(t Type) bool {
	switch t := Resolve(t).(type) {
	case *UnpackedArray, *DynamicArray, *Queue, *AssocArray:
		return true
	case *Struct:
		return !t.Packed
	case *Union:
		return !t.Packed
	}
	return false
}

// Bits is the static size of t in bits, as $bits (LRM 20.6.2) reports it. It
// fails for types with no static size: strings, classes, dynamic arrays,
// queues, associative arrays, chandles, events, void.
func Bits(t Type) (int, bool) {
	switch t := Resolve(t).(type) {
	case *Integral:
		return t.Width(), true
	case *Real:
		return t.Bits(), true
	case *Enum:
		return Bits(t.Base)
	case *PackedArray:
		return times(t.Range.Size(), t.Elem)
	case *UnpackedArray:
		return times(t.Range.Size(), t.Elem)
	case *Struct:
		total := 0
		for _, m := range t.Members {
			n, ok := Bits(m.Type)
			if !ok {
				return 0, false
			}
			total += n
			if total > maxElems {
				return 0, false
			}
		}
		return total, true
	case *Union:
		widest := 0
		for _, m := range t.Members {
			n, ok := Bits(m.Type)
			if !ok {
				return 0, false
			}
			widest = max(widest, n)
		}
		return widest, true
	}
	return 0, false
}

func times(n int, elem Type) (int, bool) {
	b, ok := Bits(elem)
	if !ok || n < 1 || b > maxElems/n {
		return 0, false
	}
	return n * b, true
}

// ---- type relations (LRM 6.22) ----

// Matches reports whether a and b are matching types (LRM 6.22.1): the same
// type, ignoring typedef aliases. Built-in types match when they name the same
// type with the same signing and range (reg is a synonym of logic, and int
// matches int signed). Declared structs, unions, enums and classes match only
// themselves.
func Matches(a, b Type) bool {
	a, b = Resolve(a), Resolve(b)
	if IsInvalid(a) || IsInvalid(b) {
		return false
	}
	if a == b {
		return true
	}
	switch a := a.(type) {
	case *Integral:
		b, ok := b.(*Integral)
		return ok && canonBase(a.Base) == canonBase(b.Base) && a.Signed == b.Signed &&
			a.HasRange == b.HasRange && (!a.HasRange || a.Range == b.Range)
	case *Real:
		b, ok := b.(*Real)
		return ok && a.Kind == b.Kind
	case *PackedArray:
		b, ok := b.(*PackedArray)
		return ok && a.Range == b.Range && Matches(a.Elem, b.Elem)
	case *UnpackedArray:
		b, ok := b.(*UnpackedArray)
		return ok && a.Range == b.Range && Matches(a.Elem, b.Elem)
	case *DynamicArray:
		b, ok := b.(*DynamicArray)
		return ok && Matches(a.Elem, b.Elem)
	case *Queue:
		b, ok := b.(*Queue)
		return ok && a.Bound == b.Bound && Matches(a.Elem, b.Elem)
	case *AssocArray:
		b, ok := b.(*AssocArray)
		return ok && Matches(a.Elem, b.Elem) && ((a.Index == nil && b.Index == nil) || (a.Index != nil && b.Index != nil && Matches(a.Index, b.Index)))
	case *VirtualInterface:
		b, ok := b.(*VirtualInterface)
		return ok && *a == *b
	}
	return false
}

// canonBase folds reg into logic, its synonym.
func canonBase(b Base) Base {
	if b == BaseReg {
		return BaseLogic
	}
	return b
}

// packedLike reports whether t takes part in the packed-equivalence rule: the
// built-in integral types and packed arrays, structs and unions, but not enums.
func packedLike(t Type) bool {
	switch t := t.(type) {
	case *Integral, *PackedArray:
		return true
	case *Struct:
		return t.Packed
	case *Union:
		return t.Packed
	}
	return false
}

// Equivalent reports whether a and b are equivalent types (LRM 6.22.2):
// matching types, packed integral types of the same width, signedness and
// state-ness, or unpacked arrays of equivalent elements with the same shape.
func Equivalent(a, b Type) bool {
	a, b = Resolve(a), Resolve(b)
	if IsInvalid(a) || IsInvalid(b) {
		return false
	}
	if Matches(a, b) {
		return true
	}
	if packedLike(a) && packedLike(b) {
		wa, okA := Bits(a)
		wb, okB := Bits(b)
		return okA && okB && wa == wb && IsSigned(a) == IsSigned(b) && IsFourState(a) == IsFourState(b)
	}
	switch a := a.(type) {
	case *UnpackedArray:
		b, ok := b.(*UnpackedArray)
		return ok && a.Range.Size() == b.Range.Size() && Equivalent(a.Elem, b.Elem)
	case *DynamicArray:
		b, ok := b.(*DynamicArray)
		return ok && Equivalent(a.Elem, b.Elem)
	case *Queue:
		b, ok := b.(*Queue)
		return ok && a.Bound == b.Bound && Equivalent(a.Elem, b.Elem)
	case *AssocArray:
		b, ok := b.(*AssocArray)
		return ok && Equivalent(a.Elem, b.Elem) && ((a.Index == nil && b.Index == nil) || (a.Index != nil && b.Index != nil && Equivalent(a.Index, b.Index)))
	}
	return false
}

// Compat is how readily a value of one type converts to another.
type Compat uint8

const (
	// Incompatible: no conversion exists.
	Incompatible Compat = iota
	// CastRequired: legal only through an explicit cast (or $cast).
	CastRequired
	// Assignable: an assignment converts implicitly.
	Assignable
)

func (c Compat) String() string {
	return [...]string{"incompatible", "cast required", "assignable"}[c%3]
}

// Assignment reports how a value of type src converts to type dst in an
// assignment (LRM 6.22.3): numeric types convert freely among themselves
// except into an enum, which needs a cast; a class handle converts to its base
// classes and a base handle needs a cast to a derived one; arrays assign when
// their elements are equivalent; other types need an equivalent type.
func Assignment(dst, src Type) Compat {
	dst, src = Resolve(dst), Resolve(src)
	if IsInvalid(dst) || IsInvalid(src) || dst == Void || src == Void {
		return Incompatible
	}
	if Equivalent(dst, src) {
		return Assignable
	}
	switch d := dst.(type) {
	case *Enum:
		if IsIntegral(src) {
			return CastRequired
		}
	case *Integral, *PackedArray, *Real:
		if IsNumeric(src) {
			return Assignable
		}
		if src == String {
			return CastRequired
		}
	case *Struct:
		if d.Packed && IsNumeric(src) {
			return Assignable
		}
	case *Union:
		if d.Packed && IsNumeric(src) {
			return Assignable
		}
	}
	switch {
	case dst == String:
		if IsIntegral(src) {
			return Assignable
		}
	case dst == Chandle || dst == Event:
		if src == Null {
			return Assignable
		}
	}
	switch d := dst.(type) {
	case *Class:
		if src == Null {
			return Assignable
		}
		if s, ok := src.(*Class); ok {
			switch {
			case s.DerivesFrom(d):
				return Assignable
			case d.DerivesFrom(s):
				return CastRequired
			}
		}
	case *VirtualInterface:
		if src == Null {
			return Assignable
		}
	case *UnpackedArray, *DynamicArray, *Queue:
		if elemEquivalent(elemOf(d), elemOf(src)) && isSequence(src) {
			return Assignable
		}
	}
	return Incompatible
}

func isSequence(t Type) bool {
	switch t.(type) {
	case *UnpackedArray, *DynamicArray, *Queue:
		return true
	}
	return false
}

func elemOf(t Type) Type {
	switch t := t.(type) {
	case *UnpackedArray:
		return t.Elem
	case *DynamicArray:
		return t.Elem
	case *Queue:
		return t.Elem
	}
	return Invalid
}

func elemEquivalent(a, b Type) bool { return !IsInvalid(a) && !IsInvalid(b) && Equivalent(a, b) }

// Castable reports whether a cast from src to dst is legal (LRM 6.24.1): among
// numeric types and enums freely, between strings and integral types, between
// class handles related by inheritance in either direction, and otherwise
// wherever an assignment is legal.
func Castable(dst, src Type) bool {
	dst, src = Resolve(dst), Resolve(src)
	if IsInvalid(dst) || IsInvalid(src) {
		return false
	}
	if IsNumeric(dst) && IsNumeric(src) {
		return true
	}
	if (dst == String && IsIntegral(src)) || (IsIntegral(dst) && src == String) {
		return true
	}
	if d, ok := dst.(*Class); ok {
		if s, ok := src.(*Class); ok {
			return s.DerivesFrom(d) || d.DerivesFrom(s)
		}
	}
	return Assignment(dst, src) != Incompatible
}
