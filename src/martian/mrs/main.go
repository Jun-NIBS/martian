//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian stage runner.
//
package main

import (
	"io/ioutil"
	"martian/core"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docopt/docopt.go"
)

func main() {
	core.SetupSignalHandlers()

	//=========================================================================
	// Commandline argument and environment variables.
	//=========================================================================
	// Parse commandline.
	doc := `Martian Stage Runner.

Usage:
    mrs <call.mro> <stagestance_name> [options]
    mrs -h | --help | --version

Options:
    --jobmode=<name>         Run jobs on custom or local job manager.
                               Valid job managers are local, sge, lsf or .template file
                               Defaults to local.
    --profile=<name>         Enables stage performance profiling.
                               Valid options are cpu, mem, line and disable.
                               Defaults to disable.
    --stackvars              Print local variables in stage code stack trace.
    --localcores=<num>       Set max cores the pipeline may request at one time.
                               (Only applies in local jobmode)
    --localmem=<num>         Set max GB the pipeline may request at one time.
                               (Only applies in local jobmode)
    --mempercore=<num>       Set max GB each job may use at one time.
                               (Only applies in non-local jobmodes)
    --maxjobs=<num>          Set maximum number of concurrent jobs at one time.
                               (Only applies in non-local jobmodes)
    --monitor                Kill jobs when using more than requested memory resources.
    --debug                  Enable debug logging for local job manager.
    -h --help                Show this message.
    --version                Show version.`
	martianVersion := core.GetVersion()
	opts, _ := docopt.Parse(doc, nil, true, martianVersion, false)
	core.Println("Martian Single-Stage Runtime - %s", martianVersion)
	core.LogInfo("cmdline", strings.Join(os.Args, " "))

	martianFlags := ""
	if martianFlags = os.Getenv("MROFLAGS"); len(martianFlags) > 0 {
		martianOptions := strings.Split(martianFlags, " ")
		core.ParseMroFlags(opts, doc, martianOptions, []string{"call.mro", "stagestance"})
		core.LogInfo("environ", "MROFLAGS=%s", martianFlags)
	}

	// Requested cores and memory.
	reqCores := -1
	if value := opts["--localcores"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			reqCores = value
			core.LogInfo("options", "--localcores=%d", reqCores)
		}
	}
	reqMem := -1
	if value := opts["--localmem"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			reqMem = value
			core.LogInfo("options", "--localmem=%d", reqMem)
		}
	}
	reqMemPerCore := -1
	if value := opts["--mempercore"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			reqMemPerCore = value
			core.LogInfo("options", "--mempercore=%d", reqMemPerCore)
		}
	}

	// Max parallel jobs.
	maxJobs := -1
	if value := opts["--maxjobs"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			maxJobs = value
			core.LogInfo("options", "--maxjobs=%d", maxJobs)
		}
	}

	// Compute MRO path.
	cwd, _ := filepath.Abs(path.Dir(os.Args[0]))
	mroPath := cwd
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPath = value
	}
	mroVersion := core.GetMroVersion(mroPath)
	core.LogInfo("environ", "MROPATH=%s", mroPath)
	core.LogInfo("version", "MRO Version=%s", mroVersion)

	// Compute job manager.
	jobMode := "local"
	if value := opts["--jobmode"]; value != nil {
		jobMode = value.(string)
	}
	core.LogInfo("options", "--jobmode=%s", jobMode)

	// Compute profiling mode.
	profileMode := "disable"
	if value := opts["--profile"]; value != nil {
		profileMode = value.(string)
	}
	core.LogInfo("options", "--profile=%s", profileMode)
	core.VerifyProfileMode(profileMode)

	// Compute stackvars flag.
	stackVars := opts["--stackvars"].(bool)
	core.LogInfo("options", "--stackvars=%v", stackVars)

	// Setup invocation-specific values.
	invocationPath := opts["<call.mro>"].(string)
	ssid := opts["<stagestance_name>"].(string)
	stagestancePath := path.Join(cwd, ssid)
	stepSecs := 1
	vdrMode := "disable"
	zip := false
	skipPreflight := false
	enableMonitor := opts["--monitor"].(bool)
	debug := opts["--debug"].(bool)
	envs := map[string]string{}

	// Validate psid.
	core.DieIf(core.ValidateID(ssid))

	//=========================================================================
	// Configure Martian runtime.
	//=========================================================================
	rt := core.NewRuntimeWithCores(jobMode, vdrMode, profileMode, martianVersion,
		reqCores, reqMem, reqMemPerCore, maxJobs, stackVars, zip, skipPreflight,
		enableMonitor, debug, false)
	rt.MroCache.CacheMros(mroPath)

	// Invoke stagestance.
	data, err := ioutil.ReadFile(invocationPath)
	core.DieIf(err)
	stagestance, err := rt.InvokeStage(string(data), invocationPath, ssid,
		stagestancePath, mroPath, mroVersion, envs)
	core.DieIf(err)

	// Start writing (including cached entries) to log file.
	core.LogTee(path.Join(stagestancePath, "_log"))

	//=========================================================================
	// Start run loop.
	//=========================================================================
	go func() {
		// Initialize state from metadata
		stagestance.LoadMetadata()
		for {
			// Refresh state on the node.
			stagestance.RefreshState()

			// Check for completion states.
			state := stagestance.GetState()
			if state == "complete" {
				stagestance.PostProcess()
				core.Println("Stage completed, exiting.")
				os.Exit(0)
			}
			if state == "failed" {
				if _, errpath, log, kind, err := stagestance.GetFatalError(); kind == "assert" {
					core.Println("\n%s\n", log)
				} else {
					core.Println("\nStage failed, errors written to:\n%s\n\n%s\n",
						errpath, err)
					core.Println("Stage failed, exiting.")
				}
				os.Exit(1)
			}

			// Check job heartbeats.
			stagestance.CheckHeartbeats()

			// Step the node.
			stagestance.Step()

			// Wait for a bit.
			time.Sleep(time.Second * time.Duration(stepSecs))
		}
	}()

	// Let the daemons take over.
	done := make(chan bool)
	<-done
}
