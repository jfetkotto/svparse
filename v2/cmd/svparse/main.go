// Command svparse checks, elaborates and lints SystemVerilog.
//
//	svparse check     [flags] files...   syntax, semantic checks and elaboration
//	svparse lint      [flags] files...   everything check does, plus lint rules
//	svparse elab      [flags] files...   print the elaborated instance hierarchy
//	svparse dump-ast  [flags] files...   print the syntax tree
//
// Flags:
//
//	-I dir            add an include directory (repeatable)
//	-D name[=value]   define a macro (repeatable)
//	--top name        elaborate this module as a root (repeatable)
//	-f listfile       read file names from a file, one per line
//	--config file     lint configuration (default: .svparse.json if present)
//	--format text|json
//	--werror          make warnings fail the run
//	--mfcu            share one $unit scope across all files
//	--default-nettype wire|none
//	--list-rules      (lint) print the available rules and exit
//
// The exit status is 0 when there are no errors, 1 when there are, and 2 for a
// usage or file error.
package main

import (
	"os"

	"github.com/jfetkotto/svparse/v2/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
