package ast

// The items below are the module-level constructs that carry no data type:
// time units, hierarchical overrides and bindings, macros, aliases, exports,
// clocking and configurations. Names written as hierarchical references are
// kept as source text (Target, Instances) because resolving them is the job
// of a later stage.

// TimeUnit is "timeunit 1ns / 1ps;" or "timeprecision 1ps;" (LRM 3.14.2.3).
// Keyword is "timeunit" or "timeprecision". Value is the time literal as
// written ("1ns"); Divisor is the precision after '/', empty if absent.
type TimeUnit struct {
	declBase
	Keyword string
	Value   string
	Divisor string
}

// Defparam is "defparam u1.W = 8, u2.W = 4;" (LRM 23.10.1). Each entry of
// Assigns has Op "=", the hierarchical parameter name as LHS and the value as
// RHS.
type Defparam struct {
	declBase
	Assigns []Assign
}

// Bind is "bind target [: instances] instantiation;" (LRM 23.11). Target is
// the module, interface or hierarchical instance the instantiation is bound
// into; Instances lists the "bind m : a.b, c" form's instance names. Inst is
// the instantiation being added.
type Bind struct {
	declBase
	Target    string
	Instances []string
	Inst      *Instantiation
}

// Let is "let name [(ports)] = expr;" (LRM 11.13).
type Let struct {
	declBase
	Name  string
	Ports []Port
	Body  Expr
}

// Alias is "alias a = b = c;" (LRM 10.12), two or more nets sharing one
// connection.
type Alias struct {
	declBase
	Nets []Expr
}

// Export is a package export, "export pkg::name;", "export pkg::*;" or
// "export *::*;" (LRM 26.6). Member is "*" for a wildcard, and so may Package
// be.
type Export struct {
	declBase
	Package string
	Member  string
}

// DPIExport is "export "DPI-C" [c_name =] function name;" (LRM 35.7). Kind is
// "function" or "task".
type DPIExport struct {
	declBase
	Spec  string
	CName string
	Kind  string
	Name  string
}

// DefaultClocking is "default clocking name;", naming an already declared
// clocking block as the default (LRM 14.12). The form that declares the block
// is a Clocking with Default set.
type DefaultClocking struct {
	declBase
	Name string
}

// DefaultDisable is "default disable iff expr;" (LRM 16.15).
type DefaultDisable struct {
	declBase
	Cond Expr
}

// Clocking is a clocking block (LRM 14.3): "[default|global] clocking [name]
// @(event); items endclocking". Event is the clocking event.
type Clocking struct {
	declBase
	Name         string
	Default      bool
	Global       bool
	Event        *TimingControl
	Items        []ClockingItem
	EndLine      int
	EndCharacter int
}

// ClockingItem is one item of a clocking block. For "default input #1 output
// #0;" Default is true and no Signals are given. Otherwise Direction is
// "input", "output", "input output" or "inout" and Signals lists the names.
// Skews are kept as source text ("#1step", "negedge #2").
type ClockingItem struct {
	Position
	Default    bool
	Direction  string
	InputSkew  string
	OutputSkew string
	Signals    []ClockingSignal
}

// ClockingSignal is one signal of a clocking item, "name [= expr]".
type ClockingSignal struct {
	Position
	Name string
	Expr Expr
}

// Config is a configuration declaration (LRM 33.4): "config name; design
// lib.cell; rules endconfig". Design lists the top cells as written.
type Config struct {
	declBase
	Name         string
	Body         []Decl // localparam declarations
	Design       []string
	Rules        []ConfigRule
	EndLine      int
	EndCharacter int
}

// ConfigRule is one configuration rule, kept as source text. Kind is
// "default", "instance" or "cell"; Text is the whole rule without its
// semicolon.
type ConfigRule struct {
	Position
	Kind string
	Text string
}
