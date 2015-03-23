//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian command-line invocation generator.
//
package main

import (
	"encoding/json"
	"fmt"
	"github.com/docopt/docopt.go"
	"martian/core"
	"os"
	"path"
	"path/filepath"
)

func main() {
	core.SetupSignalHandlers()
	// Command-line arguments.
	doc := `Martian Invocation Generator.

Usage:
    mrg 
    mrg -h | --help | --version

Options:
    -h --help       Show this message.
    --version       Show version.`
	martianVersion := core.GetVersion()
	docopt.Parse(doc, nil, true, martianVersion, false)

	core.ENABLE_LOGGING = false

	// Martian environment variables.
	cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	mroPath := cwd
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPath = value
	}
	mroVersion := core.GetMroVersion(mroPath)

	// Setup runtime with MRO path.
	rt := core.NewRuntime("local", "disable", "disable", mroPath, martianVersion, mroVersion)

	// Read and parse JSON from stdin.
	dec := json.NewDecoder(os.Stdin)
	var input map[string]interface{}
	if err := dec.Decode(&input); err == nil {
		incpaths := []string{}
		if ilist, ok := input["incpaths"].([]interface{}); ok {
			for _, i := range ilist {
				if incpath, ok := i.(string); ok {
					incpaths = append(incpaths, incpath)
				}
			}
		}
		name, ok := input["call"].(string)
		if !ok {
			fmt.Println("No pipeline or stage specified.")
			os.Exit(1)
		}

		args, argsOk := input["args"].(map[string]interface{})
		sweepargs, sweepOk := input["sweepargs"].(map[string]interface{})
		if !argsOk && !sweepOk {
			fmt.Println("No args or sweepargs given.")
			os.Exit(1)
		}

		src, bldErr := rt.BuildCallSource(incpaths, name, args, sweepargs)

		if bldErr == nil {
			fmt.Print(src)
			os.Exit(0)
		} else {
			fmt.Println(bldErr)
			os.Exit(1)
		}
	}
	os.Exit(1)
}
