//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Marsoc pipestance management.
//
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"margo/core"
	"os"
	"path"
	"path/filepath"
	_ "regexp"
	_ "sort"
	_ "strconv"
	_ "strings"
	"sync"
	"time"
)

func makeFQName(pipeline string, psid string) string {
	return fmt.Sprintf("ID.%s.%s", psid, pipeline)
}

type PipestanceManager struct {
	rt        *core.Runtime
	path      string
	cachePath string
	stepms    int
	pipelines []string
	completed map[string]bool
	failed    map[string]bool
	runList   []*core.Pipestance
	runTable  map[string]*core.Pipestance
}

func NewPipestanceManager(rt *core.Runtime, pipestancesPath string, cachePath string, stepms int) *PipestanceManager {
	self := &PipestanceManager{}
	self.rt = rt
	self.path = pipestancesPath
	self.cachePath = path.Join(cachePath, "pipestances")
	self.stepms = stepms
	self.pipelines = rt.GetPipelineNames()
	self.completed = map[string]bool{}
	self.failed = map[string]bool{}
	self.runList = []*core.Pipestance{}
	self.runTable = map[string]*core.Pipestance{}
	return self
}

func (self *PipestanceManager) loadCache(unfail bool) {
	bytes, err := ioutil.ReadFile(self.cachePath)
	if err != nil {
		logError(err, "PIPEMAN", "Could not read cache file %s.", self.cachePath)
		return
	}

	var cache map[string]map[string]bool
	if err := json.Unmarshal(bytes, &cache); err != nil {
		logError(err, "PIPEMAN", "Could not parse JSON in cache file %s.", self.cachePath)
		return
	}

	if completed, ok := cache["completed"]; ok {
		self.completed = completed
	}
	// If we got commandline flag 'unfail', ignore cached fail flags and re-evaluate all
	// previously failed pipestances because we are probably trying to restart them.
	logInfo("PIPEMAN", "Unfail flag is set to %t.", unfail)
	if failed, ok := cache["failed"]; !unfail && ok {
		self.failed = failed
	}
	logInfo("PIPEMAN", "%d completed pipestance flags loaded from cache.", len(self.completed))
	logInfo("PIPEMAN", "%d failed pipestance flags loaded from cache.", len(self.failed))
}

func (self *PipestanceManager) writeCache() {
	cache := map[string]map[string]bool{
		"completed": self.completed,
		"failed":    self.failed,
	}
	bytes, _ := json.MarshalIndent(cache, "", "    ")
	ioutil.WriteFile(self.cachePath, bytes, 0600)
}

func (self *PipestanceManager) inventoryPipestances() {
	// Look for pipestances that are not marked as completed, reattach to them
	// and put them in the runlist.

	// Iterate over top level containers (flowcells).
	containers, _ := filepath.Glob(path.Join(self.path, "*"))
	for _, container := range containers {

		// Iterate over all known pipelines.
		for _, pipeline := range self.pipelines {
			psids, _ := filepath.Glob(path.Join(self.path, container, pipeline, "*"))

			// Iterate over psids under this pipeline.
			for _, psid := range psids {
				fqname := makeFQName(pipeline, psid)
				if self.completed[fqname] || self.failed[fqname] {
					continue
				}
				pipestance, _, err := self.rt.Reattach(psid, path.Join(self.path, container, pipeline, psid, "HEAD"))
				if err != nil {
					logError(err, "PIPEMAN", "%s was previously cached but no longer exists.", fqname)
					self.writeCache()
					continue
				}
				logInfo("PIPEMAN", "%s is not cached as completed or failed, so pushing onto runList.", fqname)
				self.runList = append(self.runList, pipestance)
				self.runTable[fqname] = pipestance
			}
		}
	}
}

// Start an infinite process loop.
func (self *PipestanceManager) goRunListLoop() {
	go func() {
		for {
			self.processRunList()

			// Wait for a bit.
			time.Sleep(time.Second * time.Duration(self.stepms))
		}
	}()
}

