package main

import (
	"martian/core"
	"martian/util"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/martian-lang/docopt.go"
)

func main() {
	util.SetupSignalHandlers()
	doc := `Kepler.

Usage:
    keplerd
    keplerd -h | --help | --version

Options:
    -h --help       Show this message.
    --version       Show version.`
	martianVersion := util.GetVersion()
	docopt.Parse(doc, nil, true, martianVersion, false)

	env := util.EnvRequire([][]string{
		{"KEPLER_PORT", ">2000"},
		{"KEPLER_LOG_PATH", "path/to/kepler/logs"},
		{"KEPLER_DB_PATH", "path/to/db"},
		{"KEPLER_PIPESTANCES_PATH", "path/to/pipestances"},
	}, true)

	util.LogTee(path.Join(env["KEPLER_LOG_PATH"], time.Now().Format("20060102150405")+".log"))

	uiport := env["KEPLER_PORT"]
	dbPath := env["KEPLER_DB_PATH"]
	pipestancesPaths := strings.Split(env["KEPLER_PIPESTANCES_PATH"], ":")

	// Compute MRO path.
	cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	mroPaths := util.ParseMroPath(cwd)
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPaths = util.ParseMroPath(value)
	}
	mroVersion, _ := util.GetMroVersion(mroPaths)

	rt := core.NewRuntime("local", "disable", "disable", martianVersion)
	db := NewDatabaseManager("sqlite3", dbPath)
	pman := NewPipestanceManager(pipestancesPaths, mroPaths, mroVersion, db, rt)

	// Run web server.
	go runWebServer(uiport, martianVersion, db)

	// Start pipestance manager daemon.
	pman.Start()

	// Let daemons take over.
	done := make(chan bool)
	<-done
}
