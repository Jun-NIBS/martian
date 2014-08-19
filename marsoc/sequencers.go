//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Marsoc sequencer management.
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const RUN_IS_INACTIVE_AFTER_HOURS = 1

type Run struct {
	Path         string      `json:"path"`
	Fname        string      `json:"-"`
	Fdate        string      `json:"fdate"`
	SeqcerName   string      `json:"seqcerName"`
	InstrId      string      `json:"instrId"`
	Num          int         `json:"num"`
	Fcid         string      `json:"fcid"`
	StartTime    string      `json:"startTime"`
	CompleteTime string      `json:"completeTime"`
	TouchTime    string      `json:"touchTime"`
	State        string      `json:"state"`
	Callsrc      interface{} `json:"callsrc"`
	Preprocess   interface{} `json:"preprocess"`
	Analysis     interface{} `json:"analysis"`
}

type Sequencer struct {
	pool          *SequencerPool
	name          string
	folderPattern *regexp.Regexp
	path          string
}

func NewSequencer(pool *SequencerPool, name string, folderPattern string) *Sequencer {
	self := &Sequencer{}
	self.pool = pool
	self.name = name
	self.folderPattern = regexp.MustCompile(folderPattern)
	self.path = path.Join(self.pool.path, self.name)
	return self
}

func NewMiSeqSequencer(pool *SequencerPool, name string) *Sequencer {
	return NewSequencer(pool, name, "^(\\d{6})_(\\w+)_(\\d+)_[0]{9}-([A-Z0-9]{5})$")
}

func NewHiSeqSequencer(pool *SequencerPool, name string) *Sequencer {
	return NewSequencer(pool, name, "^(\\d{6})_(\\w+)_(\\d+)_[AB]([A-Z0-9]{9})$")
}

// Parse the folder name into info fields and get various file mod times.
func (self *Sequencer) getFolderInfo(fname string, runchan chan *Run) (int, error) {
	// Parse folder name for basic info.
	parts := self.folderPattern.FindStringSubmatch(fname)
	num, err := strconv.Atoi(parts[3])
	if err != nil {
		return 0, err
	}

	run := Run{
		Path:       path.Join(self.path, fname),
		Fname:      fname,
		Fdate:      fmt.Sprintf("20%s-%s-%s", parts[1][0:2], parts[1][2:4], parts[1][4:6]),
		SeqcerName: self.name,
		InstrId:    parts[2],
		Num:        num,
		Fcid:       parts[4],
	}

	go func(run *Run) {
		startTime := getFileModTime(path.Join(run.Path, "RunInfo.xml"))
		completeTime := getFileModTime(path.Join(run.Path, "RTAComplete.txt"))
		touchTime := getFileModTime(path.Join(run.Path, "InterOp", "ExtractionMetricsOut.bin"))

		run.State = "failed"
		if !completeTime.IsZero() {
			run.State = "complete"
		} else if touchTime.IsZero() {
			run.State = "running"
		} else if !touchTime.IsZero() && time.Since(touchTime) < time.Hour*RUN_IS_INACTIVE_AFTER_HOURS {
			run.State = "running"
		}
		run.StartTime = startTime.Format(core.TIMEFMT)
		run.CompleteTime = completeTime.Format(core.TIMEFMT)
		run.TouchTime = touchTime.Format(core.TIMEFMT)
		runchan <- run
	}(&run)
	return 1, nil
}

