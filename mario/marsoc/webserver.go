//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Marsoc webserver.
//
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"mario/core"
	"mario/gzip"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
)

//=============================================================================
// Web server helpers.
//=============================================================================

// Render a page from template.
func render(dir string, tname string, data interface{}) string {
	tmpl, err := template.New(tname).Delims("[[", "]]").ParseFiles(core.RelPath(path.Join("..", dir, tname)))
	if err != nil {
		return err.Error()
	}
	var doc bytes.Buffer
	err = tmpl.Execute(&doc, data)
	if err != nil {
		return err.Error()
	}
	return doc.String()
}

// Render JSON from data.
func makeJSON(data interface{}) string {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err.Error()
	}
	return string(bytes)
}

//=============================================================================
// Page and form structs.
//=============================================================================
// Pages
type MainPage struct {
	InstanceName     string
	Admin            bool
	MarsocVersion    string
	PipelinesVersion string
}
type GraphPage struct {
	InstanceName string
	Container    string
	Pname        string
	Psid         string
	Admin        bool
	AdminStyle   bool
}

// Forms
type FcidForm struct {
	Fcid string
}

type MetasampleIdForm struct {
	Id string
}

type MetadataForm struct {
	Path string
	Name string
}

// For a given sample, update the following fields:
// Pname    The analysis pipeline to be run on it, according to argshim
// Psstate  Current state of the sample's pipestance, if any
// Callsrc  MRO invoke source to analyze this sample, per argshim
func updateSampleState(sample *Sample, rt *core.Runtime, lena *Lena,
	argshim *ArgShim, pman *PipestanceManager) {
	pname := argshim.getPipelineForSample(sample)
	sample.Pname = pname
	sample.Psstate, _ = pman.GetPipestanceState(sample.Pscontainer, pname, strconv.Itoa(sample.Id))
	sample.Ready_to_invoke = true

	// From each def in the sample_defs, if the BCL_PROCESSOR pipestance
	// exists, add a mapping from the fcid to that pipestance's fastq_path.
	// This map will be used by the argshim to build the MRO invocation.
	fastqPaths := map[string]string{}
	for _, sample_def := range sample.Sample_defs {
		sd_fcid := sample_def.Sequencing_run.Name
		sd_state, ok := pman.GetPipestanceState(sd_fcid, "BCL_PROCESSOR_PD", sd_fcid)
		if !ok || sd_state != "complete" {
			sample.Ready_to_invoke = false
		}
		if ok {
			sample_def.Sequencing_run.Psstate = sd_state
		}
		if preprocPipestance, _ := pman.GetPipestance(sd_fcid, "BCL_PROCESSOR_PD", sd_fcid); preprocPipestance != nil {
			if outs, ok := preprocPipestance.GetOuts(0).(map[string]interface{}); ok {
				if fastq_path, ok := outs["fastq_path"].(string); ok {
					fastqPaths[sd_fcid] = fastq_path
				}
			}
		}
	}
	sample.Callsrc = argshim.buildCallSourceForSample(rt, lena.getSampleBagWithId(strconv.Itoa(sample.Id)), fastqPaths)
}

