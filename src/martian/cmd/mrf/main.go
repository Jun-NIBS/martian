//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian command-line formatter. Enforces the one true style.
//
package main

import (
	"fmt"
	"github.com/martian-lang/docopt.go"
	"io/ioutil"
	"martian/syntax"
	"martian/util"
	"os"
	"path"
	"path/filepath"
)

func main() {
	util.SetupSignalHandlers()
	// Command-line arguments.
	doc := `Martian Formatter.

Usage:
    mrf <file.mro>... [--rewrite]
    mrf --all
    mrf -h | --help | --version

Options:
    --rewrite     Rewrite the specified file(s) in place in addition to
                  printing reformatted source to stdout.
    --all         Rewrite all files in MROPATH.
    -h --help     Show this message.
    --version     Show version.`
	martianVersion := util.GetVersion()
	opts, _ := docopt.Parse(doc, nil, true, martianVersion, false)

	// Martian environment variables.
	cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	mroPaths := util.ParseMroPath(cwd)
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPaths = util.ParseMroPath(value)
	}

	if opts["--all"].(bool) {
		// Format all MRO files in MRO path.
		numFiles := 0
		for _, mroPath := range mroPaths {
			fnames, err := filepath.Glob(mroPath + "/*.mro")
			util.DieIf(err)
			for _, fname := range fnames {
				fsrc, err := syntax.FormatFile(fname)
				util.DieIf(err)
				ioutil.WriteFile(fname, []byte(fsrc), 0644)
			}
			numFiles += len(fnames)
		}
		fmt.Printf("Successfully reformatted %d files.\n", numFiles)
	} else {
		// Format just the specified MRO files.
		for _, fname := range opts["<file.mro>"].([]string) {
			fsrc, err := syntax.FormatFile(fname)
			util.DieIf(err)
			fmt.Print(fsrc)
			if opts["--rewrite"].(bool) {
				ioutil.WriteFile(fname, []byte(fsrc), 0644)
			}
		}
	}
}