// Return last modification time or zero.
func getFileModTime(p string) time.Time {
	info, err := os.Stat(p)
	if err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

type SequencerPool struct {
	path        string
	cachePath   string
	seqcers     []*Sequencer
	runList     []*Run
	runTable    map[string]*Run
	folderCache map[string]*Run
}

func NewSequencerPool(p string, cachePath string) *SequencerPool {
	self := &SequencerPool{}
	self.path = p
	self.cachePath = path.Join(cachePath, "sequencers")
	self.seqcers = []*Sequencer{}
	self.runList = []*Run{}
	self.runTable = map[string]*Run{}
	self.folderCache = map[string]*Run{}
	return self
}

// Try to pre-populate cache from on-disk JSON.
func (self *SequencerPool) loadCache() {
	bytes, err := ioutil.ReadFile(self.cachePath)
	if err != nil {
		core.LogError(err, "SEQPOOL", "Could not read cache file %s.", self.cachePath)
		return
	}
	if err := json.Unmarshal(bytes, &self.folderCache); err != nil {
		core.LogError(err, "SEQPOOL", "Could not parse JSON in cache file %s.", self.cachePath)
		return
	}

	self.indexCache()
	core.LogInfo("SEQPOOL", "%d runs loaded from cache.", len(self.runList))
}

// Sort the runList from newest to oldest.
// Index runs by flowcell id to support find() method.
func (self *SequencerPool) indexCache() {
	// Index the cached runs.
	self.runList = []*Run{}
	for _, run := range self.folderCache {
		self.runList = append(self.runList, run)
		self.runTable[run.Fcid] = run
	}
	sort.Sort(ByRevFdate(self.runList))
}

// Start an infinite inventory loop.
func (self *SequencerPool) goInventoryLoop() {
	go func() {
		for {
			self.inventorySequencers()

			// Wait for a bit.
			time.Sleep(time.Second * time.Duration(10))
		}
	}()
}

// Inventory all runs concurrently.
func (self *SequencerPool) inventorySequencers() {
	oldRunCount := len(self.runList)

	runchan := make(chan *Run)
	count := 0

	// Iterate over each sequencer.
	for _, seqcer := range self.seqcers {

		// Iterate over folders under each sequencer.
		paths, _ := filepath.Glob(path.Join(seqcer.path, "*"))
		for _, p := range paths {

			// Check that folder name matches pattern...
			fname := path.Base(p)
			if !seqcer.folderPattern.MatchString(fname) {
				continue
			}
			// ...is not already cached...
			if run, ok := self.folderCache[fname]; ok {
				// ...and is not yet complete.
				if run.State == "complete" {
					continue
				}
			}

			// Hit the filesystem for details.
			num, _ := seqcer.getFolderInfo(fname, runchan)
			count += num
		}
	}

	// Wait for all the getFolderInfo calls to complete.
	for i := 0; i < count; i++ {
		run := <-runchan
		self.folderCache[run.Fname] = run
	}

	self.indexCache()

	if len(self.runList) > oldRunCount {
		bytes, _ := json.MarshalIndent(self.folderCache, "", "    ")
		ioutil.WriteFile(self.cachePath, bytes, 0600)
		core.LogInfo("SEQPOOL", "%d new runs written to cache. %d total.", len(self.runList)-oldRunCount, len(self.runList))
	}
}

// Sorting support for Sequencer.runList
type ByRevFdate []*Run

func (a ByRevFdate) Len() int      { return len(a) }
func (a ByRevFdate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByRevFdate) Less(i, j int) bool {
	if a[i].Fdate == a[j].Fdate {
		return a[i].Num > a[j].Num
	}
	return a[i].Fdate > a[j].Fdate
}

// Add a named sequencer to the pool.
func (self *SequencerPool) add(name string) {
	if strings.HasPrefix(name, "miseq") {
		self.seqcers = append(self.seqcers, NewMiSeqSequencer(self, name))
		core.LogInfo("SEQPOOL", "Add MiSeq %s.", name)
	} else if strings.HasPrefix(name, "hiseq") {
		self.seqcers = append(self.seqcers, NewHiSeqSequencer(self, name))
		core.LogInfo("SEQPOOL", "Add HiSeq %s.", name)
	}
}

// Find a run in the pool by flowcell id.
func (self *SequencerPool) find(fcid string) *Run {
	return self.runTable[fcid]
}
