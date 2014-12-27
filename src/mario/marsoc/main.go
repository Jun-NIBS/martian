//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Marsoc daemon.
//
package main

import (
	"fmt"
	"mario/core"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt.go"
	"github.com/dustin/go-humanize"
)

func sendNotificationMail(users []string, mailer *Mailer, notices []*PipestanceNotification) {
	// Build summary of the notices.
	results := []string{}
	worstState := "complete"
	psids := []string{}
	var vdrsize uint64
	for _, notice := range notices {
		psids = append(psids, notice.Psid)
		var url string
		if notice.State == "complete" {
			url = fmt.Sprintf("lena/seq_results/sample%strim10/", notice.Psid)
		} else {
			url = fmt.Sprintf("%s/pipestance/%s/%s/%s", mailer.InstanceName, notice.Container, notice.Pname, notice.Psid)
		}
		result := fmt.Sprintf("%s of %s/%s is %s (http://%s)", notice.Pname, notice.Container, notice.Psid, strings.ToUpper(notice.State), url)
		results = append(results, result)
		vdrsize += notice.Vdrsize
		if notice.State == "failed" {
			worstState = notice.State
		}
	}

	// Compose the email.
	body := ""
	if worstState == "complete" {
		body = fmt.Sprintf("Hey Preppie,\n\nI totally nailed all your analysis!\n\n%s\n\nLena might take up to an hour to show your results.\n\nBtw I also saved you %s with VDR. Show me love!", strings.Join(results, "\n"), humanize.Bytes(vdrsize))
	} else {
		body = fmt.Sprintf("Hey Preppie,\n\nSome of your analysis failed!\n\n%s\n\nDon't feel bad, you'll get 'em next time!", strings.Join(results, "\n"))
	}
	subj := fmt.Sprintf("Analysis runs %s! (%s)", worstState, strings.Join(psids, ", "))
	mailer.Sendmail(users, subj, body)
}

func emailNotifierLoop(pman *PipestanceManager, lena *Lena, mailer *Mailer) {
	go func() {
		for {
			// Copy and clear the notifyQueue from PipestanceManager to avoid races.
			notifyQueue := pman.CopyAndClearNotifyQueue()

			// Build a table of users to lists of notifications.
			// Also, collect all the notices that don't have a user associated.
			emailTable := map[string][]*PipestanceNotification{}
			userlessNotices := []*PipestanceNotification{}
			for _, notice := range notifyQueue {
				// Get the sample with the psid in the notice.
				sample := lena.getSampleWithId(notice.Psid)

				// If no sample, add to the userless table.
				if sample == nil {
					userlessNotices = append(userlessNotices, notice)
					continue
				}

				// Otherwise, build a list of notices for each user.
				nlist, ok := emailTable[sample.User.Email]
				if ok {
					emailTable[sample.User.Email] = append(nlist, notice)
				} else {
					emailTable[sample.User.Email] = []*PipestanceNotification{notice}
				}
			}

			// Send emails to all users associated with samples.
			for email, notices := range emailTable {
				sendNotificationMail([]string{email}, mailer, notices)
			}

			// Send userless notices to the admins.
			if len(userlessNotices) > 0 {
				sendNotificationMail([]string{}, mailer, userlessNotices)
			}

			// Wait a bit.
			time.Sleep(time.Minute * time.Duration(30))
		}
	}()
}

func processRunLoop(pool *SequencerPool, pman *PipestanceManager, argshim *ArgShim, rt *core.Runtime, mailer *Mailer) {
	go func() {
		for {
			runQueue := pool.CopyAndClearRunQueue()

			fcids := []string{}
			for _, runNotification := range runQueue {
				run := runNotification.run
				fcids = append(fcids, run.Fcid)
				pman.Invoke(run.Fcid, "BCL_PROCESSOR_PD", run.Fcid, argshim.buildCallSourceForRun(rt, run))
			}

			// If there are new runs completed, send email.
			if len(fcids) > 0 {
				mailer.Sendmail(
					[]string{},
					fmt.Sprintf("Sequencing runs complete! (%s)", strings.Join(fcids, ", ")),
					fmt.Sprintf("Hey Preppie,\n\nI noticed sequencing runs %s are done.\n\nI started this BCL PROCESSOR party at http://%s/.",
						strings.Join(fcids, ", "), mailer.InstanceName),
				)
			}

			// Wait a bit.
			time.Sleep(time.Minute * time.Duration(30))
		}
	}()
}

