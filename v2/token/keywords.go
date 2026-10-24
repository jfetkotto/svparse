package token

// Keywords is the full set of reserved words defined by IEEE 1800-2017
// Annex B.6, transcribed in full rather than a representative subset --
// this is mechanical, low-risk work worth doing completely up front so a
// later parser never has to wonder whether a missing keyword is a real
// gap or just not yet transcribed. If a keyword turns out to be missing
// during Phase 3 (parser) development, add it here rather than special-
// casing it elsewhere.
var Keywords = map[string]bool{
	"accept_on": true, "alias": true, "always": true, "always_comb": true, "always_ff": true,
	"always_latch": true, "and": true, "assert": true, "assign": true, "assume": true, "automatic": true,

	"before": true, "begin": true, "bind": true, "bins": true, "binsof": true, "bit": true,
	"break": true, "buf": true, "bufif0": true, "bufif1": true, "byte": true,

	"case": true, "casex": true, "casez": true, "cell": true, "chandle": true, "checker": true,
	"class": true, "clocking": true, "cmos": true, "config": true, "const": true, "constraint": true,
	"context": true, "continue": true, "cover": true, "covergroup": true, "coverpoint": true, "cross": true,

	"deassign": true, "default": true, "defparam": true, "design": true, "disable": true,
	"dist": true, "do": true,

	"edge": true, "else": true, "end": true, "endcase": true, "endchecker": true, "endclass": true,
	"endclocking": true, "endconfig": true, "endfunction": true, "endgenerate": true, "endgroup": true,
	"endinterface": true, "endmodule": true, "endpackage": true, "endprimitive": true, "endprogram": true,
	"endproperty": true, "endspecify": true, "endsequence": true, "endtable": true, "endtask": true,
	"enum": true, "event": true, "eventually": true, "expect": true, "export": true, "extends": true,
	"extern": true,

	"final": true, "first_match": true, "for": true, "force": true, "foreach": true, "forever": true,
	"fork": true, "forkjoin": true, "function": true,

	"generate": true, "genvar": true, "global": true,

	"highz0": true, "highz1": true,

	"if": true, "iff": true, "ifnone": true, "ignore_bins": true, "illegal_bins": true,
	"implements": true, "implies": true, "import": true, "incdir": true, "include": true,
	"initial": true, "inout": true, "input": true, "inside": true, "instance": true, "int": true,
	"integer": true, "interconnect": true, "interface": true, "intersect": true,

	"join": true, "join_any": true, "join_none": true,

	"large": true, "let": true, "liblist": true, "library": true, "local": true, "localparam": true,
	"logic": true, "longint": true,

	"macromodule": true, "matches": true, "medium": true, "modport": true, "module": true,

	"nand": true, "negedge": true, "nettype": true, "new": true, "nexttime": true, "nmos": true,
	"nor": true, "noshowcancelled": true, "not": true, "notif0": true, "notif1": true, "null": true,

	"or": true, "output": true,

	"package": true, "packed": true, "parameter": true, "pmos": true, "posedge": true,
	"primitive": true, "priority": true, "program": true, "property": true, "protected": true,
	"pull0": true, "pull1": true, "pulldown": true, "pullup": true,
	"pulsestyle_ondetect": true, "pulsestyle_onevent": true, "pure": true,

	"rand": true, "randc": true, "randcase": true, "randsequence": true, "rcmos": true,
	"real": true, "realtime": true, "ref": true, "reg": true, "reject_on": true, "release": true,
	"repeat": true, "restrict": true, "return": true, "rnmos": true, "rpmos": true, "rtran": true,
	"rtranif0": true, "rtranif1": true,

	"s_always": true, "s_eventually": true, "s_nexttime": true, "s_until": true, "s_until_with": true,
	"scalared": true, "sequence": true, "shortint": true, "shortreal": true, "showcancelled": true,
	"signed": true, "small": true, "soft": true, "solve": true, "specify": true, "specparam": true,
	"static": true, "string": true, "strong": true, "strong0": true, "strong1": true, "struct": true,
	"super": true, "supply0": true, "supply1": true, "sync_accept_on": true, "sync_reject_on": true,

	"table": true, "tagged": true, "task": true, "this": true, "throughout": true, "time": true,
	"timeprecision": true, "timeunit": true, "tran": true, "tranif0": true, "tranif1": true,
	"tri": true, "tri0": true, "tri1": true, "triand": true, "trior": true, "trireg": true,
	"type": true, "typedef": true,

	"union": true, "unique": true, "unique0": true, "unsigned": true, "until": true,
	"until_with": true, "untyped": true, "use": true, "uwire": true,

	"var": true, "vectored": true, "virtual": true, "void": true,

	"wait": true, "wait_order": true, "wand": true, "weak": true, "weak0": true, "weak1": true,
	"while": true, "wildcard": true, "wire": true, "with": true, "within": true, "wor": true,

	"xnor": true, "xor": true,
}
