//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Margo
//
package core

import (
	"fmt"
	"os"
)

func EnvRequire(reqs [][]string, log bool) map[string]string {
	e := map[string]string{}
	for _, req := range reqs {
		val := os.Getenv(req[0])
		if len(val) == 0 {
			fmt.Println("Please set the following environment variables:\n")
			for _, req := range reqs {
				if len(os.Getenv(req[0])) == 0 {
					fmt.Println("export", req[0], "=", req[1])
				}
			}
			os.Exit(1)
		}
		e[req[0]] = val
		if log {
			fmt.Println("[ENV]", req[0], "=", val)
		}
	}
	return e
}
