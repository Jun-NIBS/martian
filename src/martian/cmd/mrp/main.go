//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//

// Martian pipeline runner.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"martian/api"
	"martian/core"
	"martian/util"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/martian-lang/docopt.go"
)

// We need to be able to recreate pipestances and share the new pipestance
// object between the runloop and the UI.
type pipestanceHolder struct {
	pipestance *core.Pipestance
	factory    core.PipestanceFactory
	lock       sync.Mutex
}

func (self *pipestanceHolder) getPipestance() *core.Pipestance {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.pipestance
}

func (self *pipestanceHolder) setPipestance(newPipe *core.Pipestance) {
	self.pipestance = newPipe
}

func (self *pipestanceHolder) reset() error {
	self.lock.Lock()
	defer self.lock.Unlock()
	ps, err := self.factory.ReattachToPipestance()
	if err == nil {
		err = ps.Reset()
		if err != nil {
			ps.Unlock()
			return err
		}
		ps.LoadMetadata()
		self.setPipestance(ps)
	}
	return err
}

//=============================================================================
// Pipestance runner.
//=============================================================================
func runLoop(pipestanceBox *pipestanceHolder, stepSecs int, vdrMode string,
	noExit bool, enableUI bool, retries int) {
	showedFailed := false
	showedComplete := false
	WAIT_SECS := 6
	pipestanceBox.getPipestance().LoadMetadata()

	for {
		pipestance := pipestanceBox.getPipestance()
		pipestance.RefreshState()

		// Check for completion states.
		state := pipestance.GetState()
		if state == "complete" {
			if vdrMode == "disable" {
				util.LogInfo("runtime", "VDR disabled. No files killed.")
			} else {
				killReport := pipestance.VDRKill()
				util.LogInfo("runtime", "VDR killed %d files, %s.",
					killReport.Count, humanize.Bytes(killReport.Size))
			}
			pipestance.PostProcess()
			pipestance.Unlock()
			if !showedComplete {
				pipestance.OnFinishHook()
				showedComplete = true
			}
			if noExit {
				util.Println("Pipestance completed successfully, staying alive because --noexit given.\n")
				break
			} else {
				if enableUI {
					// Give time for web ui client to get last update.
					util.Println("Waiting %d seconds for UI to do final refresh.", WAIT_SECS)
					time.Sleep(time.Second * time.Duration(WAIT_SECS))
					pipestance.ClearUiPort()
				}
				util.Println("Pipestance completed successfully!\n")
				os.Exit(0)
			}
		} else if state == "failed" {
			canRetry := false
			var transient_log string
			if retries > 0 {
				canRetry, transient_log = pipestance.IsErrorTransient()
			}
			if canRetry {
				pipestance.Unlock()
				retries--
				if transient_log != "" {
					util.LogInfo("runtime", "Transient error detected.  Log content:\n\n%s\n", transient_log)
				}
				util.LogInfo("runtime", "Attempting retry.")
				if err := pipestanceBox.reset(); err != nil {
					util.LogInfo("runtime", "Retry failed:\n%v\n", err)
					// Let the next loop around actually handle the failure.
				}
			} else {
				pipestance.Unlock()
				if !showedFailed {
					pipestance.OnFinishHook()
					if _, preflight, _, log, kind, errPaths := pipestance.GetFatalError(); kind == "assert" {
						// Print preflight check failures.
						util.Println("\n[%s] %s\n", "error", log)
						if preflight {
							os.Exit(2)
						} else {
							os.Exit(1)
						}
					} else if len(errPaths) > 0 {
						// Build relative path to _errors file
						errPath, _ := filepath.Rel(filepath.Dir(pipestance.GetPath()), errPaths[0])

						if log != "" {
							util.Println("\n[%s] Pipestance failed. Error log at:\n%s\n\nLog message:\n%s\n",
								"error", errPath, log)
						} else {
							// Print path to _errors metadata file in failed stage.
							util.Println("\n[%s] Pipestance failed. Please see log at:\n%s\n", "error", errPath)
						}
					}
				}
				if noExit {
					// If pipestance failed but we're staying alive, only print this once
					// as long as we stay failed.
					if !showedFailed {
						showedFailed = true
						util.Println("Pipestance failed, staying alive because --noexit given.\n")
					}
				} else {
					if enableUI {
						// Give time for web ui client to get last update.
						util.Println("Waiting %d seconds for UI to do final refresh.", WAIT_SECS)
						time.Sleep(time.Second * time.Duration(WAIT_SECS))
						util.Println("Pipestance failed. Use --noexit option to keep UI running after failure.\n")
						pipestance.ClearUiPort()
					}
					os.Exit(1)
				}
			}
		} else {
			// If we went from failed to something else, allow the failure message to
			// be shown once if we fail again.
			showedFailed = false

			// Check job heartbeats.
			pipestance.CheckHeartbeats()

			// Step all nodes.
			pipestance.StepNodes()
		}

		// Wait for a bit.
		time.Sleep(time.Second * time.Duration(stepSecs))
	}
}

