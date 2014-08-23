//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Marsoc daemon.
//
package main

import (
	"fmt"
	"margo/core"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/dustin/go-humanize"
)

func sendNotificationMail(users []string, mailer *core.Mailer, notices []*PipestanceNotification) {
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
		result := fmt.Sprintf("%s of %s is %s (http://%s)", notice.Pname, notice.Psid, strings.ToUpper(notice.State), url)
		results = append(results, result)
		vdrsize += notice.Vdrsize
		if notice.State == "failed" {
			worstState = notice.State
		}
	}

	// Compose the email.
	body := ""
	if worstState == "complete" {
		body = fmt.Sprintf("Hey Preppie,\n\nI totally nailed all your analysis!\n\n%s\n\nBtw I also saved you %s with VDR. Show me love!\n\nLena might take up to an hour to show your results.", strings.Join(results, "\n"), humanize.Bytes(vdrsize))
	} else {
		body = fmt.Sprintf("Hey Preppie,\n\nSome of your analysis failed!\n\n%s\n\nDon't feel bad, you'll get 'em next time!", strings.Join(results, "\n"))
	}
	subj := fmt.Sprintf("Analysis runs %s! (%s)", worstState, strings.Join(psids, ", "))
	mailer.Sendmail(users, subj, body)
}

func runEmailNotifier(pman *PipestanceManager, lena *Lena, mailer *core.Mailer) {
	for {
		// Copy and clear the notifyQueue from PipestanceManager to avoid races.
		notifyQueue := pman.CopyAndClearNotifyQueue()

		// Build a table of users to lists of notifications.
		// Also, collect all the notices that don't have a user associated.
		userTable := map[string][]*PipestanceNotification{}
		userlessNotices := []*PipestanceNotification{}
		for _, notice := range notifyQueue {
			// Get the sample with the psid in the notice.
			sample, ok := lena.getSampleWithId(notice.Psid)

			// If no sample, add to the userless table.
			if !ok {
				userlessNotices = append(userlessNotices, notice)
				continue
			}

			// Otherwise, build a list of notices for each user.
			nlist, ok := userTable[sample.User.Username]
			if ok {
				userTable[sample.User.Username] = append(nlist, notice)
			} else {
				userTable[sample.User.Username] = []*PipestanceNotification{notice}
			}
		}

		// Send emails to all users associated with samples.
		for user, notices := range userTable {
			sendNotificationMail([]string{user + "@10xtechnologies.com"}, mailer, notices)
		}

		// Send userless notices to the admins.
		if len(userlessNotices) > 0 {
			sendNotificationMail([]string{}, mailer, userlessNotices)
		}

		// Wait a bit.
		time.Sleep(time.Minute * time.Duration(5))
	}
}

func main() {
	runtime.GOMAXPROCS(2)
	core.LogInfo("*", "MARSOC")
	core.LogInfo("cmdline", strings.Join(os.Args, " "))

	//=========================================================================
	// Commandline argument and environment variables.
	//=========================================================================
	// Parse commandline.
	doc :=
		`Usage: 
    marsoc 
    marsoc -h | --help | --version`
	opts, _ := docopt.Parse(doc, nil, true, "marsoc", false)
	_ = opts

	// Required Mario environment variables.
	env := core.EnvRequire([][]string{
		{"MARSOC_PORT", ">2000"},
		{"MARSOC_INSTANCE_NAME", "displayed_in_ui"},
		{"MARSOC_JOBMODE", "local|sge"},
		{"MARSOC_SEQUENCERS", "miseq001;hiseq001"},
		{"MARSOC_SEQRUNS_PATH", "path/to/sequencers"},
		{"MARSOC_CACHE_PATH", "path/to/marsoc/cache"},
		{"MARSOC_PIPELINES_PATH", "path/to/pipelines"},
		{"MARSOC_PIPESTANCES_PATH", "path/to/pipestances"},
		{"MARSOC_NOTIFY_EMAIL", "email@address.com"},
	}, true)

	// Required job mode and SGE environment variables.
	jobMode := env["MARSOC_JOBMODE"]
	if jobMode == "sge" {
		core.EnvRequire([][]string{
			{"SGE_ROOT", "path/to/sge/root"},
			{"SGE_CLUSTER_NAME", "SGE cluster name"},
			{"SGE_CELL", "usually 'default'"},
		}, true)
	}

	// Do not log the value of these environment variables.
	envPrivate := core.EnvRequire([][]string{
		{"LENA_DOWNLOAD_URL", "url"},
		{"LENA_AUTH_TOKEN", "token"},
		{"MARSOC_SMTP_USER", "username"},
		{"MARSOC_SMTP_PASS", "password"},
	}, false)

	// Prepare configuration variables.
	uiport := env["MARSOC_PORT"]
	notifyEmail := env["MARSOC_NOTIFY_EMAIL"]
	instanceName := env["MARSOC_INSTANCE_NAME"]
	pipelinesPath := env["MARSOC_PIPELINES_PATH"]
	cachePath := env["MARSOC_CACHE_PATH"]
	seqrunsPath := env["MARSOC_SEQRUNS_PATH"]
	pipestancesPath := env["MARSOC_PIPESTANCES_PATH"]
	seqcerNames := strings.Split(env["MARSOC_SEQUENCERS"], ";")
	lenaAuthToken := envPrivate["LENA_AUTH_TOKEN"]
	lenaDownloadUrl := envPrivate["LENA_DOWNLOAD_URL"]
	smtpUser := envPrivate["MARSOC_SMTP_USER"]
	smtpPass := envPrivate["MARSOC_SMTP_PASS"]
	stepSecs := 5

	//=========================================================================
	// Setup Mailer.
	//=========================================================================
	mailer := core.NewMailer(instanceName, smtpUser, smtpPass, notifyEmail, instanceName != "MARSOC")

	//=========================================================================
	// Setup Mario Runtime with pipelines path.
	//=========================================================================
	rt := core.NewRuntime(jobMode, pipelinesPath)
	_, err := rt.CompileAll()
	core.DieIf(err)
	core.LogInfo("configs", "CODE_VERSION = %s", rt.CodeVersion)

	//=========================================================================
	// Setup SequencerPool, add sequencers, load cache, start inventory loop.
	//=========================================================================
	pool := NewSequencerPool(seqrunsPath, cachePath, mailer)
	for _, seqcerName := range seqcerNames {
		pool.add(seqcerName)
	}
	pool.loadCache()
	pool.goInventoryLoop()

	//=========================================================================
	// Setup PipestanceManager, load cache, start runlist loop.
	//=========================================================================
	pman := NewPipestanceManager(rt, pipestancesPath, cachePath, stepSecs, mailer)
	pman.loadCache()
	pman.inventoryPipestances()
	pman.goRunListLoop()

	//=========================================================================
	// Setup Lena and load cache.
	//=========================================================================
	lena := NewLena(lenaDownloadUrl, lenaAuthToken, cachePath, mailer)
	lena.loadDatabase()
	lena.goDownloadLoop()

	//=========================================================================
	// Setup argshim.
	//=========================================================================
	argshim := NewArgShim(pipelinesPath)

	//=========================================================================
	// Start web server.
	//=========================================================================
	go runWebServer(uiport, instanceName, rt, pool, pman, lena, argshim)

	//=========================================================================
	// Start email notifier.
	//=========================================================================
	go runEmailNotifier(pman, lena, mailer)

	// Let daemons take over.
	done := make(chan bool)
	<-done
}
