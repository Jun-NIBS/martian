//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Mario command-line compiler. Primarily used for unit testing.
//
package main

import (
	"fmt"
	"github.com/docopt/docopt-go"
	"mario/core"
	"os"
	"path"
	"path/filepath"
)

func main() {
	// Command-line arguments.
	doc := `Mario Compiler.

Usage:
    mrc <file.mro>... | --all
    mrc -h | --help | --version

Options:
    --all         Compile all files in $MROPATH.
    -h --help     Show this message.
    --version     Show version.`
	opts, _ := docopt.Parse(doc, nil, true, core.GetVersion(), false)

	core.ENABLE_LOGGING = false

	// Mario environment variables.
	cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	mroPath := cwd
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPath = value
	}

	// Setup runtime with MRO path.
	rt := core.NewRuntime("local", mroPath, core.GetVersion(), false)

	count := 0
	if opts["--all"].(bool) {
		// Compile all MRO files in MRO path.
		num, err := rt.CompileAll()
		core.DieIf(err)
		count += num
	} else {
		// Compile just the specified MRO files.
		for _, fname := range opts["<file.mro>"].([]string) {
			_, err := rt.Compile(fname)
			core.DieIf(err)
			count++
		}
	}
	fmt.Println("Successfully compiled", count, "mro files.")
}