// List of environment variables which might be useful in debugging.
var loggedEnvs = map[string]bool{
	"COMMD_PORT":   true,
	"CWD":          true,
	"ENVIRONMENT":  true, // SGE
	"EXE":          true,
	"HOME":         true,
	"HOST":         true,
	"HOSTNAME":     true,
	"HOSTTYPE":     true, // LSF
	"HYDRA_ROOT":   true,
	"LANG":         true,
	"LIBRARY_PATH": true,
	"LOGNAME":      true,
	"NHOSTS":       true, // SGE
	"NQUEUES":      true, // SGE
	"NSLOTS":       true, // SGE
	"PATH":         true,
	"PID":          true,
	"PWD":          true,
	"SHELL":        true,
	"SHLVL":        true,
	"SPOOLDIR":     true, // LSF
	"TERM":         true,
	"TMPDIR":       true,
	"USER":         true,
	"WAFDIR":       true,
	"_":            true,
}

// List of environment variable prefixes which might be useful in debugging.
// These are accepted for variables of the form "KEY_*"
var loggedEnvPrefixes = map[string]bool{
	"BASH":    true,
	"CONDA":   true,
	"DYLD":    true, // Linker
	"EC2":     true,
	"EGO":     true, // LSF
	"JAVA":    true,
	"JOB":     true, // SGE
	"LC":      true,
	"LD":      true, // Linker
	"LS":      true, // LSF
	"LSB":     true, // LSF
	"LSF":     true, // LSF
	"MYSYS2":  true, // Anaconda
	"PBS":     true, // PBS
	"PD":      true,
	"SBATCH":  true, // Slurm
	"SELINUX": true, // Linux
	"SGE":     true,
	"SLURM":   true,
	"SSH":     true,
	"TENX":    true,
	"XDG":     true,
}

// Returns true if the environment variable should be logged.
func logEnv(env string) bool {
	if loggedEnvs[env] {
		return true
	}
	// Various important PYTHON environment variables don't have a _ separator.
	if strings.HasPrefix(env, "PYTHON") {
		return true
	}
	if idx := strings.Index(env, "_"); idx >= 0 {
		return loggedEnvPrefixes[env[:idx]]
	} else {
		return loggedEnvPrefixes[env]
	}
}