func runWebServer(uiport string, instanceName string, marioVersion string,
	mroVersion string, rt *core.Runtime, pool *SequencerPool,
	pman *PipestanceManager, lena *Lena, argshim *ArgShim, info map[string]string) {

	//=========================================================================
	// Configure server.
	//=========================================================================
	m := martini.New()
	r := martini.NewRouter()
	m.Use(martini.Recovery())
	m.Use(martini.Static(core.RelPath("../web-marsoc/res"), martini.StaticOptions{"", true, "index.html", nil}))
	m.Use(martini.Static(core.RelPath("../web-marsoc/client"), martini.StaticOptions{"", true, "index.html", nil}))
	m.Use(martini.Static(core.RelPath("../web/res"), martini.StaticOptions{"", true, "index.html", nil}))
	m.Use(martini.Static(core.RelPath("../web/client"), martini.StaticOptions{"", true, "index.html", nil}))
	m.MapTo(r, (*martini.Routes)(nil))
	m.Action(r.Handle)
	app := &martini.ClassicMartini{m, r}
	app.Use(gzip.All())

	//=========================================================================
	// MARSOC renderers and API.
	//=========================================================================

	// Page renderers.
	app.Get("/", func() string {
		return render("web-marsoc/templates", "marsoc.html",
			&MainPage{
				InstanceName:     instanceName,
				Admin:            false,
				MarsocVersion:    marioVersion,
				PipelinesVersion: mroVersion,
			})
	})
	app.Get("/admin", func() string {
		return render("web-marsoc/templates", "marsoc.html",
			&MainPage{
				InstanceName:     instanceName,
				Admin:            true,
				MarsocVersion:    marioVersion,
				PipelinesVersion: mroVersion,
			})
	})
	app.Get("/metasamples", func() string {
		return render("web-marsoc/templates", "metasamples.html",
			&MainPage{
				InstanceName:     instanceName,
				Admin:            true,
				MarsocVersion:    marioVersion,
				PipelinesVersion: mroVersion,
			})
	})

	// Get all sequencing runs.
	app.Get("/api/get-runs", func() string {

		// Iterate concurrently over all sequencing runs and populate or
		// update the state fields in each run before sending to client.
		var wg sync.WaitGroup
		wg.Add(len(pool.runList))
		for _, run := range pool.runList {
			go func(wg *sync.WaitGroup, run *Run) {
				defer wg.Done()

				// Get the state of the BCL_PROCESSOR_PD pipeline for this run.
				run.Preprocess = nil
				if state, ok := pman.GetPipestanceState(run.Fcid, "BCL_PROCESSOR_PD", run.Fcid); ok {
					run.Preprocess = state
				}

				// If BCL_PROCESSOR_PD is not complete yet, neither is ANALYZER_PD.
				run.Analysis = nil
				if run.Preprocess != "complete" {
					return
				}

				// Get the state of ANALYZER_PD for each sample in this run.
				samples := lena.getSamplesForFlowcell(run.Fcid)
				if len(samples) == 0 {
					return
				}

				// Gather the states of ANALYZER_PD for each sample.
				states := []string{}
				run.Analysis = "running"
				for _, sample := range samples {
					state, ok := pman.GetPipestanceState(run.Fcid, argshim.getPipelineForSample(sample), strconv.Itoa(sample.Id))
					if ok {
						states = append(states, state)
					} else {
						// If some pipestance doesn't exist, show no state for analysis.
						run.Analysis = nil
						return
					}
				}

				// If every sample is complete, show analysis as complete.
				every := true
				for _, state := range states {
					if state != "complete" {
						every = false
						break
					}
				}
				if every && len(states) > 0 {
					run.Analysis = "complete"
				}

				// If any sample is failed, show analysis as failed.
				for _, state := range states {
					if state == "failed" {
						run.Analysis = "failed"
						break
					}
				}
			}(&wg, run)
		}
		wg.Wait()

		// Send JSON for all runs in the sequencer pool.
		return makeJSON(pool.runList)
	})

	// Get samples for a given flowcell id.
	app.Post("/api/get-samples", binding.Bind(FcidForm{}), func(body FcidForm, params martini.Params) string {
		samples := lena.getSamplesForFlowcell(body.Fcid)

		var wg sync.WaitGroup
		wg.Add(len(samples))
		for _, sample := range samples {
			go func(wg *sync.WaitGroup, sample *Sample) {
				updateSampleState(sample, rt, lena, argshim, pman)
				wg.Done()
			}(&wg, sample)
		}
		wg.Wait()
		return makeJSON(samples)
	})

	// Build BCL_PROCESSOR_PD call source.
	app.Post("/api/get-callsrc", binding.Bind(FcidForm{}), func(body FcidForm, params martini.Params) string {
		if run, ok := pool.runTable[body.Fcid]; ok {
			return argshim.buildCallSourceForRun(rt, run)
		}
		return fmt.Sprintf("Could not find run with fcid %s.", body.Fcid)
	})

	// Get all metasamples.
	app.Get("/api/get-metasamples", func() string {
		metasamples := lena.getMetasamples()
		for _, metasample := range metasamples {
			state, ok := pman.GetPipestanceState(metasample.Pscontainer, argshim.getPipelineForSample(metasample), strconv.Itoa(metasample.Id))
			if ok {
				metasample.Psstate = state
			}
		}
		return makeJSON(lena.getMetasamples())
	})

	// Build analysis call source for a metasample with given id.
	app.Post("/api/get-metasample-callsrc", binding.Bind(MetasampleIdForm{}), func(body MetasampleIdForm, params martini.Params) string {
		if sample := lena.getSampleWithId(body.Id); sample != nil {
			updateSampleState(sample, rt, lena, argshim, pman)
			return makeJSON(sample)
		}
		return fmt.Sprintf("Could not find metasample with id %s.", body.Id)
	})

	//=========================================================================
	// Pipestance graph renderers and display API.
	//=========================================================================

	// Page renderers.
	app.Get("/pipestance/:container/:pname/:psid", func(p martini.Params) string {
		return render("web/templates", "graph.html", &GraphPage{
			InstanceName: instanceName,
			Container:    p["container"],
			Pname:        p["pname"],
			Psid:         p["psid"],
			Admin:        false,
			AdminStyle:   false,
		})
	})
	app.Get("/admin/pipestance/:container/:pname/:psid", func(p martini.Params) string {
		return render("web/templates", "graph.html", &GraphPage{
			InstanceName: instanceName,
			Container:    p["container"],
			Pname:        p["pname"],
			Psid:         p["psid"],
			Admin:        true,
			AdminStyle:   true,
		})
	})

	// Get graph nodes.
	app.Get("/api/get-state/:container/:pname/:psid", func(p martini.Params) string {
		container := p["container"]
		pname := p["pname"]
		psid := p["psid"]
		state := map[string]interface{}{}
		state["error"] = nil
		psinfo := map[string]string{}
		for k, v := range info {
			psinfo[k] = v
		}
		//core.LogInfo("pipeman", "> GetPipestance")
		if pipestance, ok := pman.GetPipestance(container, pname, psid); ok {
			psstate := pipestance.GetState()
			psinfo["state"] = psstate
			psinfo["pname"] = pname
			psinfo["psid"] = psid
			psinfo["invokesrc"] = pipestance.GetInvokeSrc()
			if psstate == "failed" {
				fqname, summary, log, errpaths := pipestance.GetFatalError()
				errpath := ""
				if len(errpaths) > 0 {
					errpath = errpaths[0]
				}
				state["error"] = map[string]string{
					"fqname":  fqname,
					"path":    errpath,
					"summary": summary,
					"log":     log,
				}
			}
		}
		//core.LogInfo("pipeman", "< GetPipestance")
		//core.LogInfo("pipeman", "> GetPipestanceSerialization")
		ser, _ := pman.GetPipestanceSerialization(container, pname, psid)
		state["nodes"] = ser
		state["info"] = psinfo
		js := makeJSON(state)
		//core.LogInfo("pipeman", "< GetPipestanceSerialization (%d bytes)", len(js))
		return js
	})

	// Get metadata file contents.
	app.Post("/api/get-metadata/:container/:pname/:psid", binding.Bind(MetadataForm{}), func(body MetadataForm, p martini.Params) string {
		if strings.Index(body.Path, "..") > -1 {
			return "'..' not allowed in path."
		}
		data, err := ioutil.ReadFile(path.Join(body.Path, "_"+body.Name))
		if err != nil {
			return err.Error()
		}
		return string(data)
	})

	// Restart failed stage.
	app.Post("/api/restart/:container/:pname/:psid/:fqname", func(p martini.Params) string {
		pman.UnfailPipestance(p["container"], p["pname"], p["psid"], p["fqname"])
		return ""
	})

	//=========================================================================
	// Pipestance invocation API.
	//=========================================================================

	// Invoke BCL_PROCESSOR_PD.
	app.Post("/api/invoke-preprocess", binding.Bind(FcidForm{}), func(body FcidForm, p martini.Params) string {
		// Use argshim to build MRO call source and invoke.
		fcid := body.Fcid
		run := pool.find(fcid)
		if err := pman.Invoke(fcid, "BCL_PROCESSOR_PD", fcid, argshim.buildCallSourceForRun(rt, run)); err != nil {
			return err.Error()
		}
		return ""
	})

	// Invoke ANALYZER_PD.
	app.Post("/api/invoke-analysis", binding.Bind(FcidForm{}), func(body FcidForm, p martini.Params) string {
		// Get all the samples for this fcid.
		samples := lena.getSamplesForFlowcell(body.Fcid)

		// Invoke the appropriate pipeline on each sample.
		errors := []string{}
		for _, sample := range samples {
			// Invoke the pipestance.
			if err := pman.Invoke(sample.Pscontainer, sample.Pname, strconv.Itoa(sample.Id), sample.Callsrc); err != nil {
				errors = append(errors, err.Error())
			}
		}
		return strings.Join(errors, "\n")
	})

	// Invoke metasample ANALYZER_PD.
	app.Post("/api/invoke-metasample-analysis", binding.Bind(MetasampleIdForm{}), func(body MetasampleIdForm, p martini.Params) string {
		// Get the sample with this id.
		sample := lena.getSampleWithId(body.Id)
		if sample == nil {
			return fmt.Sprintf("Sample '%s' not found.", body.Id)
		}

		// Invoke the pipestance.
		if err := pman.Invoke(sample.Pscontainer, sample.Pname, strconv.Itoa(sample.Id), sample.Callsrc); err != nil {
			return err.Error()
		}
		return ""
	})

	//=========================================================================
	// Pipestance archival API.
	//=========================================================================

	// Archive pipestances.
	app.Post("/api/archive-fcid-samples", binding.Bind(FcidForm{}), func(body FcidForm, p martini.Params) string {
		// Get all the samples for this fcid.
		samples := lena.getSamplesForFlowcell(body.Fcid)

		// Archive the samples.
		errors := []string{}
		for _, sample := range samples {
			if err := pman.ArchivePipestanceHead(sample.Pscontainer, sample.Pname, strconv.Itoa(sample.Id)); err != nil {
				errors = append(errors, err.Error())
			}
		}
		return strings.Join(errors, "\n")
	})

	//=========================================================================
	// Start webserver.
	//=========================================================================
	if err := http.ListenAndServe(":"+uiport, app); err != nil {
		// Don't continue starting if we detect another instance running.
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
