package main

import (
	"martian/core"
	_ "os"
	"path"
	_ "path/filepath"
	_ "strings"
	"time"

	"github.com/docopt/docopt.go"
)

func main() {
	core.SetupSignalHandlers()
	doc := `Houston.

Usage:
    houston
    houston -h | --help | --version

Options:
    -h --help       Show this message.
    --version       Show version.`
	martianVersion := core.GetVersion()
	docopt.Parse(doc, nil, true, martianVersion, false)

	env := core.EnvRequire([][]string{
		{"HOUSTON_PORT", ">2000"},
		{"HOUSTON_BUCKET", "s3_bucket"},
		{"HOUSTON_LOG_PATH", "path/to/houston/logs"},
		{"HOUSTON_DOWNLOAD_PATH", "path/to/houston/downloads"},
		{"HOUSTON_STORAGE_PATH", "path/to/houston/storage"},
	}, true)

	core.LogTee(path.Join(env["HOUSTON_LOG_PATH"], time.Now().Format("20060102150405")+".log"))

	//uiport := env["HOUSTON_PORT"]
	bucket := env["HOUSTON_BUCKET"]
	dlPath := env["HOUSTON_DOWNLOAD_PATH"]
	stPath := env["HOUSTON_STORAGE_PATH"]

	dl := NewDownloadManager(bucket, dlPath, stPath)
	dl.StartDownloadLoop()
	//pipestancesPaths := strings.Split(env["HOUSTON_PIPESTANCES_PATH"], ":")

	// Compute MRO path.
	//cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	//mroPath := cwd
	//if value := os.Getenv("MROPATH"); len(value) > 0 {
	//	mroPath = value
	//}
	//mroVersion := core.GetMroVersion(mroPath)

	//rt := core.NewRuntime("local", "disable", "disable", martianVersion)
	//db := NewDatabaseManager("sqlite3", dbPath)
	//pman := NewPipestanceManager(pipestancesPaths, mroPath, mroVersion, db, rt)

	// Run web server.
	//go runWebServer(uiport, martianVersion, db)

	// Start pipestance manager daemon.
	//pman.Start()

	// Let daemons take over.
	done := make(chan bool)
	<-done
}