func main() {
	core.LogInfo("*", "MARSOC")
	core.LogInfo("cmdline", strings.Join(os.Args, " "))

	//=========================================================================
	// Commandline argument and environment variables.
	//=========================================================================
	// Parse commandline.
	doc := `MARSOC: Mario SeqOps Command

Usage: 
    marsoc [--debug]
    marsoc -h | --help | --version

Options:
    --debug    Enable debug printing for argshim.
    -h --help  Show this message.
    --version  Show version.`
	marioVersion := core.GetVersion()
	opts, _ := docopt.Parse(doc, nil, true, marioVersion, false)
	_ = opts
	core.LogInfo("*", "MARSOC")
	core.LogInfo("version", marioVersion)
	core.LogInfo("cmdline", strings.Join(os.Args, " "))

	// Required Mario environment variables.
	env := core.EnvRequire([][]string{
		{"MARSOC_PORT", ">2000"},
		{"MARSOC_INSTANCE_NAME", "displayed_in_ui"},
		{"MARSOC_SEQUENCERS", "miseq001;hiseq001"},
		{"MARSOC_SEQUENCERS_PATH", "path/to/sequencers"},
		{"MARSOC_CACHE_PATH", "path/to/marsoc/cache"},
		{"MARSOC_ARGSHIM_PATH", "path/to/argshim"},
		{"MARSOC_MROPATH", "path/to/mros"},
		{"MARSOC_PIPESTANCES_PATH", "path/to/pipestances"},
		{"MARSOC_SCRATCH_PATH", "path/to/scratch/pipestances"},
		{"MARSOC_EMAIL_HOST", "smtp.server.local"},
		{"MARSOC_EMAIL_SENDER", "email@address.com"},
		{"MARSOC_EMAIL_RECIPIENT", "email@address.com"},
		{"LENA_DOWNLOAD_URL", "url"},
	}, true)

	// Verify SGE job manager configuration
	core.VerifyJobManager("sge")

	// Do not log the value of these environment variables.
	envPrivate := core.EnvRequire([][]string{
		{"LENA_AUTH_TOKEN", "token"},
	}, false)

	// Prepare configuration variables.
	uiport := env["MARSOC_PORT"]
	instanceName := env["MARSOC_INSTANCE_NAME"]
	mroPath := env["MARSOC_MROPATH"]
	argshimPath := env["MARSOC_ARGSHIM_PATH"]
	cachePath := env["MARSOC_CACHE_PATH"]
	seqrunsPath := env["MARSOC_SEQUENCERS_PATH"]
	pipestancesPaths := strings.Split(env["MARSOC_PIPESTANCES_PATH"], ":")
	scratchPaths := strings.Split(env["MARSOC_SCRATCH_PATHS"], ":")
	seqcerNames := strings.Split(env["MARSOC_SEQUENCERS"], ";")
	lenaAuthToken := envPrivate["LENA_AUTH_TOKEN"]
	lenaDownloadUrl := env["LENA_DOWNLOAD_URL"]
	emailHost := env["MARSOC_EMAIL_HOST"]
	emailSender := env["MARSOC_EMAIL_SENDER"]
	emailRecipient := env["MARSOC_EMAIL_RECIPIENT"]
	stepSecs := 5
	mroVersion := core.GetGitTag(mroPath)
	debug := opts["--debug"].(bool)

	//=========================================================================
	// Setup Mario Runtime with pipelines path.
	//=========================================================================
	jobMode := "sge"
	profile := true
	locals := false
	checkSrcPath := true
	rt := core.NewRuntime(jobMode, mroPath, marioVersion, mroVersion, profile, locals, debug)
	if _, err := rt.CompileAll(checkSrcPath); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	core.LogInfo("version", "MRO_STAGES = %s", mroVersion)

	//=========================================================================
	// Setup Mailer.
	//=========================================================================
	mailer := NewMailer(instanceName, emailHost, emailSender, emailRecipient,
		instanceName != "MARSOC")

	//=========================================================================
	// Setup SequencerPool, add sequencers, and load seq run cache.
	//=========================================================================
	pool := NewSequencerPool(seqrunsPath, cachePath)
	for _, seqcerName := range seqcerNames {
		pool.add(seqcerName)
	}
	pool.loadCache()

	//=========================================================================
	// Setup PipestanceManager and load pipestance cache.
	//=========================================================================
	pman := NewPipestanceManager(rt, marioVersion, mroVersion, pipestancesPaths,
		scratchPaths, cachePath, stepSecs, mailer)
	pman.loadCache()
	pman.inventoryPipestances()

	//=========================================================================
	// Setup Lena and load cache.
	//=========================================================================
	lena := NewLena(lenaDownloadUrl, lenaAuthToken, cachePath, mailer)
	lena.loadDatabase()

	//=========================================================================
	// Setup argshim.
	//=========================================================================
	argshim := NewArgShim(argshimPath, debug)

	//=========================================================================
	// Start all daemon loops.
	//=========================================================================
	pool.goInventoryLoop()
	pman.goRunListLoop()
	lena.goDownloadLoop()
	emailNotifierLoop(pman, lena, mailer)
	processRunLoop(pool, pman, argshim, rt, mailer)

	//=========================================================================
	// Collect pipestance static info.
	//=========================================================================
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	user, err := user.Current()
	username := "unknown"
	if err == nil {
		username = user.Username
	}
	info := map[string]string{
		"hostname":   hostname,
		"username":   username,
		"cwd":        "",
		"binpath":    core.RelPath(os.Args[0]),
		"cmdline":    strings.Join(os.Args, " "),
		"pid":        strconv.Itoa(os.Getpid()),
		"version":    marioVersion,
		"pname":      "",
		"psid":       "",
		"jobmode":    jobMode,
		"maxcores":   strconv.Itoa(rt.JobManager.GetMaxCores()),
		"maxmemgb":   strconv.Itoa(rt.JobManager.GetMaxMemGB()),
		"invokepath": "",
		"invokesrc":  "",
		"MROPATH":    mroPath,
		"MRONODUMP":  "false",
		"MROPROFILE": fmt.Sprintf("%v", profile),
		"MROPORT":    uiport,
		"mroversion": mroVersion,
		"mrobranch":  core.GetGitBranch(mroPath),
	}

	//=========================================================================
	// Start web server.
	//=========================================================================
	runWebServer(uiport, instanceName, marioVersion, mroVersion, rt, pool, pman,
		lena, argshim, info)

	// Let daemons take over.
	done := make(chan bool)
	<-done
}