func (self *PipestanceManager) processRunList() {
	continueToRunList := []*core.Pipestance{}
	done := make(chan bool)
	mutex := &sync.Mutex{}
	for _, pipestance := range self.runList {
		go func() {
			nodes := pipestance.Node().AllNodes()

			// Metadata refreshes can be asynchronous amongst themselves but
			// all must be complete and consistent before starting to step.
			ndone := make(chan bool)
			count := 0
			for _, node := range nodes {
				count += node.RefreshMetadata(ndone)
			}
			for i := 0; i < count; i++ {
				<-ndone
			}

			state := pipestance.GetOverallState()
			fqname := pipestance.GetFQName()
			if state == "complete" {
				// If pipestance is done, remove from runTable, mark it in the
				// cache as completed, and flush the cache.
				logInfo("PIPEMAN", "Complete and removing from runList: %s.", fqname)
				mutex.Lock()
				delete(self.runTable, fqname)
				self.completed[fqname] = true
				self.writeCache()
				mutex.Unlock()
				pipestance.Immortalize()
			} else if state == "failed" {
				logInfo("PIPEMAN", "Failed and removing from runList: %s.", fqname)
				mutex.Lock()
				delete(self.runTable, fqname)
				self.failed[fqname] = true
				self.writeCache()
				mutex.Unlock()
				pipestance.Immortalize()
			} else {
				// If it is not done, step and keep it running.
				mutex.Lock()
				continueToRunList = append(continueToRunList, pipestance)
				mutex.Unlock()
				for _, node := range nodes {
					node.Step()
				}
			}
			done <- true
		}()
	}
	for i := 0; i < len(self.runList); i++ {
		<-done
	}
	// Remove completed and failed pipestances by omission.
	self.runList = continueToRunList
}

func (self *PipestanceManager) Invoke(container string, pipeline string, psid string, src string) error {
	psPath := path.Join(self.path, container, pipeline, psid, self.rt.CodeVersion)
	if err := os.MkdirAll(psPath, 0700); err != nil {
		return err
	}
	pipestance, _, err := self.rt.InvokeWithSource(psid, src, psPath)
	if err != nil {
		return err
	}
	fqname := pipestance.GetFQName()
	logInfo("PIPEMAN", "Instantiating and pushing to runList: %s.", fqname)
	self.runList = append(self.runList, pipestance)
	self.runTable[fqname] = pipestance
	headPath := path.Join(self.path, container, pipeline, psid, "HEAD")
	os.Remove(headPath)
	os.Symlink(self.rt.CodeVersion, headPath)

	return nil
}

func (self *PipestanceManager) GetPipestanceState(container string, pipeline string, psid string) (string, bool) {
	fqname := makeFQName(pipeline, psid)
	if _, ok := self.completed[fqname]; ok {
		return "complete", true
	}
	if _, ok := self.failed[fqname]; ok {
		return "failed", true
	}
	if run, ok := self.runTable[fqname]; ok {
		return run.GetOverallState(), true
	}
	return "", false
}

func (self *PipestanceManager) GetPipestanceSerialization(container string, pipeline string, psid string) (interface{}, bool) {
	psPath := path.Join(self.path, container, pipeline, psid, "HEAD")
	if ser, ok := self.rt.GetSerialization(psPath); ok {
		return ser, true
	}
	pipestance, ok := self.GetPipestance(container, pipeline, psid)
	if !ok {
		return nil, false
	}
	data := []interface{}{}
	for _, node := range pipestance.Node().AllNodes() {
		data = append(data, node.Serialize())
	}
	return data, true
}

func (self *PipestanceManager) GetPipestance(container string, pipeline string, psid string) (*core.Pipestance, bool) {
	fqname := makeFQName(pipeline, psid)

	// Check if requested pipestance actually exists.
	if _, ok := self.GetPipestanceState(container, pipeline, psid); !ok {
		return nil, false
	}

	// Check the runTable.
	if pipestance, ok := self.runTable[fqname]; ok {
		return pipestance, true
	}

	// Reattach to the pipestance.
	pipestance, _, err := self.rt.Reattach(psid, path.Join(self.path, container, pipeline, psid, "HEAD"))
	if err != nil {
		return nil, false
	}

	// Refresh its metadata state and return.
	nodes := pipestance.Node().AllNodes()
	done := make(chan bool)
	count := 0
	for _, node := range nodes {
		count += node.RefreshMetadata(done)
	}
	for i := 0; i < count; i++ {
		<-done
	}
	return pipestance, true
}
