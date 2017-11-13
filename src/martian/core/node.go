//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian runtime management for pipeline graph nodes.
//

package core

import (
	"fmt"
	"martian/syntax"
	"martian/util"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//=============================================================================
// Node
//=============================================================================

type Nodable interface {
	getNode() *Node
}

// Represents a node in the pipeline graph.
type Node struct {
	parent             Nodable
	rt                 *Runtime
	kind               string
	name               string
	fqname             string
	path               string
	metadata           *Metadata
	outparams          *syntax.Params
	argbindings        map[string]*Binding
	argbindingList     []*Binding // for stable ordering
	retbindings        map[string]*Binding
	retbindingList     []*Binding // for stable ordering
	sweepbindings      []*Binding
	subnodes           map[string]Nodable
	prenodes           map[string]Nodable
	directPrenodes     []Nodable
	postnodes          map[string]Nodable
	frontierNodes      *threadSafeNodeMap
	forks              []*Fork
	split              bool
	state              MetadataState
	volatile           bool
	local              bool
	preflight          bool
	stagecodeLang      syntax.StageCodeType
	stagecodeCmd       string
	journalPath        string
	tmpPath            string
	mroPaths           []string
	mroVersion         string
	envs               map[string]string
	invocation         *InvocationData
	blacklistedFromMRT bool // Don't used cached data when MRT'ing
}

// Represents an edge in the pipeline graph.
type EdgeInfo struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Encapsulates information about a node failure.
type NodeErrorInfo struct {
	FQname  string `json:"fqname"`
	Path    string `json:"path"`
	Summary string `json:"summary,omitempty"`
	Log     string `json:"log,omitempty"`
}

type NodeInfo struct {
	Name          string               `json:"name"`
	Fqname        string               `json:"fqname"`
	Type          string               `json:"type"`
	Path          string               `json:"path"`
	State         MetadataState        `json:"state"`
	Metadata      *MetadataInfo        `json:"metadata"`
	SweepBindings []*BindingInfo       `json:"sweepbindings"`
	Forks         []*ForkInfo          `json:"forks"`
	Edges         []EdgeInfo           `json:"edges"`
	StagecodeLang syntax.StageCodeType `json:"stagecodeLang"`
	StagecodeCmd  string               `json:"stagecodeCmd"`
	Error         *NodeErrorInfo       `json:"error,omitempty"`
}

func (self *Node) getNode() *Node { return self }

func NewNode(parent Nodable, kind string, callStm *syntax.CallStm, callables *syntax.Callables) *Node {
	self := &Node{}
	self.parent = parent

	self.rt = parent.getNode().rt
	self.kind = kind
	self.name = callStm.Id
	self.fqname = parent.getNode().fqname + "." + self.name
	self.path = path.Join(parent.getNode().path, self.name)
	self.journalPath = parent.getNode().journalPath
	self.tmpPath = parent.getNode().tmpPath
	self.mroPaths = parent.getNode().mroPaths
	self.mroVersion = parent.getNode().mroVersion
	self.envs = parent.getNode().envs
	self.invocation = parent.getNode().invocation
	self.metadata = NewMetadata(self.fqname, self.path)
	self.volatile = callStm.Modifiers.Volatile
	self.local = callStm.Modifiers.Local
	self.preflight = callStm.Modifiers.Preflight

	self.outparams = callables.Table[self.name].GetOutParams()
	self.argbindings = map[string]*Binding{}
	self.argbindingList = []*Binding{}
	self.retbindings = map[string]*Binding{}
	self.retbindingList = []*Binding{}
	self.subnodes = map[string]Nodable{}
	self.prenodes = map[string]Nodable{}
	self.directPrenodes = []Nodable{}
	self.postnodes = map[string]Nodable{}
	self.frontierNodes = parent.getNode().frontierNodes

	for id, bindStm := range callStm.Bindings.Table {
		binding := NewBinding(self, bindStm)
		self.argbindings[id] = binding
		self.argbindingList = append(self.argbindingList, binding)
	}
	prenodes, directPrenodes := recurseBoundNodes(self.argbindingList)
	for key, prenode := range prenodes {
		self.prenodes[key] = prenode
		prenode.getNode().postnodes[self.fqname] = self
	}
	self.directPrenodes = append(self.directPrenodes, directPrenodes...)
	// Do not set state = getState here, or else nodes will wrongly report
	// complete before the first refreshMetadata call
	return self
}

// Get the set of distinct precurser nodes and direct precurser nodes based on
// the given binding set.
func recurseBoundNodes(bindingList []*Binding) (prenodes map[string]Nodable, parents []Nodable) {
	found := make(map[string]Nodable)
	allParents := make(map[Nodable]struct{})
	parentList := make([]Nodable, 0, len(bindingList))
	addPrenode := func(prenode Nodable) {
		prename := prenode.getNode().fqname
		if existing, ok := found[prename]; !ok {
			found[prename] = prenode
		} else if existing != prenode {
			util.LogInfo("runtime",
				"WARNING: multiple prenodes with the same fqname %s",
				prename)
		}
	}
	for _, binding := range bindingList {
		if binding.mode == "reference" && binding.boundNode != nil {
			addPrenode(binding.boundNode)
			parent := binding.parentNode
			if _, ok := allParents[parent]; !ok {
				allParents[parent] = struct{}{}
				parentList = append(parentList, parent)
			}
		} else if binding.mode == "array" {
			prenodes, parents := recurseBoundNodes(binding.value.([]*Binding))
			for _, prenode := range prenodes {
				addPrenode(prenode)
			}
			for _, parent := range parents {
				if _, ok := allParents[parent]; !ok {
					allParents[parent] = struct{}{}
					parentList = append(parentList, parent)
				}
			}
		}
	}
	return found, parentList
}

//
// Folder construction
//
func (self *Node) mkdirs() error {
	if err := util.MkdirAll(self.path); err != nil {
		msg := fmt.Sprintf("Could not create root directory for %s: %s", self.fqname, err.Error())
		util.LogError(err, "runtime", msg)
		self.metadata.WriteRaw(Errors, msg)
		return err
	}
	if err := util.Mkdir(self.journalPath); err != nil {
		msg := fmt.Sprintf("Could not create directories for %s: %s", self.fqname, err.Error())
		util.LogError(err, "runtime", msg)
		self.metadata.WriteRaw(Errors, msg)
		return err
	}
	if err := util.Mkdir(self.tmpPath); err != nil {
		msg := fmt.Sprintf("Could not create directories for %s: %s", self.fqname, err.Error())
		util.LogError(err, "runtime", msg)
		self.metadata.WriteRaw(Errors, msg)
		return err
	}

	var wg sync.WaitGroup
	for _, fork := range self.forks {
		wg.Add(1)
		go func(f *Fork) {
			f.mkdirs()
			wg.Done()
		}(fork)
	}
	wg.Wait()
	return nil
}

//
// Sweep management
//
func (self *Node) buildUniqueSweepBindings(bindings map[string]*Binding) {
	// Add all unique sweep bindings to self.sweepbindings.
	// Make sure to use sweepRootId to uniquify and not just id.
	// This will ensure stages bind a sweep value to differently
	// named local params will not create unnecessary fork multiplication.

	bindingTable := map[string]*Binding{}

	// Add local sweep bindings.
	for _, binding := range bindings {
		if binding.sweep {
			bindingTable[binding.sweepRootId] = binding
		}
	}
	// Add upstream sweep bindings (from prenodes).
	for _, prenode := range self.prenodes {
		for _, binding := range prenode.getNode().sweepbindings {
			bindingTable[binding.sweepRootId] = binding
		}
	}

	// Sort keys in bindingTable to ensure stable fork ordering.
	ids := []string{}
	for id := range bindingTable {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Save sorted, unique sweep bindings.
	for _, id := range ids {
		binding := bindingTable[id]
		self.sweepbindings = append(self.sweepbindings, binding)
	}
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

func (self *Node) buildForks(bindings map[string]*Binding) {
	self.buildUniqueSweepBindings(bindings)

	// Expand out sweep values for each binding.
	paramIds := []string{}
	argRanges := []interface{}{}
	for _, binding := range self.sweepbindings {
		// This needs to use self.sweepRootId because Binding::resolve
		// will also match using sweepRootId, not id.
		// This is required for proper forking when param names don't match.
		paramIds = append(paramIds, binding.sweepRootId)
		argRanges = append(argRanges, binding.resolve(nil))
	}

	// Build out argument permutations.
	for i, valPermute := range cartesianProduct(argRanges) {
		argPermute := map[string]interface{}{}
		for j, paramId := range paramIds {
			argPermute[paramId] = valPermute.([]interface{})[j]
		}
		self.forks = append(self.forks, NewFork(self, i, argPermute))
	}

	// Match forks with their parallel, same-value upstream forks.
	for _, fork := range self.forks {
		for _, subnode := range self.subnodes {
			if matchedFork := subnode.getNode().matchFork(fork.argPermute); matchedFork != nil {
				matchedFork.parentFork = fork
				fork.subforks = append(fork.subforks, matchedFork)
			}
		}
	}
}

func (self *Node) matchFork(targetArgPermute map[string]interface{}) *Fork {
	if targetArgPermute == nil {
		return nil
	}
	for _, fork := range self.forks {
		every := true
		for paramId, argValue := range fork.argPermute {
			if !reflect.DeepEqual(targetArgPermute[paramId], argValue) {
				every = false
				break
			}
		}
		if every {
			return fork
		}
	}
	return nil
}

//
// Subnode management
//
func (self *Node) setPrenode(prenode Nodable) {
	for _, subnode := range self.subnodes {
		subnode.getNode().setPrenode(prenode)
	}
	self.prenodes[prenode.getNode().fqname] = prenode
	prenode.getNode().postnodes[self.fqname] = self
}

func (self *Node) findBoundNode(id string, outputId string, mode string,
	value interface{}) (Nodable, string, string, interface{}) {
	if self.kind == "pipeline" {
		subnode := self.subnodes[id]
		for _, binding := range subnode.getNode().retbindings {
			if binding.id == outputId {
				return binding.boundNode, binding.output, binding.mode, binding.value
			}
		}
		return subnode, outputId, mode, value
	}
	return self, outputId, mode, value
}

func (self *Node) addFrontierNode(node Nodable) {
	self.frontierNodes.Add(node.getNode().fqname, node)
}

func (self *Node) removeFrontierNode(node Nodable) {
	self.frontierNodes.Remove(node.getNode().fqname)
}

func (self *Node) getFrontierNodes() []*Node {
	return self.frontierNodes.GetNodes()
}

func (self *Node) allNodes() []*Node {
	all := []*Node{self}

	// Enumerate and sort the keys in subnodes first.
	// This ensures a stable chirality for the dag UI.
	ids := []string{}
	for id := range self.subnodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Build a list of all subnodes.
	for _, id := range ids {
		subnode := self.subnodes[id]
		all = append(all, subnode.getNode().allNodes()...)
	}
	return all
}

func (self *Node) find(fqname string) *Node {
	if self.fqname == fqname {
		return self
	}
	for _, subnode := range self.subnodes {
		node := subnode.getNode().find(fqname)
		if node != nil {
			return node
		}
	}
	return nil
}

//
// State management
//
func (self *Node) collectMetadatas() []*Metadata {
	metadatas := []*Metadata{self.metadata}
	for _, fork := range self.forks {
		metadatas = append(metadatas, fork.collectMetadatas()...)
	}
	return metadatas
}

func (self *Node) loadMetadata() {
	metadatas := self.collectMetadatas()
	for _, metadata := range metadatas {
		// Load metadata file cache
		metadata.loadCache()

		// Reset metadata heartbeat timer
		metadata.resetHeartbeat()
	}
	self.state = self.getState()
	self.addFrontierNode(self)
}

func (self *Node) removeMetadata() {
	for _, fork := range self.forks {
		fork.removeMetadata()
	}
}

func (self *Node) getFork(index int) *Fork {
	if index < len(self.forks) {
		return self.forks[index]
	}
	return nil
}

func (self *Node) getState() MetadataState {
	// If every fork is complete, we're complete.
	complete := true
	for _, fork := range self.forks {
		if fork.getState() != Complete {
			complete = false
			break
		}
	}
	if complete {
		return Complete
	}
	// If any fork is failed, we're failed.
	for _, fork := range self.forks {
		if fork.getState() == Failed {
			return Failed
		}
	}
	// If any prenode is not complete, we're waiting.
	for _, prenode := range self.prenodes {
		if prenode.getNode().getState() != Complete {
			return Waiting
		}
	}
	// Otherwise we're running.
	return Running

}

func (self *Node) reset() error {
	if self.rt.fullStageReset {
		util.PrintInfo("runtime", "(reset)           %s", self.fqname)

		// Blow away the entire stage node.
		if err := os.RemoveAll(self.path); err != nil {
			util.PrintInfo("runtime", "Cannot reset the stage because its folder contents could not be deleted.\n\nPlease resolve this error in order to continue running the pipeline:")
			return err
		}
		// Remove all related files from journal directory.
		if files, err := filepath.Glob(path.Join(self.journalPath, self.fqname+"*")); err == nil {
			for _, file := range files {
				os.Remove(file)
			}
		}

		// Clear chunks in the forks so they can be rebuilt on split.
		for _, fork := range self.forks {
			fork.reset()
		}

		// Create stage node directories.
		if err := self.mkdirs(); err != nil {
			return err
		}
	} else {
		for _, fork := range self.forks {
			if err := fork.resetPartial(); err != nil {
				return err
			}
		}
	}

	// Refresh the metadata.
	self.loadMetadata()
	return nil
}

func (self *Node) restartLocallyQueuedJobs() error {
	if self.rt.fullStageReset {
		// If entire stages got blown away then this isn't needed.
		return nil
	}
	for _, fork := range self.forks {
		if err := fork.restartLocallyQueuedJobs(); err != nil {
			return err
		}
	}
	return nil
}

func (self *Node) restartLocalJobs() error {
	if self.rt.fullStageReset {
		// If entire stages got blown away then this isn't needed.
		return nil
	}
	for _, fork := range self.forks {
		if err := fork.restartLocalJobs(); err != nil {
			return err
		}
	}
	return nil
}

func (self *Node) checkHeartbeats() {
	for _, metadata := range self.collectMetadatas() {
		metadata.checkHeartbeat()
	}
}

func (self *Node) kill(message string) {
	for _, fork := range self.forks {
		fork.kill(message)
	}
}

func (self *Node) postProcess() {
	os.RemoveAll(self.journalPath)
	os.RemoveAll(self.tmpPath)

	for _, fork := range self.forks {
		fork.postProcess()
	}
}

func (self *Node) cachePerf() {
	if _, ok := self.vdrKill(); ok {
		// Cache all fork performance info if node can be VDR-ed.
		for _, fork := range self.forks {
			fork.cachePerf()
		}
	}
}

func (self *Node) GetFQName() string {
	return self.fqname
}

func (self *Node) getFatalError() (string, bool, string, string, MetadataFileName, []string) {
	for _, metadata := range self.collectMetadatas() {
		if state, _ := metadata.getState(); state != Failed {
			continue
		}
		if metadata.exists(Errors) {
			errlog := metadata.readRaw(Errors)
			summary := "<none>"
			if self.stagecodeLang == syntax.PythonStage {
				errlines := strings.Split(errlog, "\n")
				if len(errlines) >= 2 {
					summary = errlines[len(errlines)-2]
				} else if len(errlines) == 1 {
					summary = errlines[0]
				}
			}
			errpaths := []string{
				metadata.MetadataFilePath(Errors),
				metadata.MetadataFilePath(StdOut),
				metadata.MetadataFilePath(StdErr),
			}
			if self.rt.enableStackVars {
				errpaths = append(errpaths, metadata.MetadataFilePath(Stackvars))
			}
			return metadata.fqname, self.preflight, summary, errlog, Errors, errpaths
		}
		if metadata.exists(Assert) {
			assertlog := metadata.readRaw(Assert)
			summary := "<none>"
			assertlines := strings.Split(assertlog, "\n")
			if len(assertlines) >= 1 {
				summary = assertlines[len(assertlines)-1]
			}
			return metadata.fqname, self.preflight, summary, assertlog, Assert, []string{
				metadata.MetadataFilePath(Assert),
			}
		}
	}
	return "", false, "", "", "", []string{}
}

// Returns true if there is no error or if the error is one we expect to not
// recur if the pipeline is rerun.
func (self *Node) isErrorTransient() (bool, string) {
	passRegexp, _ := getRetryRegexps()
	for _, metadata := range self.collectMetadatas() {
		if state, _ := metadata.getState(); state != Failed {
			continue
		}
		if metadata.exists(Assert) {
			return false, ""
		}
		if metadata.exists(Errors) {
			errlog := metadata.readRaw(Errors)
			for _, line := range strings.Split(errlog, "\n") {
				for _, re := range passRegexp {
					if re.MatchString(line) {
						return true, errlog
					}
				}
			}
			return false, errlog
		}
	}
	return true, ""
}

func (self *Node) step() bool {
	if self.state == Running {
		for _, fork := range self.forks {
			if self.preflight && self.rt.skipPreflight {
				fork.skip()
			} else {
				fork.step()
			}
		}
	}
	previousState := self.state
	self.state = self.getState()
	switch self.state {
	case Failed:
		self.addFrontierNode(self)
	case Running:
		if self.state != previousState {
			self.mkdirs()
		}
		self.addFrontierNode(self)
	case Complete:
		if self.rt.vdrMode == "rolling" {
			for _, node := range self.prenodes {
				node.getNode().vdrKill()
				node.getNode().cachePerf()
			}
			self.vdrKill()
			self.cachePerf()
		}
		for _, node := range self.postnodes {
			self.addFrontierNode(node)
		}
		self.removeFrontierNode(self)
	case "waiting":
		self.removeFrontierNode(self)
	}
	return self.state != previousState
}

// Regular expression to convert a fully qualified name for a chunk into the
// component parts of the pipeline path.  The parts are:
// 1. The fully qualified stage name.
// 2. The fork index.
// 3. The chunk index, if any.
// 4. The job uniquifier, if any.
// 5. The metadata file name.
var jobJournalRe = regexp.MustCompile(`(.*)\.fork(\d+)(?:\.chnk(\d+))?(?:\.u([a-f0-9]{10}))?\.(.*)$`)

func (self *Node) parseRunFilename(fqname string) (string, int, int, string, string) {
	if match := jobJournalRe.FindStringSubmatch(fqname); match != nil {
		forkIndex, _ := strconv.Atoi(match[2])
		chunkIndex := -1
		if match[3] != "" {
			chunkIndex, _ = strconv.Atoi(match[3])
		}
		return match[1], forkIndex, chunkIndex, match[4], match[5]
	}
	return "", -1, -1, "", ""
}

func (self *Node) refreshState(readOnly bool) {
	startTime := time.Now().Add(-self.rt.JobManager.queueCheckGrace())
	files, _ := filepath.Glob(path.Join(self.journalPath, "*"))
	updatedForks := make(map[*Fork]struct{})
	for _, file := range files {
		filename := path.Base(file)
		if strings.HasSuffix(filename, ".tmp") {
			continue
		}

		fqname, forkIndex, chunkIndex, uniquifier, state := self.parseRunFilename(filename)
		if node := self.find(fqname); node != nil {
			if fork := node.getFork(forkIndex); fork != nil {
				if chunkIndex >= 0 {
					if chunk := fork.getChunk(chunkIndex); chunk != nil {
						chunk.updateState(MetadataFileName(state), uniquifier)
					}
				} else {
					fork.updateState(state, uniquifier)
				}
				updatedForks[fork] = struct{}{}
			}
		}
		if !readOnly {
			os.Remove(file)
		}
	}
	for _, node := range self.getFrontierNodes() {
		for _, meta := range node.collectMetadatas() {
			meta.endRefresh(startTime)
		}
	}
	for fork, _ := range updatedForks {
		fork.printUpdateIfNeeded()
	}
}

//
// Serialization
//
func (self *Node) serializeState() *NodeInfo {
	sweepbindings := []*BindingInfo{}
	for _, sweepbinding := range self.sweepbindings {
		sweepbindings = append(sweepbindings, sweepbinding.serializeState(nil))
	}
	forks := []*ForkInfo{}
	for _, fork := range self.forks {
		forks = append(forks, fork.serializeState())
	}
	edges := make([]EdgeInfo, 0, len(self.directPrenodes))
	for _, prenode := range self.directPrenodes {
		edges = append(edges, EdgeInfo{
			From: prenode.getNode().fqname,
			To:   self.fqname,
		})
	}
	var err *NodeErrorInfo
	if self.state == Failed {
		fqname, _, summary, log, _, errpaths := self.getFatalError()
		errpath := ""
		if len(errpaths) > 0 {
			errpath = errpaths[0]
		}
		err = &NodeErrorInfo{
			FQname:  fqname,
			Path:    errpath,
			Summary: summary,
			Log:     log,
		}
	}
	return &NodeInfo{
		Name:          self.name,
		Fqname:        self.fqname,
		Type:          self.kind,
		Path:          self.path,
		State:         self.state,
		Metadata:      self.metadata.serializeState(),
		SweepBindings: sweepbindings,
		Forks:         forks,
		Edges:         edges,
		StagecodeLang: self.stagecodeLang,
		StagecodeCmd:  self.stagecodeCmd,
		Error:         err,
	}
}

func (self *Node) serializePerf() *NodePerfInfo {
	forks := []*ForkPerfInfo{}
	for _, fork := range self.forks {
		forkSer, _ := fork.serializePerf()
		forks = append(forks, forkSer)
	}
	return &NodePerfInfo{
		Name:   self.name,
		Fqname: self.fqname,
		Type:   self.kind,
		Forks:  forks,
	}
}

//=============================================================================
// Job Runners
//=============================================================================
func (self *Node) getJobReqs(jobDef *JobResources, stageType string) (int, int, string) {
	threads := 0
	memGB := 0
	special := ""

	// Get values passed from the stage code
	if jobDef != nil {
		threads = jobDef.Threads
		memGB = jobDef.MemGB
		special = jobDef.Special
	}

	// Override with job manager caps specified from commandline
	overrideThreads := self.rt.overrides.GetOverride(self,
		fmt.Sprintf("%s.threads", stageType),
		float64(threads))
	if overrideThreadsNum, ok := overrideThreads.(float64); ok {
		threads = int(overrideThreadsNum)
	} else {
		util.PrintInfo("runtime",
			"Invalid value for %s %s.threads: %v",
			self.fqname, stageType, overrideThreads)
	}

	overrideMem := self.rt.overrides.GetOverride(self,
		fmt.Sprintf("%s.mem_gb", stageType),
		float64(memGB))
	if overrideMemFloat, ok := overrideMem.(float64); ok {
		memGB = int(overrideMemFloat)
	} else {
		util.PrintInfo("runtime",
			"Invalid value for %s %s.mem_gb: %v",
			self.fqname, stageType, overrideMem)
	}

	if self.local {
		threads, memGB = self.rt.LocalJobManager.GetSystemReqs(threads, memGB)
	} else {
		threads, memGB = self.rt.JobManager.GetSystemReqs(threads, memGB)
	}

	// Return modified values
	return threads, memGB, special
}

func (self *Node) setJobReqs(jobDef *JobResources, stageType string) (int, int, string) {
	// Get values and possibly modify them
	threads, memGB, special := self.getJobReqs(jobDef, stageType)

	// Write modified values back
	if jobDef != nil {
		jobDef.Threads = threads
		jobDef.MemGB = memGB
	}

	return threads, memGB, special
}

func (self *Node) setSplitJobReqs() (int, int, string) {
	return self.setJobReqs(nil, STAGE_TYPE_SPLIT)
}

func (self *Node) setChunkJobReqs(jobDef *JobResources) (int, int, string) {
	return self.setJobReqs(jobDef, STAGE_TYPE_CHUNK)
}

func (self *Node) setJoinJobReqs(jobDef *JobResources) (int, int, string) {
	return self.setJobReqs(jobDef, STAGE_TYPE_JOIN)
}

func (self *Node) runSplit(fqname string, metadata *Metadata) {
	threads, memGB, special := self.setSplitJobReqs()
	self.runJob("split", fqname, metadata, threads, memGB, special)
}

func (self *Node) runJoin(fqname string, metadata *Metadata, threads int, memGB int, special string) {
	self.runJob("join", fqname, metadata, threads, memGB, special)
}

func (self *Node) runChunk(fqname string, metadata *Metadata, threads int, memGB int, special string) {
	self.runJob("main", fqname, metadata, threads, memGB, special)
}

func (self *Node) runJob(shellName string, fqname string, metadata *Metadata,
	threads int, memGB int, special string) {

	// Configure local variable dumping.
	stackVars := "disable"
	if self.rt.enableStackVars {
		stackVars = "stackvars"
	}

	// Configure memory monitoring.
	monitor := "disable"
	if self.rt.enableMonitor {
		monitor = "monitor"
	}

	// Construct path to the shell.
	shellCmd := ""
	var argv []string
	stagecodeParts := strings.Split(self.stagecodeCmd, " ")
	runFile := path.Join(self.journalPath, fqname)
	if metadata.uniquifier != "" {
		runFile += ".u" + metadata.uniquifier
	}
	version := &VersionInfo{
		Martian:   self.rt.martianVersion,
		Pipelines: self.mroVersion,
	}
	envs := self.envs

	switch self.stagecodeLang {
	case syntax.PythonStage:
		if len(stagecodeParts) != 1 {
			panic(fmt.Sprintf("Invalid python stage module specification \"%s\"", self.stagecodeCmd))
		}
		shellCmd = self.rt.mrjob
		argv = []string{
			path.Join(self.rt.adaptersPath, "python", "martian_shell.py"),
			stagecodeParts[0],
			shellName,
			metadata.path,
			metadata.curFilesPath,
			runFile,
		}
	case syntax.CompiledStage:
		shellCmd = self.rt.mrjob
		argv = append(stagecodeParts, shellName, metadata.path, metadata.curFilesPath, runFile)
	case syntax.ExecStage:
		shellCmd = stagecodeParts[0]
		argv = append(stagecodeParts[1:], shellName, metadata.path, metadata.curFilesPath, runFile)
	default:
		panic(fmt.Sprintf("Unknown stage code language: %v", self.stagecodeLang))
	}

	// Log the job run.
	jobMode := self.rt.jobMode
	jobManager := self.rt.JobManager
	if self.local {
		jobMode = "local"
		jobManager = self.rt.LocalJobManager
	}
	jobModeLabel := strings.Replace(jobMode, ".template", "", -1)
	padding := strings.Repeat(" ", int(math.Max(0, float64(10-len(path.Base(jobModeLabel))))))
	msg := fmt.Sprintf("(run:%s) %s %s.%s", path.Base(jobModeLabel), padding, fqname, shellName)
	if self.preflight {
		util.LogInfo("runtime", msg)
	} else {
		util.PrintInfo("runtime", msg)
	}

	func() {
		util.EnterCriticalSection()
		defer util.ExitCriticalSection()
		metadata.WriteTime(QueuedLocally)
		metadata.Write(JobInfoFile, &JobInfo{
			Name:        fqname,
			Type:        jobMode,
			Threads:     threads,
			MemGB:       memGB,
			ProfileMode: self.rt.profileMode,
			Stackvars:   stackVars,
			Monitor:     monitor,
			Invocation:  self.invocation,
			Version:     version,
		})
	}()
	jobManager.execJob(shellCmd, argv, envs, metadata, threads, memGB, special, fqname,
		shellName, self.preflight && self.local)
}
