//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Mario command-line formatter. Enforces the one true style.
//
package main

import (
	"fmt"
	"github.com/docopt/docopt-go"
	"io/ioutil"
	"mario/core"
	"os"
	"path"
	"path/filepath"
)

func main() {
	// Command-line arguments.
	doc := `Mario Formatter.

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
	opts, _ := docopt.Parse(doc, nil, true, core.GetVersion(), false)

	// Mario environment variables.
	cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	mroPath := cwd
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPath = value
	}

	if opts["--all"].(bool) {
		// Format all MRO files in MRO path.
		fnames, err := filepath.Glob(mroPath + "/*.mro")
		core.DieIf(err)
		for _, fname := range fnames {
			fmt.Printf("Reformatting %s...\n", fname)
			fsrc, err := core.FormatFile(fname)
			core.DieIf(err)
			ioutil.WriteFile(fname, []byte(fsrc), 0600)
		}
	} else {
		// Format just the specified MRO files.
		for _, fname := range opts["<file.mro>"].([]string) {
			fsrc, err := core.FormatFile(fname)
			core.DieIf(err)
			fmt.Print(fsrc)
			if opts["--rewrite"].(bool) {
				ioutil.WriteFile(fname, []byte(fsrc), 0600)
			}
		}
	}
}