func main() {
	util.SetupSignalHandlers()

	//=========================================================================
	// Commandline argument and environment variables.
	//=========================================================================
	// Parse commandline.
	doc := `Martian Pipeline Runner.

Usage:
    mrp <call.mro> <pipestance_name> [options]
    mrp -h | --help | --version

Options:
    --jobmode=MODE      Job manager to use. Valid options:
                            local (default), sge, lsf, or a .template file
    --localcores=NUM    Set max cores the pipeline may request at one time.
                            Only applies when --jobmode=local.
    --localmem=NUM      Set max GB the pipeline may request at one time.
                            Only applies when --jobmode=local.
    --mempercore=NUM    Specify min GB per core on your cluster.
                            Only applies in cluster jobmodes.
    --maxjobs=NUM       Set max jobs submitted to cluster at one time.
                            Only applies in cluster jobmodes.
    --jobinterval=NUM   Set delay between submitting jobs to cluster, in ms.
                            Only applies in cluster jobmodes.
    --limit-loadavg     Avoid scheduling jobs when the system loadavg is high.
                            Only applies when --jobmode=local.

    --vdrmode=MODE      Enables Volatile Data Removal. Valid options:
                            post (default), rolling, or disable

    --nopreflight       Skips preflight stages.
    --uiport=NUM        Serve UI at http://<hostname>:NUM
    --disable-ui        Do not serve the UI.
    --disable-auth      Do not require authentication for reading the web UI.
    --require-auth      Always require authentication (this is the default
                        if --uiport is not set).
    --auth-key=KEY      Set the authentication key required for accessing the
                        web UI.
    --noexit            Keep UI running after pipestance completes or fails.
    --onfinish=EXEC     Run this when pipeline finishes, success or fail.
    --zip               Zip metadata files after pipestance completes.
    --tags=TAGS         Tag pipestance with comma-separated key:value pairs.

    --profile=MODE      Enables stage performance profiling. Valid options:
                            disable (default), cpu, mem, or line
    --stackvars         Print local variables in stage code stack trace.
    --monitor           Kill jobs that exceed requested memory resources.
    --inspect           Inspect pipestance without resetting failed stages.
    --debug             Enable debug logging for local job manager.
    --stest             Substitute real stages with stress-testing stage.
    --autoretry=NUM     Automatically retry failed runs up to NUM times.
    --overrides=JSON    JSON file supplying custom run conditions per stage.

    -h --help           Show this message.
    --version           Show version.`
	martianVersion := util.GetVersion()
	opts, _ := docopt.Parse(doc, nil, true, martianVersion, false)
	util.Println("Martian Runtime - %s", martianVersion)
	util.LogInfo("build  ", "Built with Go version %s", runtime.Version())
	util.LogInfo("cmdline", strings.Join(os.Args, " "))
	util.LogInfo("pid    ", strconv.Itoa(os.Getpid()))

	for _, env := range os.Environ() {
		pair := strings.Split(env, "=")
		if len(pair) == 2 && logEnv(pair[0]) {
			util.LogInfo("environ", env)
		}
	}

	martianFlags := ""
	if martianFlags = os.Getenv("MROFLAGS"); len(martianFlags) > 0 {
		martianOptions := strings.Split(martianFlags, " ")
		util.ParseMroFlags(opts, doc, martianOptions, []string{"call.mro", "pipestance"})
		util.LogInfo("environ", "MROFLAGS=%s", martianFlags)
	}

	// Requested cores and memory.
	reqCores := -1
	if value := opts["--localcores"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			reqCores = value
			util.LogInfo("options", "--localcores=%d", reqCores)
		} else {
			util.PrintError(err, "options",
				"Could not parse --localcores value \"%s\"", opts["--localcores"].(string))
			os.Exit(1)
		}
	}
	reqMem := -1
	if value := opts["--localmem"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			reqMem = value
			util.LogInfo("options", "--localmem=%d", reqMem)
		} else {
			util.PrintError(err, "options",
				"Could not parse --localmem value \"%s\"", opts["--localmem"].(string))
			os.Exit(1)
		}
	}
	reqMemPerCore := -1
	if value := opts["--mempercore"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			reqMemPerCore = value
			util.LogInfo("options", "--mempercore=%d", reqMemPerCore)
		} else {
			util.PrintError(err, "options",
				"Could not parse --mempercore value \"%s\"", opts["--mempercore"].(string))
			os.Exit(1)
		}
	}

	// Special to resources mappings
	jobResources := ""
	if value := os.Getenv("MRO_JOBRESOURCES"); len(value) > 0 {
		jobResources = value
		util.LogInfo("options", "MRO_JOBRESOURCES=%s", jobResources)
	}

	// Flag for full stage reset, default is chunk-granular
	fullStageReset := false
	if value := os.Getenv("MRO_FULLSTAGERESET"); len(value) > 0 {
		fullStageReset = true
		util.LogInfo("options", "MRO_FULLSTAGERESET=%v", fullStageReset)
	}

	// Compute MRO path.
	cwd, _ := os.Getwd()
	mro_dir, _ := filepath.Abs(path.Dir(os.Args[1]))
	mroPaths := util.ParseMroPath(mro_dir)
	if value := os.Getenv("MROPATH"); len(value) > 0 {
		mroPaths = util.ParseMroPath(value)
	}
	mroVersion, _ := util.GetMroVersion(mroPaths)
	util.LogInfo("environ", "MROPATH=%s", util.FormatMroPath(mroPaths))
	util.LogInfo("version", "MRO Version=%s", mroVersion)

	// Compute job manager.
	jobMode := "local"
	if value := opts["--jobmode"]; value != nil {
		jobMode = value.(string)
	}
	util.LogInfo("options", "--jobmode=%s", jobMode)

	// Max parallel jobs.
	maxJobs := -1
	if jobMode != "local" {
		maxJobs = 64
	}
	if value := opts["--maxjobs"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			maxJobs = value
		} else {
			util.PrintError(err, "options", "Could not parse --maxjobs value \"%s\"", opts["--maxjobs"].(string))
			os.Exit(1)
		}
	}
	util.LogInfo("options", "--maxjobs=%d", maxJobs)

	// frequency (in milliseconds) that jobs will be sent to the queue
	// (this is a minimum bound, as it may take longer to emit jobs)
	jobFreqMillis := -1
	if jobMode != "local" {
		jobFreqMillis = 100
	}
	if value := opts["--jobinterval"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			jobFreqMillis = value
		} else {
			util.PrintError(err, "options", "Could not parse --jobinterval value \"%s\"", opts["--jobinterval"].(string))
			os.Exit(1)
		}
	}
	util.LogInfo("options", "--jobinterval=%d", jobFreqMillis)

	// Compute vdrMode.
	vdrMode := "post"
	if value := opts["--vdrmode"]; value != nil {
		vdrMode = value.(string)
	}
	util.LogInfo("options", "--vdrmode=%s", vdrMode)
	core.VerifyVDRMode(vdrMode)

	// Compute onfinish
	onfinish := ""
	if value := opts["--onfinish"]; value != nil {
		onfinish = value.(string)
		core.VerifyOnFinish(onfinish)
	}

	// Compute profiling mode.
	profileMode := core.DisableProfile
	if value := opts["--profile"]; value != nil {
		profileMode = core.ProfileMode(value.(string))
	}
	util.LogInfo("options", "--profile=%s", profileMode)
	core.VerifyProfileMode(profileMode)

	// Compute UI port.
	requireAuth := true
	uiport := ""
	if value := opts["--uiport"]; value != nil {
		uiport = value.(string)
		requireAuth = false
	} else if os.Getenv("MRVHOST") != "" {
		requireAuth = false
	}
	if len(uiport) > 0 {
		util.LogInfo("options", "--uiport=%s", uiport)
	}

	enableUI := (opts["--disable-ui"] == nil || !opts["--disable-ui"].(bool))
	if !enableUI {
		util.LogInfo("options", "--disable-ui")
	}
	if value := opts["--disable-auth"]; value != nil && value.(bool) {
		requireAuth = false
		util.LogInfo("options", "--disable-auth")
	}
	if value := opts["--require-auth"]; value != nil && value.(bool) {
		requireAuth = true
		util.LogInfo("options", "--require-auth")
	}
	var authKey string
	if value := opts["--authkey"]; value != nil {
		authKey = value.(string)
		util.LogInfo("options", "--authkey=%s", authKey)
	} else if enableUI {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			util.PrintError(err, "webserv", "Failed to generate an authentication key.")
			os.Exit(1)
		}
		authKey = base64.RawURLEncoding.EncodeToString(key)
	}

	// Parse tags.
	tags := []string{}
	if value := opts["--tags"]; value != nil {
		tags = util.ParseTagsOpt(value.(string))
	}
	for _, tag := range tags {
		util.LogInfo("options", "--tag='%s'", tag)
	}

	// Parse supplied overrides file.
	var overrides *core.PipestanceOverrides
	if v := opts["--overrides"]; v != nil {
		var err error
		overrides, err = core.ReadOverrides(v.(string))
		if err != nil {
			util.PrintError(err, "startup", "Failed to parse overrides file")
			os.Exit(1)

		}
	}

	// Compute stackVars flag.
	stackVars := opts["--stackvars"].(bool)
	util.LogInfo("options", "--stackvars=%v", stackVars)

	zip := opts["--zip"].(bool)
	util.LogInfo("options", "--zip=%v", zip)

	limitLoadavg := opts["--limit-loadavg"].(bool)
	util.LogInfo("options", "--limit-loadavg=%v", limitLoadavg)

	noExit := opts["--noexit"].(bool)
	util.LogInfo("options", "--noexit=%v", noExit)

	skipPreflight := opts["--nopreflight"].(bool)
	util.LogInfo("options", "--nopreflight=%v", skipPreflight)

	psid := opts["<pipestance_name>"].(string)
	invocationPath := opts["<call.mro>"].(string)
	pipestancePath := path.Join(cwd, psid)
	stepSecs := 3
	checkSrc := true
	readOnly := false
	enableMonitor := opts["--monitor"].(bool)
	inspect := opts["--inspect"].(bool)
	debug := opts["--debug"].(bool)
	stest := opts["--stest"].(bool)
	envs := map[string]string{}
	retries := core.DefaultRetries()
	if value := opts["--autoretry"]; value != nil {
		if value, err := strconv.Atoi(value.(string)); err == nil {
			retries = value
			util.LogInfo("options", "--autoretry=%d", retries)
		} else {
			util.PrintError(err, "options",
				"Could not parse --autoretry value \"%s\"", opts["--autoretry"].(string))
			os.Exit(1)
		}
	}
	if retries > 0 && fullStageReset {
		retries = 0
		util.Println(
			"\nWARNING: ignoring autoretry when MRO_FULLSTAGERESET is set.\n")
		util.LogInfo("options", "autoretry disabled due to MRO_FULLSTAGERESET.\n")
	}
	// Validate psid.
	util.DieIf(util.ValidateID(psid))

	// Get hostname and username.
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	user, err := user.Current()
	username := "unknown"
	if err == nil {
		username = user.Username
	}

	//=========================================================================
	// Configure Martian runtime.
	//=========================================================================
	rt := core.NewRuntimeWithCores(jobMode, vdrMode, profileMode, martianVersion,
		reqCores, reqMem, reqMemPerCore, maxJobs, jobFreqMillis, "", fullStageReset,
		stackVars, zip, skipPreflight, enableMonitor, debug, stest, onfinish,
		overrides, limitLoadavg)
	rt.MroCache.CacheMros(mroPaths)

	//=========================================================================
	// Invoke pipestance or Reattach if exists.
	//=========================================================================
	data, err := ioutil.ReadFile(invocationPath)
	util.DieIf(err)
	invocationSrc := string(data)
	executingPreflight := !skipPreflight

	factory := core.NewRuntimePipestanceFactory(rt,
		invocationSrc, invocationPath, psid, mroPaths, pipestancePath, mroVersion,
		envs, checkSrc, readOnly, tags)

	// Attempt to reattach to the pipestance.
	reattaching := false
	pipestance, err := factory.InvokePipeline()
	if err != nil {
		if _, ok := err.(*core.PipestanceExistsError); ok {
			if pipestance, err = factory.ReattachToPipestance(); err == nil {
				martianVersion, mroVersion, _ = pipestance.GetVersions()
				reattaching = true
			} else {
				util.DieIf(err)
			}
		} else {
			util.DieIf(err)
		}
	}
	pipestanceBox := pipestanceHolder{
		pipestance: pipestance,
		factory:    factory,
	}

	if !readOnly {
		// Start writing (including cached entries) to log file.
		util.LogTee(path.Join(pipestancePath, "_log"))
	}

	//=========================================================================
	// Collect pipestance static info.
	//=========================================================================
	info := &api.PipestanceInfo{
		Hostname:     hostname,
		Username:     username,
		Cwd:          cwd,
		Binpath:      util.RelPath(os.Args[0]),
		Cmdline:      strings.Join(os.Args, " "),
		Pid:          os.Getpid(),
		Start:        pipestance.GetTimestamp(),
		Version:      martianVersion,
		Pname:        pipestance.GetPname(),
		PsId:         psid,
		State:        pipestance.GetState(),
		JobMode:      jobMode,
		MaxCores:     rt.JobManager.GetMaxCores(),
		MaxMemGB:     rt.JobManager.GetMaxMemGB(),
		InvokePath:   invocationPath,
		InvokeSource: invocationSrc,
		MroPath:      util.FormatMroPath(mroPaths),
		ProfileMode:  profileMode,
		Port:         uiport,
		MroVersion:   mroVersion,
	}

	//=========================================================================
	// Register with mrv.
	//=========================================================================
	if mrvhost := os.Getenv("MRVHOST"); len(mrvhost) > 0 && enableUI {
		u := url.URL{
			Scheme: "http",
			Host:   mrvhost,
			Path:   api.QueryRegisterMrv,
		}
		if res, err := http.PostForm(u.String(), info.AsForm()); err == nil {
			if res.StatusCode == 200 {
				if content, err := ioutil.ReadAll(res.Body); err == nil {
					uiport = string(content)
				} else {
					util.LogError(err, "mrvcli", "Could not read response from mrv %s.", u.String())
				}
			} else {
				util.LogError(err, "mrvcli", "HTTP request failed %v.", res.StatusCode)
			}
		} else {
			util.LogError(err, "mrvcli", "HTTP request failed %s.", u.String())
		}
	}

	// Print this here because the log makes more sense when this appears before
	// the runloop messages start to appear.
	var listener net.Listener
	if enableUI {
		var err error
		dieWithoutUi := true
		if uiport == "" {
			uiport = "0"
			dieWithoutUi = false
		}
		if listener, err = net.Listen("tcp",
			fmt.Sprintf(":%s", uiport)); err != nil {
			util.PrintError(err, "webserv", "Cannot open port %s", uiport)
			if dieWithoutUi {
				os.Exit(1)
			}
		} else {
			u := url.URL{
				Scheme: "http",
				Host:   listener.Addr().String(),
			}
			info.Port = u.Port()
			u.Host = net.JoinHostPort(hostname, info.Port)
			if authKey != "" {
				q := u.Query()
				q.Set("auth", authKey)
				u.RawQuery = q.Encode()
			}
			util.Println("Serving UI at %s\n", u.String())
			pipestance.RecordUiPort(u.String())
		}
	} else {
		util.LogInfo("webserv", "UI disabled.")
	}

	if reattaching {
		// If it already exists, try to reattach to it.
		if !inspect {
			if err = pipestance.Reset(); err == nil {
				err = pipestance.RestartLocalJobs(jobMode)
			}
			util.DieIf(err)
		}
	} else if executingPreflight {
		util.Println("Running preflight checks (please wait)...")
	}

	//=========================================================================
	// Start web server.
	//=========================================================================
	if listener != nil {
		go runWebServer(listener, rt, &pipestanceBox, info, authKey, requireAuth)
	}

	//=========================================================================
	// Start run loop.
	//=========================================================================
	go runLoop(&pipestanceBox, stepSecs, vdrMode, noExit, enableUI, retries)

	// Let daemons take over.
	done := make(chan bool)
	<-done
}
