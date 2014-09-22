//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Mario miscellaneous utilities.
//
package core

import (
	"fmt"
	"github.com/10XDev/osext"
	"os"
	"path"
	"time"
)

func mkdir(p string) {
	err := os.Mkdir(p, 0755)
	if err != nil {
		panic(err.Error())
	}
}

func RelPath(p string) string {
	folder, _ := osext.ExecutableFolder()
	return path.Join(folder, p)
}

func idemMkdir(p string) {
	os.Mkdir(p, 0755)
}

func cartesianProduct(valueSets []interface{}) []interface{} {
	perms := []interface{}{[]interface{}{}}
	for _, valueSet := range valueSets {
		newPerms := []interface{}{}
		for _, perm := range perms {
			for _, value := range valueSet.([]interface{}) {
				perm := perm.([]interface{})
				newPerm := make([]interface{}, len(perm))
				copy(newPerm, perm)
				newPerm = append(newPerm, value)
				newPerms = append(newPerms, newPerm)
			}
		}
		perms = newPerms
	}
	return perms
}

const TIMEFMT = "2006-01-02 15:04:05"

func Timestamp() string {
	return time.Now().Format(TIMEFMT)
}

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
			LogInfo("environ", "%s = %s", req[0], val)
		}
	}
	return e
}
