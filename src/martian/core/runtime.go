//
// Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
//
// Martian runtime. This is where the action happens.
//
package core

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//=============================================================================
// Metadata
//=============================================================================
type Metadata struct {
	fqname    string
	path      string
	contents  map[string]bool
	filesPath string
	mutex     *sync.Mutex
}

func NewMetadata(fqname string, p string) *Metadata {
	self := &Metadata{}
	self.fqname = fqname
	self.path = p
	self.contents = map[string]bool{}
	self.filesPath = path.Join(p, "files")
	self.mutex = &sync.Mutex{}
	return self
}

func (self *Metadata) glob() []string {
	paths, _ := filepath.Glob(path.Join(self.path, "_*"))
	return paths
}

func (self *Metadata) enumerateFiles() ([]string, error) {
	return filepath.Glob(path.Join(self.filesPath, "*"))
}

func (self *Metadata) mkdirs() {
	// When making/remaking dirs, clear the cache.
	self.mutex.Lock()
	self.contents = map[string]bool{}
	self.mutex.Unlock()
	mkdir(self.path)
	mkdir(self.filesPath)
}

func (self *Metadata) idemMkdirs() {
	// When making/remaking dirs, clear the cache.
	self.mutex.Lock()
	self.contents = map[string]bool{}
	self.mutex.Unlock()
	idemMkdir(self.path)
	idemMkdir(self.filesPath)
}

func (self *Metadata) getState(name string) (string, bool) {
	if self.exists("errors") {
		return "failed", true
	}
	if self.exists("assert") {
		return "failed", true
	}
	if self.exists("complete") {
		return name + "complete", true
	}
	if self.exists("log") {
		return name + "running", true
	}
	if self.exists("jobinfo") {
		return name + "queued", true
	}
	return "", false
}

func (self *Metadata) cache(name string) {
	self.mutex.Lock()
	self.contents[name] = true
	self.mutex.Unlock()
}

func (self *Metadata) uncache(name string) {
	self.mutex.Lock()
	delete(self.contents, name)
	self.mutex.Unlock()
}

func (self *Metadata) loadCache() {
	if !self.exists("complete") {
		paths := self.glob()
		self.mutex.Lock()
		self.contents = map[string]bool{}
		for _, p := range paths {
			self.contents[path.Base(p)[1:]] = true
		}
		self.mutex.Unlock()
	}
}

func (self *Metadata) makePath(name string) string {
	return path.Join(self.path, "_"+name)
}
func (self *Metadata) exists(name string) bool {
	self.mutex.Lock()
	_, ok := self.contents[name]
	self.mutex.Unlock()
	return ok
}
func (self *Metadata) readRaw(name string) string {
	bytes, _ := ioutil.ReadFile(self.makePath(name))
	return string(bytes)
}
func (self *Metadata) read(name string) interface{} {
	var v interface{}
	json.Unmarshal([]byte(self.readRaw(name)), &v)
	return v
}
func (self *Metadata) writeRaw(name string, text string) {
	ioutil.WriteFile(self.makePath(name), []byte(text), 0644)
	self.cache(name)
}
func (self *Metadata) write(name string, object interface{}) {
	bytes, _ := json.MarshalIndent(object, "", "    ")
	self.writeRaw(name, string(bytes))
}
func (self *Metadata) writeTime(name string) {
	self.writeRaw(name, Timestamp())
}
func (self *Metadata) remove(name string) { os.Remove(self.makePath(name)) }

func (self *Metadata) serialize() interface{} {
	names := []string{}
	self.mutex.Lock()
	for content, _ := range self.contents {
		names = append(names, content)
	}
	self.mutex.Unlock()
	sort.Strings(names)
	return map[string]interface{}{
		"path":  self.path,
		"names": names,
	}
}

//=============================================================================
// Binding
//=============================================================================
type Binding struct {
	node       *Node
	id         string
	tname      string
	sweep      bool
	waiting    bool
	valexp     string
	mode       string
	parentNode Nodable
	boundNode  Nodable
	output     string
	value      interface{}
}

func newBinding(node *Node, bindStm *BindStm, returnBinding bool) *Binding {
	self := &Binding{}
	self.node = node
	self.id = bindStm.id
	self.tname = bindStm.tname
	self.sweep = bindStm.sweep
	self.waiting = false
	switch valueExp := bindStm.exp.(type) {
	case *RefExp:
		if valueExp.kind == "self" {
			var parentBinding *Binding
			if returnBinding {
				parentBinding = self.node.argbindings[valueExp.id]
			} else {
				parentBinding = self.node.parent.getNode().argbindings[valueExp.id]
			}
			if parentBinding != nil {
				self.node = parentBinding.node
				self.tname = parentBinding.tname
				self.sweep = parentBinding.sweep
				self.waiting = parentBinding.waiting
				self.mode = parentBinding.mode
				self.parentNode = parentBinding.parentNode
				self.boundNode = parentBinding.boundNode
				self.output = parentBinding.output
				self.value = parentBinding.value
			}
			self.id = bindStm.id
			self.valexp = "self." + valueExp.id
		} else if valueExp.kind == "call" {
			if returnBinding {
				self.parentNode = self.node.subnodes[valueExp.id]
				self.boundNode, self.output, self.mode, self.value = self.node.findBoundNode(
					valueExp.id, valueExp.outputId, "reference", nil)
			} else {
				self.parentNode = self.node.parent.getNode().subnodes[valueExp.id]
				self.boundNode, self.output, self.mode, self.value = self.node.parent.getNode().findBoundNode(
					valueExp.id, valueExp.outputId, "reference", nil)
			}
			if valueExp.outputId == "default" {
				self.valexp = valueExp.id
			} else {
				self.valexp = valueExp.id + "." + valueExp.outputId
			}
		}
	case *ValExp:
		self.mode = "value"
		self.parentNode = node
		self.boundNode = node
		self.value = expToInterface(bindStm.exp)
	}
	return self
}

func NewBinding(node *Node, bindStm *BindStm) *Binding {
	return newBinding(node, bindStm, false)
}

func NewReturnBinding(node *Node, bindStm *BindStm) *Binding {
	return newBinding(node, bindStm, true)
}

func expToInterface(exp Exp) interface{} {
	// Convert tree of Exps into a tree of interface{}s.
	valExp, ok := exp.(*ValExp)
	if !ok {
		return nil
	}
	if valExp.kind == "array" {
		varray := []interface{}{}
		for _, exp := range valExp.value.([]Exp) {
			varray = append(varray, expToInterface(exp))
		}
		return varray
	} else if valExp.kind == "map" {
		vmap := map[string]interface{}{}
		for k, exp := range valExp.value.(map[string]Exp) {
			vmap[k] = expToInterface(exp)
		}
		return vmap
	} else {
		return valExp.value
	}
}

func (self *Binding) resolve(argPermute map[string]interface{}) interface{} {
	self.waiting = false
	if self.mode == "value" {
		if argPermute == nil {
			// In this case we want to get the raw value, which might be a sweep array.
			return self.value
		}
		// Replace literal sweep ranges with specific permuted argument values.
		if self.sweep {
			return argPermute[self.id]
		} else {
			return self.value
		}
	}
	if argPermute == nil {
		return nil
	}
	if self.boundNode != nil {
		matchedFork := self.boundNode.getNode().matchFork(argPermute)
		outputs, ok := matchedFork.metadata.read("outs").(map[string]interface{})
		if ok {
			output, ok := outputs[self.output]
			if ok {
				return output
			}
		}
	}
	self.waiting = true
	return nil
}

func (self *Binding) serialize(argPermute map[string]interface{}) interface{} {
	var node interface{} = nil
	var matchedFork interface{} = nil
	if self.boundNode != nil {
		node = self.boundNode.getNode().name
		f := self.boundNode.getNode().matchFork(argPermute)
		if f != nil {
			matchedFork = f.index
		}
	}
	return map[string]interface{}{
		"id":          self.id,
		"type":        self.tname,
		"valexp":      self.valexp,
		"mode":        self.mode,
		"output":      self.output,
		"sweep":       self.sweep,
		"node":        node,
		"matchedFork": matchedFork,
		"value":       self.resolve(argPermute),
		"waiting":     self.waiting,
	}
}

// Helpers
func resolveBindings(bindings map[string]*Binding, argPermute map[string]interface{}) map[string]interface{} {
	resolvedBindings := map[string]interface{}{}
	for id, binding := range bindings {
		resolvedBindings[id] = binding.resolve(argPermute)
	}
	return resolvedBindings
}

func makeOutArgs(outParams *Params, filesPath string) map[string]interface{} {
	args := map[string]interface{}{}
	for id, param := range outParams.table {
		if param.getIsFile() {
			args[id] = path.Join(filesPath, param.getId()+"."+param.getTname())
		} else if param.getTname() == "path" {
			args[id] = path.Join(filesPath, param.getId())
		} else {
			args[id] = nil
		}
	}
	return args
}

func dynamicCast(val interface{}, typename string, arrayDim int) bool {
	ret := true
	if arrayDim > 0 {
		arr, ok := val.([]interface{})
		if !ok {
			return false
		}
		for _, v := range arr {
			ret = ret && dynamicCast(v, typename, arrayDim-1)
		}
	} else {
		switch typename {
		case "path":
			fallthrough
		case "file":
			fallthrough
		case "string":
			_, ret = val.(string)
		case "float":
			_, ret = val.(float64)
		case "int":
			var num float64
			num, ret = val.(float64)
			if ret {
				ret = num == math.Trunc(num)
			}
		case "bool":
			_, ret = val.(bool)
		case "map":
			_, ret = val.(map[string]interface{})
		}
	}
	return ret
}

//=============================================================================
// Chunk
//=============================================================================
type Chunk struct {
	node       *Node
	fork       *Fork
	index      int
	chunkDef   map[string]interface{}
	path       string
	fqname     string
	metadata   *Metadata
	hasBeenRun bool
}

func NewChunk(nodable Nodable, fork *Fork, index int, chunkDef map[string]interface{}) *Chunk {
	self := &Chunk{}
	self.node = nodable.getNode()
	self.fork = fork
	self.index = index
	self.chunkDef = chunkDef
	self.path = path.Join(fork.path, fmt.Sprintf("chnk%d", index))
	self.fqname = fork.fqname + fmt.Sprintf(".chnk%d", index)
	self.metadata = NewMetadata(self.fqname, self.path)
	self.hasBeenRun = false
	if !self.node.split {
		// If we're not splitting, just set the sole chunk's filesPath
		// to the filesPath of the parent fork, to save a pseudo-join copy.
		self.metadata.filesPath = self.fork.metadata.filesPath
	}
	// We have to mkdirs here because runtime might have been interrupted after chunk_defs were
	// written but before next step interval caused the actual creation of the chnk folders.
	// in that scenario, upon restart the fork step would try to write _args into chnk folders
	// that don't exist.
	// This also gets run if we are restarting from a failed stage.
	self.mkdirs()
	return self
}

func (self *Chunk) mkdirs() {
	self.metadata.idemMkdirs()
}

func (self *Chunk) getState() string {
	if state, ok := self.metadata.getState(""); ok {
		return state
	} else {
		return "ready"
	}
}

func (self *Chunk) updateState(state string) {
	self.metadata.cache(state)
}

func (self *Chunk) step() {
	if self.getState() != "ready" {
		return
	}

	// Belt and suspenders for not double-submitting a job.
	if self.hasBeenRun {
		return
	} else {
		self.hasBeenRun = true
	}

	//
	// Process __threads and __mem_gb requested by stage split.
	//
	// __threads tells job manager how much concurrency this chunk wants.
	// __mem_gb  tells SGE to kill-if-exceed. For local mode, it is
	//           instead a consumption request like __threads.

	// A chunk consumes 1 thread unless stage split explicitly asks for more.
	threads := 1
	if v, ok := self.chunkDef["__threads"].(float64); ok {
		threads = int(v)

		// In local mode, cap to the job manager's max cores.
		// It is not sufficient for the job manager to do the capping downstream.
		// We rewrite the chunkDef here to inform the chunk it should use less
		// concurrency.
		if self.node.rt.jobMode == "local" {
			maxCores := self.node.rt.JobManager.GetMaxCores()
			if threads > maxCores {
				threads = maxCores
			}
			self.chunkDef["__threads"] = threads
		}
	}

	// Default to -1 to impose no limit (no flag will be passed to SGE).
	// The local mode job manager will convert -1 to 1 downstream.
	memGB := -1
	if v, ok := self.chunkDef["__mem_gb"].(float64); ok {
		memGB = int(v)

		if self.node.rt.jobMode == "local" {
			maxMemGB := self.node.rt.JobManager.GetMaxMemGB()
			if memGB > maxMemGB {
				memGB = maxMemGB
			}
			self.chunkDef["__mem_gb"] = memGB
		}
	}

	// Resolve input argument bindings and merge in the chunk defs.
	resolvedBindings := resolveBindings(self.node.argbindings, self.fork.argPermute)
	for id, value := range self.chunkDef {
		resolvedBindings[id] = value
	}

	// Write out input and ouput args for the chunk.
	self.metadata.write("args", resolvedBindings)
	self.metadata.write("outs", makeOutArgs(self.node.outparams, self.metadata.filesPath))

	// Run the chunk.
	self.node.runChunk(self.fqname, self.metadata, threads, memGB)
}

func (self *Chunk) serialize() interface{} {
	return map[string]interface{}{
		"index":    self.index,
		"chunkDef": self.chunkDef,
		"state":    self.getState(),
		"metadata": self.metadata.serialize(),
	}
}

//=============================================================================
// Fork
//=============================================================================
type Fork struct {
	node           *Node
	index          int
	path           string
	fqname         string
	metadata       *Metadata
	split_metadata *Metadata
	join_metadata  *Metadata
	chunks         []*Chunk
	split_has_run  bool
	join_has_run   bool
	argPermute     map[string]interface{}
}

func NewFork(nodable Nodable, index int, argPermute map[string]interface{}) *Fork {
	self := &Fork{}
	self.node = nodable.getNode()
	self.index = index
	self.path = path.Join(self.node.path, fmt.Sprintf("fork%d", index))
	self.fqname = self.node.fqname + fmt.Sprintf(".fork%d", index)
	self.metadata = NewMetadata(self.fqname, self.path)
	self.split_metadata = NewMetadata(self.fqname+".split", path.Join(self.path, "split"))
	self.join_metadata = NewMetadata(self.fqname+".join", path.Join(self.path, "join"))
	self.argPermute = argPermute
	self.split_has_run = false
	self.join_has_run = false
	// reconstruct chunks using chunk_defs on reattach, do not rely
	// on metadata.exists('chunk_defs') since it may not be cached
	self.chunks = []*Chunk{}
	chunkDefIfaces := self.split_metadata.read("chunk_defs")
	if chunkDefs, ok := chunkDefIfaces.([]interface{}); ok {
		for i, chunkDef := range chunkDefs {
			chunk := NewChunk(self.node, self, i, chunkDef.(map[string]interface{}))
			self.chunks = append(self.chunks, chunk)
		}
	}
	return self
}

func (self *Fork) clearChunks() {
	self.chunks = []*Chunk{}
}

func (self *Fork) collectMetadatas() []*Metadata {
	metadatas := []*Metadata{self.metadata, self.split_metadata, self.join_metadata}
	for _, chunk := range self.chunks {
		metadatas = append(metadatas, chunk.metadata)
	}
	return metadatas
}

func (self *Fork) mkdirs() {
	self.metadata.mkdirs()
	self.split_metadata.mkdirs()
	self.join_metadata.mkdirs()
	self.split_has_run = false
}

func (self *Fork) verifyOutput() (bool, string) {
	outparams := self.node.outparams
	msg := ""
	ret := true
	if len(outparams.list) > 0 {
		outputs := self.metadata.read("outs").(map[string]interface{})
		for _, param := range outparams.table {
			val, ok := outputs[param.getId()]
			if !ok {
				msg += fmt.Sprintf("Fork did not return parameter '%s'\n", param.getId())
				ret = false
				continue
			}
			if val == nil {
				// Allow for null output parameters
				continue
			}
			if !dynamicCast(val, param.getTname(), param.getArrayDim()) {
				msg += fmt.Sprintf("Fork returned %s parameter '%s' with incorrect type\n", param.getTname(), param.getId())
				ret = false
			}
		}
	}
	return ret, msg
}

func (self *Fork) getState() string {
	if state, _ := self.metadata.getState(""); state == "failed" {
		return "failed"
	}
	if state, _ := self.metadata.getState(""); state == "complete" {
		return "complete"
	}
	if state, ok := self.join_metadata.getState("join_"); ok {
		return state
	}
	if len(self.chunks) > 0 {
		// If any chunks have failed, we're failed.
		for _, chunk := range self.chunks {
			if chunk.getState() == "failed" {
				return "failed"
			}
		}
		// If every chunk is complete, we're complete.
		every := true
		for _, chunk := range self.chunks {
			if chunk.getState() != "complete" {
				every = false
				break
			}
		}
		if every {
			return "chunks_complete"
		}
		// If every chunk is queued, running, or complete, we're complete.
		every = true
		runningStates := map[string]bool{"queued": true, "running": true, "complete": true}
		for _, chunk := range self.chunks {
			if _, ok := runningStates[chunk.getState()]; !ok {
				every = false
				break
			}
		}
		if every {
			return "chunks_running"
		}
	}
	if state, ok := self.split_metadata.getState("split_"); ok {
		return state
	}
	return "ready"
}

func (self *Fork) updateState(state string) {
	if strings.HasPrefix(state, "split_") {
		self.split_metadata.cache(strings.TrimPrefix(state, "split_"))
	} else if strings.HasPrefix(state, "join_") {
		self.join_metadata.cache(strings.TrimPrefix(state, "join_"))
	} else {
		self.metadata.cache(state)
	}
}

func (self *Fork) getChunk(index int) *Chunk {
	if index < len(self.chunks) {
		return self.chunks[index]
	}
	return nil
}

func (self *Fork) step() {
	if self.node.kind == "stage" {
		state := self.getState()
		if !strings.HasSuffix(state, "_running") && !strings.HasSuffix(state, "_queued") {
			statePad := strings.Repeat(" ", int(math.Max(0, float64(15-len(state)))))
			msg := fmt.Sprintf("(%s)%s %s", state, statePad, self.node.fqname)
			if self.node.preflight {
				LogInfo("runtime", msg)
			} else {
				PrintInfo("runtime", msg)
			}
		}

		if state == "ready" {
			self.split_metadata.write("args", resolveBindings(self.node.argbindings, self.argPermute))
			if self.node.split {
				if !self.split_has_run {
					self.split_has_run = true
					// Default memory to -1 for no limit.
					self.node.runSplit(self.fqname, self.split_metadata)
				}
			} else {
				self.split_metadata.write("chunk_defs", []interface{}{map[string]interface{}{}})
				self.split_metadata.writeTime("complete")
			}
		} else if state == "split_complete" {
			chunkDefs := self.split_metadata.read("chunk_defs")
			if _, ok := chunkDefs.([]interface{}); !ok {
				self.split_metadata.idemMkdirs()
				self.split_metadata.writeRaw("errors", "The split method must return an array of chunk def dicts but did not.\n")
			} else {
				if len(self.chunks) == 0 {
					for i, chunkDef := range chunkDefs.([]interface{}) {
						if _, ok := chunkDef.(map[string]interface{}); !ok {
							self.split_metadata.idemMkdirs()
							self.split_metadata.writeRaw("errors", "The split method must return an array of chunk def dicts but did not.\n")
							break
						}
						chunk := NewChunk(self.node, self, i, chunkDef.(map[string]interface{}))
						self.chunks = append(self.chunks, chunk)
						chunk.mkdirs()
					}
				}
				for _, chunk := range self.chunks {
					chunk.step()
				}
			}
		} else if state == "chunks_complete" {
			self.join_metadata.write("args", resolveBindings(self.node.argbindings, self.argPermute))
			self.join_metadata.write("chunk_defs", self.split_metadata.read("chunk_defs"))
			if self.node.split {
				chunkOuts := []interface{}{}
				for _, chunk := range self.chunks {
					outs := chunk.metadata.read("outs")
					chunkOuts = append(chunkOuts, outs)
				}
				self.join_metadata.write("chunk_outs", chunkOuts)
				self.join_metadata.write("outs", makeOutArgs(self.node.outparams, self.metadata.filesPath))
				if !self.join_has_run {
					self.join_has_run = true
					self.node.runJoin(self.fqname, self.join_metadata)
				}
			} else {
				self.join_metadata.write("outs", self.chunks[0].metadata.read("outs"))
				self.join_metadata.writeTime("complete")
			}
		} else if state == "join_complete" {
			self.metadata.write("outs", self.join_metadata.read("outs"))
			if ok, msg := self.verifyOutput(); ok {
				self.metadata.writeTime("complete")
			} else {
				self.metadata.writeRaw("errors", msg)
			}
		}

	} else if self.node.kind == "pipeline" {
		self.metadata.write("outs", resolveBindings(self.node.retbindings, self.argPermute))
		if ok, msg := self.verifyOutput(); ok {
			self.metadata.writeTime("complete")
		} else {
			self.metadata.writeRaw("errors", msg)
		}
	}
}

func (self *Fork) serialize() interface{} {
	argbindings := []interface{}{}
	for _, argbinding := range self.node.argbindingList {
		argbindings = append(argbindings, argbinding.serialize(self.argPermute))
	}
	retbindings := []interface{}{}
	for _, retbinding := range self.node.retbindingList {
		retbindings = append(retbindings, retbinding.serialize(self.argPermute))
	}
	bindings := map[string]interface{}{
		"Argument": argbindings,
		"Return":   retbindings,
	}
	chunks := []interface{}{}
	for _, chunk := range self.chunks {
		chunks = append(chunks, chunk.serialize())
	}
	return map[string]interface{}{
		"index":          self.index,
		"argPermute":     self.argPermute,
		"state":          self.getState(),
		"metadata":       self.metadata.serialize(),
		"split_metadata": self.split_metadata.serialize(),
		"join_metadata":  self.join_metadata.serialize(),
		"chunks":         chunks,
		"bindings":       bindings,
	}
}

//=============================================================================
// Node
//=============================================================================
type Nodable interface {
	getNode() *Node
}

type Node struct {
	parent         Nodable
	rt             *Runtime
	kind           string
	name           string
	fqname         string
	path           string
	metadata       *Metadata
	outparams      *Params
	argbindings    map[string]*Binding
	argbindingList []*Binding // for stable ordering
	retbindings    map[string]*Binding
	retbindingList []*Binding // for stable ordering
	sweepbindings  []*Binding
	subnodes       map[string]Nodable
	prenodes       map[string]Nodable
	directPrenodes []Nodable
	postnodes      map[string]Nodable
	frontierNodes  map[string]Nodable
	forks          []*Fork
	split          bool
	state          string
	volatile       bool
	local          bool
	preflight      bool
	stagecodeLang  string
	stagecodeCmd   string
	journalPath    string
	tmpPath        string
}

func (self *Node) getNode() *Node { return self }

func NewNode(parent Nodable, kind string, callStm *CallStm, callables *Callables) *Node {
	self := &Node{}
	self.parent = parent

	self.rt = parent.getNode().rt
	self.kind = kind
	self.name = callStm.id
	self.fqname = parent.getNode().fqname + "." + self.name
	self.path = path.Join(parent.getNode().path, self.name)
	self.journalPath = parent.getNode().journalPath
	self.tmpPath = parent.getNode().tmpPath
	self.metadata = NewMetadata(self.fqname, self.path)
	self.volatile = callStm.modifiers.volatile
	self.local = callStm.modifiers.local
	self.preflight = callStm.modifiers.preflight

	self.outparams = callables.table[self.name].getOutParams()
	self.argbindings = map[string]*Binding{}
	self.argbindingList = []*Binding{}
	self.retbindings = map[string]*Binding{}
	self.retbindingList = []*Binding{}
	self.subnodes = map[string]Nodable{}
	self.prenodes = map[string]Nodable{}
	self.directPrenodes = []Nodable{}
	self.postnodes = map[string]Nodable{}
	self.frontierNodes = parent.getNode().frontierNodes

	for id, bindStm := range callStm.bindings.table {
		binding := NewBinding(self, bindStm)
		self.argbindings[id] = binding
		self.argbindingList = append(self.argbindingList, binding)
	}
	for _, binding := range self.argbindingList {
		if binding.mode == "reference" && binding.boundNode != nil {
			prenode := binding.boundNode
			self.prenodes[prenode.getNode().fqname] = prenode
			self.directPrenodes = append(self.directPrenodes, binding.parentNode)

			prenode.getNode().postnodes[self.fqname] = self
		}
	}
	// Do not set state = getState here, or else nodes will wrongly report
	// complete before the first refreshMetadata call
	return self
}

//
// Folder construction
//
func (self *Node) mkdirs(wg *sync.WaitGroup) {
	mkdir(self.path)
	idemMkdir(self.journalPath)
	idemMkdir(self.tmpPath)
	for _, fork := range self.forks {
		wg.Add(1)
		go func(f *Fork) {
			f.mkdirs()
			wg.Done()
		}(fork)
	}
	for _, subnode := range self.subnodes {
		wg.Add(1)
		go func(n Nodable) {
			n.getNode().mkdirs(wg)
			wg.Done()
		}(subnode)
	}
}

//
// Sweep management
//
func (self *Node) buildForks(bindings map[string]*Binding) {
	// Use a map to uniquify bindings by id.
	bindingTable := map[string]*Binding{}

	// Add local sweep bindings.
	for _, binding := range bindings {
		if binding.sweep {
			bindingTable[binding.id] = binding
		}
	}
	// Add upstream sweep bindings (from prenodes).
	for _, prenode := range self.prenodes {
		for _, binding := range prenode.getNode().sweepbindings {
			bindingTable[binding.id] = binding
		}
	}

	for _, binding := range bindingTable {
		self.sweepbindings = append(self.sweepbindings, binding)
	}

	// Add all unique bindings to self.sweepbindings.
	paramIds := []string{}
	argRanges := []interface{}{}
	for _, binding := range self.sweepbindings {
		//  self.sweepbindings = append(self.sweepbindings, binding)
		paramIds = append(paramIds, binding.id)
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
}

func (self *Node) matchFork(targetArgPermute map[string]interface{}) *Fork {
	if targetArgPermute == nil {
		return nil
	}
	for _, fork := range self.forks {
		every := true
		for paramId, argValue := range fork.argPermute {
			if targetArgPermute[paramId] != argValue {
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
	self.frontierNodes[node.getNode().fqname] = node
}

func (self *Node) removeFrontierNode(node Nodable) {
	delete(self.frontierNodes, node.getNode().fqname)
}

func (self *Node) getFrontierNodes() []*Node {
	frontierNodes := []*Node{}
	for _, node := range self.frontierNodes {
		frontierNodes = append(frontierNodes, node.getNode())
	}
	return frontierNodes
}

func (self *Node) allNodes() []*Node {
	all := []*Node{self}

	// Enumerate and sort the keys in subnodes first.
	// This ensures a stable chirality for the dag UI.
	ids := []string{}
	for id, _ := range self.subnodes {
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
		metadata.loadCache()
	}
	self.state = self.getState()
	self.addFrontierNode(self)
}

func (self *Node) getFork(index int) *Fork {
	if index < len(self.forks) {
		return self.forks[index]
	}
	return nil
}

func (self *Node) getState() string {
	// If every fork is complete, we're complete.
	complete := true
	for _, fork := range self.forks {
		if fork.getState() != "complete" {
			complete = false
			break
		}
	}
	if complete {
		return "complete"
	}
	// If any fork is failed, we're failed.
	for _, fork := range self.forks {
		if fork.getState() == "failed" {
			return "failed"
		}
	}
	// If any prenode is not complete, we're waiting.
	for _, prenode := range self.prenodes {
		if prenode.getNode().getState() != "complete" {
			return "waiting"
		}
	}
	// Otherwise we're running.
	return "running"
}

func (self *Node) reset() error {
	PrintInfo("runtime", "(reset)           %s", self.fqname)

	// Blow away the entire stage node.
	if err := os.RemoveAll(self.path); err != nil {
		PrintInfo("runtime", "mrp cannot reset the stage because its folder contents could not be deleted. Error was:\n\n%s\n\nPlease resolve the error in order to continue running the pipeline.", err.Error())
		return err
	}
	// Remove all files from journal and tmp directories.
	os.RemoveAll(self.journalPath)
	os.RemoveAll(self.tmpPath)

	// Re-create the folders.
	// This will also clear all the metadata in-memory caches.
	var rewg sync.WaitGroup
	self.mkdirs(&rewg)
	rewg.Wait()

	// Load the metadata.
	self.loadMetadata()

	// Clear chunks in the forks so they can be rebuilt on split.
	for _, fork := range self.forks {
		fork.clearChunks()
	}
	return nil
}

func (self *Node) resetJobMonitors() {
	for _, metadata := range self.collectMetadatas() {
		state, _ := metadata.getState("")
		if state == "running" || state == "queued" {
			self.rt.JobManager.MonitorJob(metadata)
		}
	}
}

func (self *Node) cleanup() {
	os.RemoveAll(self.journalPath)
	os.RemoveAll(self.tmpPath)
}

func (self *Node) getWarnings() (string, bool) {
	warnings := ""
	isWarnings := false
	for _, metadata := range self.collectMetadatas() {
		if !metadata.exists("warn") {
			continue
		}
		warnings += metadata.readRaw("warn")
		isWarnings = true
	}
	return warnings, isWarnings
}

func (self *Node) getFatalError() (string, string, string, string, []string) {
	for _, metadata := range self.collectMetadatas() {
		if state, _ := metadata.getState(""); state != "failed" {
			continue
		}
		if metadata.exists("errors") {
			errlog := metadata.readRaw("errors")
			summary := "<none>"
			if self.stagecodeLang == "Python" {
				errlines := strings.Split(errlog, "\n")
				if len(errlines) >= 1 {
					summary = errlines[len(errlines)-1]
				}
			}
			errpaths := []string{
				metadata.makePath("errors"),
				metadata.makePath("stdout"),
				metadata.makePath("stderr"),
			}
			if self.rt.enableLocalVars {
				errpaths = append(errpaths, metadata.makePath("localvars"))
			}
			return metadata.fqname, summary, errlog, "errors", errpaths
		}
		if metadata.exists("assert") {
			assertlog := metadata.readRaw("assert")
			summary := "<none>"
			assertlines := strings.Split(assertlog, "\n")
			if len(assertlines) >= 1 {
				summary = assertlines[len(assertlines)-1]
			}
			return metadata.fqname, summary, assertlog, "assert", []string{
				metadata.makePath("assert"),
			}
		}
	}
	return "", "", "", "", []string{}
}

func (self *Node) step() {
	if self.state == "running" {
		for _, fork := range self.forks {
			fork.step()
		}
	}
	self.state = self.getState()
	switch self.state {
	case "failed":
		self.addFrontierNode(self)
	case "running":
		self.addFrontierNode(self)
	case "complete":
		for _, node := range self.postnodes {
			self.addFrontierNode(node)
		}
		self.removeFrontierNode(self)
	case "waiting":
		self.removeFrontierNode(self)
	}
}

func (self *Node) parseRunFilename(fqname string) (string, int, int, string) {
	r := regexp.MustCompile("(.*)\\.fork(\\d+)\\.chnk(\\d+)\\.(.*)$")
	if match := r.FindStringSubmatch(fqname); match != nil {
		forkIndex, _ := strconv.Atoi(match[2])
		chunkIndex, _ := strconv.Atoi(match[3])
		return match[1], forkIndex, chunkIndex, match[4]
	}
	r = regexp.MustCompile("(.*)\\.fork(\\d+)\\.(.*)$")
	if match := r.FindStringSubmatch(fqname); match != nil {
		forkIndex, _ := strconv.Atoi(match[2])
		return match[1], forkIndex, -1, match[3]
	}
	return "", -1, -1, ""
}

func (self *Node) refreshState() {
	files, _ := filepath.Glob(path.Join(self.journalPath, "*"))
	for _, file := range files {
		filename := path.Base(file)
		if strings.HasSuffix(filename, ".tmp") {
			continue
		}

		fqname, forkIndex, chunkIndex, state := self.parseRunFilename(filename)
		if node := self.find(fqname); node != nil {
			if fork := node.getFork(forkIndex); fork != nil {
				if chunkIndex >= 0 {
					if chunk := fork.getChunk(chunkIndex); chunk != nil {
						chunk.updateState(state)
					}
				} else {
					fork.updateState(state)
				}
			}
		}
		os.Remove(file)
	}
}

//
// Serialization
//
func (self *Node) serialize() interface{} {
	sweepbindings := []interface{}{}
	for _, sweepbinding := range self.sweepbindings {
		sweepbindings = append(sweepbindings, sweepbinding.serialize(nil))
	}
	forks := []interface{}{}
	for _, fork := range self.forks {
		forks = append(forks, fork.serialize())
	}
	edges := []interface{}{}
	for _, prenode := range self.directPrenodes {
		edges = append(edges, map[string]string{
			"from": prenode.getNode().fqname,
			"to":   self.fqname,
		})
	}
	var err interface{} = nil
	if self.state == "failed" {
		fqname, summary, log, _, errpaths := self.getFatalError()
		errpath := ""
		if len(errpaths) > 0 {
			errpath = errpaths[0]
		}
		err = map[string]string{
			"fqname":  fqname,
			"path":    errpath,
			"summary": summary,
			"log":     log,
		}
	}
	return map[string]interface{}{
		"name":          self.name,
		"fqname":        self.fqname,
		"type":          self.kind,
		"path":          self.path,
		"state":         self.state,
		"metadata":      self.metadata.serialize(),
		"sweepbindings": sweepbindings,
		"forks":         forks,
		"edges":         edges,
		"stagecodeLang": self.stagecodeLang,
		"stagecodeCmd":  self.stagecodeCmd,
		"error":         err,
	}
}

//=============================================================================
// Job Runners
//=============================================================================
func (self *Node) runSplit(fqname string, metadata *Metadata) {
	self.runJob("split", fqname, metadata, 1, -1)
}

func (self *Node) runJoin(fqname string, metadata *Metadata) {
	self.runJob("join", fqname, metadata, 1, -1)
}

func (self *Node) runChunk(fqname string, metadata *Metadata, threads int, memGB int) {
	self.runJob("main", fqname, metadata, threads, memGB)
}

func (self *Node) runJob(shellName string, fqname string, metadata *Metadata,
	threads int, memGB int) {

	// Configure profiling.
	profile := "disable"
	if self.rt.enableProfiling {
		profile = "profile"
	}

	// Configure local variable dumping.
	localVars := "disable"
	if self.rt.enableLocalVars {
		localVars = "localvars"
	}

	// Set environment variables
	os.Setenv("TMPDIR", self.tmpPath)
	envs := []string{fmt.Sprintf("TMPDIR=%s", self.tmpPath)}

	// Construct path to the shell.
	shellCmd := ""
	argv := []string{}
	stagecodeParts := strings.Split(self.stagecodeCmd, " ")
	runFile := path.Join(self.journalPath, fqname)

	switch self.stagecodeLang {
	case "Python":
		shellCmd = path.Join(self.rt.adaptersPath, "python", shellName+".py")
		argv = append(stagecodeParts, metadata.path, metadata.filesPath, runFile, profile, localVars)
	case "Executable":
		shellCmd = stagecodeParts[0]
		argv = append(stagecodeParts[1:], shellName, metadata.path, metadata.filesPath, runFile, profile, localVars)
	default:
		panic(fmt.Sprintf("Unknown stage code language: %s", self.stagecodeLang))
	}

	// Log the job run.
	jobMode := self.rt.jobMode
	jobManager := self.rt.JobManager
	if self.local {
		jobMode = "local"
		jobManager = self.rt.LocalJobManager
	}
	padding := strings.Repeat(" ", int(math.Max(0, float64(10-len(jobMode)))))
	msg := fmt.Sprintf("(run:%s) %s %s.%s", jobMode, padding, fqname, shellName)
	if self.preflight {
		LogInfo("runtime", msg)
	} else {
		PrintInfo("runtime", msg)
	}

	metadata.write("jobinfo", map[string]interface{}{"name": fqname, "type": jobMode})
	jobManager.execJob(shellCmd, argv, envs, metadata, threads, memGB, fqname, shellName)
}

//=============================================================================
// Stagestance
//=============================================================================
type Stagestance struct {
	node *Node
}

func NewStagestance(parent Nodable, callStm *CallStm, callables *Callables) *Stagestance {
	langMap := map[string]string{
		"py":   "Python",
		"exec": "Executable",
	}

	self := &Stagestance{}
	self.node = NewNode(parent, "stage", callStm, callables)
	stage, ok := callables.table[self.node.name].(*Stage)
	if !ok {
		return nil
	}

	stagecodePaths := append([]string{self.node.rt.mroPath}, strings.Split(os.Getenv("PATH"), ":")...)
	stagecodePath, _ := searchPaths(stage.src.path, stagecodePaths)
	self.node.stagecodeCmd = strings.Join(append([]string{stagecodePath}, stage.src.args...), " ")
	if self.node.rt.stest {
		switch stage.src.lang {
		case "py":
			self.node.stagecodeCmd = RelPath(path.Join("..", "adapters", "python", "tester"))
		default:
			panic(fmt.Sprintf("Unsupported stress test language: %s", stage.src.lang))
		}
	}
	self.node.stagecodeLang = langMap[stage.src.lang]
	self.node.split = len(stage.splitParams.list) > 0
	self.node.buildForks(self.node.argbindings)
	return self
}

func (self *Stagestance) getNode() *Node   { return self.node }
func (self *Stagestance) GetState() string { return self.getNode().getState() }
func (self *Stagestance) Step()            { self.getNode().step() }
func (self *Stagestance) RefreshState()    { self.getNode().refreshState() }
func (self *Stagestance) LoadMetadata()    { self.getNode().loadMetadata() }
func (self *Stagestance) Cleanup()         { self.getNode().cleanup() }
func (self *Stagestance) GetFatalError() (string, string, string, string, []string) {
	return self.getNode().getFatalError()
}
func (self *Stagestance) GetWarnings() (string, bool) {
	return self.getNode().getWarnings()
}

//=============================================================================
// Pipestance
//=============================================================================
type Pipestance struct {
	node      *Node
	invokeSrc string
}

func NewPipestance(parent Nodable, invokeSrc string, callStm *CallStm,
	callables *Callables) *Pipestance {
	self := &Pipestance{}
	self.node = NewNode(parent, "pipeline", callStm, callables)
	self.invokeSrc = invokeSrc

	// Build subcall tree.
	pipeline, ok := callables.table[self.node.name].(*Pipeline)
	if !ok {
		return nil
	}
	var preflightNode Nodable = nil
	for _, subcallStm := range pipeline.calls {
		callable := callables.table[subcallStm.id]
		switch callable.(type) {
		case *Stage:
			self.node.subnodes[subcallStm.id] = NewStagestance(self.node, subcallStm, callables)
		case *Pipeline:
			self.node.subnodes[subcallStm.id] = NewPipestance(self.node, "", subcallStm, callables)
		}
		if self.node.subnodes[subcallStm.id].getNode().preflight {
			preflightNode = self.node.subnodes[subcallStm.id]
		}
	}

	// Also depends on stages bound to return values.
	self.node.retbindings = map[string]*Binding{}
	for id, bindStm := range pipeline.ret.bindings.table {
		binding := NewReturnBinding(self.node, bindStm)
		self.node.retbindings[id] = binding
		self.node.retbindingList = append(self.node.retbindingList, binding)
		if binding.mode == "reference" && binding.boundNode != nil {
			prenode := binding.boundNode
			self.node.prenodes[prenode.getNode().fqname] = prenode
			self.node.directPrenodes = append(self.node.directPrenodes, binding.parentNode)

			prenode.getNode().postnodes[self.node.fqname] = self.node
		}
	}
	// Add preflight dependency if preflight stage exists.
	if preflightNode != nil {
		for _, subnode := range self.node.subnodes {
			if subnode != preflightNode {
				subnode.getNode().setPrenode(preflightNode)
			}
		}
	}

	self.node.buildForks(self.node.retbindings)
	return self
}

func (self *Pipestance) getNode() *Node       { return self.node }
func (self *Pipestance) GetPname() string     { return self.node.name }
func (self *Pipestance) GetPsid() string      { return self.node.parent.getNode().name }
func (self *Pipestance) GetFQName() string    { return self.node.fqname }
func (self *Pipestance) GetInvokeSrc() string { return self.invokeSrc }
func (self *Pipestance) RefreshState()        { self.node.refreshState() }

func (self *Pipestance) LoadMetadata() {
	// We used to make this concurrent but ended up with too many
	// goroutines (Pranav's 96-sample run).
	for _, node := range self.node.allNodes() {
		node.loadMetadata()
	}
	for _, node := range self.node.allNodes() {
		node.state = node.getState()
	}
}

func (self *Pipestance) GetState() string {
	nodes := self.node.getFrontierNodes()
	for _, node := range nodes {
		if node.state == "failed" {
			return "failed"
		}
	}
	for _, node := range nodes {
		if node.state == "running" {
			return "running"
		}
	}
	every := true
	for _, node := range nodes {
		if node.state != "complete" {
			every = false
			break
		}
	}
	if every {
		return "complete"
	}
	return "waiting"
}

func (self *Pipestance) RestartAssertedNodes() error {
	self.LoadMetadata()
	nodes := self.node.getFrontierNodes()
	for _, node := range nodes {
		if node.state == "failed" {
			if _, _, _, kind, _ := node.getFatalError(); kind == "assert" {
				if err := node.reset(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (self *Pipestance) RestartRunningNodes(jobMode string) error {
	self.LoadMetadata()
	nodes := self.node.getFrontierNodes()
	localNodes := []*Node{}
	remoteNodes := []*Node{}
	for _, node := range nodes {
		if node.state == "running" {
			PrintInfo("runtime", "Found orphaned stage: %s", node.fqname)
			if jobMode == "local" || node.local {
				localNodes = append(localNodes, node)
			} else {
				remoteNodes = append(remoteNodes, node)
			}
		}
	}
	for _, node := range localNodes {
		if err := node.reset(); err != nil {
			return err
		}
	}
	for _, node := range remoteNodes {
		node.resetJobMonitors()
	}
	return nil
}

func (self *Pipestance) GetWarnings() (string, bool) {
	nodes := self.node.allNodes()
	warnings := ""
	isWarnings := false
	for _, node := range nodes {
		if warning, ok := node.getWarnings(); ok {
			warnings += warning
			isWarnings = true
		}
	}
	return warnings, isWarnings
}

func (self *Pipestance) GetFatalError() (string, string, string, string, []string) {
	nodes := self.node.getFrontierNodes()
	for _, node := range nodes {
		if node.state == "failed" {
			return node.getFatalError()
		}
	}
	return "", "", "", "", []string{}
}

func (self *Pipestance) StepNodes() {
	for _, node := range self.node.getFrontierNodes() {
		node.step()
	}
}

func (self *Pipestance) Reset() error {
	for _, node := range self.node.allNodes() {
		if node.state == "failed" {
			if err := node.reset(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (self *Pipestance) Serialize() interface{} {
	ser := []interface{}{}
	for _, node := range self.node.allNodes() {
		ser = append(ser, node.serialize())
	}
	return ser
}

func (self *Pipestance) Cleanup() {
	self.node.cleanup()
}

func (self *Pipestance) Immortalize() {
	metadata := NewMetadata(self.node.parent.getNode().fqname,
		self.node.parent.getNode().path)
	metadata.write("finalstate", self.Serialize())
}

func (self *Pipestance) Unimmortalize() {
	metadata := NewMetadata(self.node.parent.getNode().fqname,
		self.node.parent.getNode().path)
	metadata.remove("finalstate")
}

func (self *Pipestance) GetOuts(forki int) interface{} {
	if v := self.getNode().forks[forki].metadata.read("outs"); v != nil {
		return v
	}
	return map[string]interface{}{}
}

type VDRKillReport struct {
	Count  uint     `json:"count"`
	Size   uint64   `json:"size"`
	Paths  []string `json:"paths"`
	Errors []string `json:"errors"`
}

func (self *Pipestance) VDRKill() *VDRKillReport {
	killPaths := []string{}

	// Iterate over all nodes.
	for _, node := range self.node.allNodes() {
		// Iterate over all forks.
		for _, fork := range node.forks {
			// For volatile nodes, kill fork-level files.
			if node.volatile {
				if paths, err := fork.metadata.enumerateFiles(); err == nil {
					killPaths = append(killPaths, paths...)
				}
				if paths, err := fork.split_metadata.enumerateFiles(); err == nil {
					killPaths = append(killPaths, paths...)
				}
				if paths, err := fork.join_metadata.enumerateFiles(); err == nil {
					killPaths = append(killPaths, paths...)
				}
			}
			// For ALL nodes, if the node splits, kill chunk-level files.
			// Must check for split here, otherwise we'll end up deleting
			// output files of non-volatile nodes because single-chunk nodes
			// get their output redirected to the one chunk's files path.
			if node.split {
				for _, chunk := range fork.chunks {
					if paths, err := chunk.metadata.enumerateFiles(); err == nil {
						killPaths = append(killPaths, paths...)
					}
				}
			}
		}
	}

	// Actually delete the paths.
	killReport := VDRKillReport{}
	for _, p := range killPaths {
		filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
			if err == nil {
				killReport.Size += uint64(info.Size())
				killReport.Count++
			} else {
				killReport.Errors = append(killReport.Errors, err.Error())
			}
			return nil
		})
		killReport.Paths = append(killReport.Paths, p)
		os.RemoveAll(p)
	}
	metadata := NewMetadata(self.node.parent.getNode().fqname,
		self.node.parent.getNode().path)
	metadata.write("vdrkill", &killReport)
	return &killReport
}

//=============================================================================
// TopNode
//=============================================================================
type TopNode struct {
	node *Node
}

func (self *TopNode) getNode() *Node { return self.node }

func NewTopNode(rt *Runtime, psid string, p string) *TopNode {
	self := &TopNode{}
	self.node = &Node{}
	self.node.frontierNodes = map[string]Nodable{}
	self.node.path = p
	self.node.rt = rt
	self.node.journalPath = path.Join(self.node.path, "journal")
	self.node.tmpPath = path.Join(self.node.path, "tmp")
	self.node.fqname = "ID." + psid
	self.node.name = psid
	return self
}

//=============================================================================
// Runtime
//=============================================================================
type Runtime struct {
	mroPath         string
	adaptersPath    string
	martianVersion  string
	mroVersion      string
	callableTable   map[string]Callable
	PipelineNames   []string
	jobMode         string
	JobManager      JobManager
	LocalJobManager JobManager
	enableProfiling bool
	enableLocalVars bool
	stest           bool
}

func NewRuntime(jobMode string, mroPath string, martianVersion string,
	mroVersion string, enableProfiling bool, enableLocalVars bool, debug bool) *Runtime {
	return NewRuntimeWithCores(jobMode, mroPath, martianVersion, mroVersion,
		-1, -1, enableProfiling, enableLocalVars, debug, false)
}

func NewRuntimeWithCores(jobMode string, mroPath string, martianVersion string,
	mroVersion string, reqCores int, reqMem int, enableProfiling bool,
	enableLocalVars bool, debug bool, stest bool) *Runtime {

	self := &Runtime{}
	self.mroPath = mroPath
	self.adaptersPath = RelPath(path.Join("..", "adapters"))
	self.martianVersion = martianVersion
	self.mroVersion = mroVersion
	self.jobMode = jobMode
	self.enableProfiling = enableProfiling
	self.enableLocalVars = enableLocalVars
	self.callableTable = map[string]Callable{}
	self.PipelineNames = []string{}
	self.stest = stest

	self.LocalJobManager = NewLocalJobManager(reqCores, reqMem, debug)
	if self.jobMode == "local" {
		self.JobManager = self.LocalJobManager
	} else {
		self.JobManager = NewRemoteJobManager(self.jobMode)
	}

	// Parse all MROs in MROPATH and cache pipelines by name.
	fpaths, _ := filepath.Glob(self.mroPath + "/[^_]*.mro")
	for _, fpath := range fpaths {
		if data, err := ioutil.ReadFile(fpath); err == nil {
			if _, ast, err := parseSource(string(data), fpath, []string{self.mroPath}, true); err == nil {
				for _, callable := range ast.callables.table {
					self.callableTable[callable.getId()] = callable
					if _, ok := callable.(*Pipeline); ok {
						self.PipelineNames = append(self.PipelineNames, callable.getId())
					}
				}
			}
		}
	}
	return self
}

// Compile an MRO file in cwd or self.mroPath.
func (self *Runtime) Compile(fpath string, checkSrcPath bool) (string, *Ast, error) {
	if data, err := ioutil.ReadFile(fpath); err != nil {
		return "", nil, err
	} else {
		return parseSource(string(data), fpath, []string{self.mroPath}, checkSrcPath)
	}
}

// Compile all the MRO files in self.mroPath.
func (self *Runtime) CompileAll(checkSrcPath bool) (int, error) {
	fpaths, _ := filepath.Glob(self.mroPath + "/[^_]*.mro")
	for _, fpath := range fpaths {
		if _, _, err := self.Compile(fpath, checkSrcPath); err != nil {
			return 0, err
		}
	}
	return len(fpaths), nil
}

// Instantiate a pipestance object given a psid, MRO source, and a
// pipestance path. This is the core (private) method called by the
// public InvokeWithSource and Reattach methods.
func (self *Runtime) instantiatePipeline(src string, srcPath string, psid string,
	pipestancePath string) (string, *Pipestance, error) {
	// Parse the invocation source.
	postsrc, ast, err := parseSource(src, srcPath, []string{self.mroPath}, true)
	if err != nil {
		return "", nil, err
	}

	// Check there's a call.
	if ast.call == nil {
		return "", nil, &RuntimeError{"cannot start a pipeline without a call statement"}
	}
	// Make sure it's a pipeline we're calling.
	if pipeline := ast.callables.table[ast.call.id]; pipeline == nil {
		return "", nil, &RuntimeError{fmt.Sprintf("'%s' is not a declared pipeline", ast.call.id)}
	}

	// Instantiate the pipeline.
	pipestance := NewPipestance(NewTopNode(self, psid, pipestancePath), src, ast.call, ast.callables)
	if pipestance == nil {
		return "", nil, &RuntimeError{fmt.Sprintf("'%s' is not a declared pipeline", ast.call.id)}
	}
	return postsrc, pipestance, nil
}

// Invokes a new pipestance.
func (self *Runtime) InvokePipeline(src string, srcPath string, psid string,
	pipestancePath string) (*Pipestance, error) {

	// Error if pipestance exists, otherwise create.
	if _, err := os.Stat(pipestancePath); err == nil {
		return nil, &PipestanceExistsError{psid}
	} else if err := os.MkdirAll(pipestancePath, 0755); err != nil {
		return nil, err
	}

	// Expand env vars in invocation source and instantiate.
	src = os.ExpandEnv(src)
	postsrc, pipestance, err := self.instantiatePipeline(src, srcPath, psid, pipestancePath)
	if err != nil {
		// If instantiation failed, delete the pipestance folder.
		os.RemoveAll(pipestancePath)
		return nil, err
	}

	// Write top-level metadata files.
	metadata := NewMetadata("ID."+psid, pipestancePath)
	metadata.writeRaw("invocation", src)
	metadata.writeRaw("mrosource", postsrc)
	metadata.write("versions", map[string]string{
		"martian":   GetVersion(),
		"pipelines": GetGitTag(self.mroPath),
	})
	metadata.writeTime("timestamp")

	// Create pipestance folder graph concurrently.
	var wg sync.WaitGroup
	pipestance.getNode().mkdirs(&wg)
	wg.Wait()

	return pipestance, nil
}

// Reattaches to an existing pipestance.
func (self *Runtime) ReattachToPipestance(psid string, pipestancePath string) (*Pipestance, error) {
	fname := "_invocation"

	// Read in the existing _invocation file.
	data, err := ioutil.ReadFile(path.Join(pipestancePath, fname))
	if err != nil {
		return nil, err
	}

	// Instantiate the pipestance.
	_, pipestance, err := self.instantiatePipeline(string(data), fname, psid, pipestancePath)

	// If we're reattaching in local mode, restart any stages that were
	// left in a running state from last mrp run. The actual job would
	// have been killed by the CTRL-C.
	if err == nil {
		PrintInfo("runtime", "Reattaching in %s mode.", self.jobMode)
		err = pipestance.RestartRunningNodes(self.jobMode)
	}

	return pipestance, err
}

// Instantiate a stagestance.
func (self *Runtime) InvokeStage(src string, srcPath string, ssid string,
	stagestancePath string) (*Stagestance, error) {
	// Check if stagestance path already exists.
	if _, err := os.Stat(stagestancePath); err == nil {
		return nil, &RuntimeError{fmt.Sprintf("stagestance '%s' already exists", ssid)}
	} else if err := os.MkdirAll(stagestancePath, 0755); err != nil {
		return nil, err
	}

	// Parse the invocation source.
	src = os.ExpandEnv(src)
	_, ast, err := parseSource(src, srcPath, []string{self.mroPath}, true)
	if err != nil {
		return nil, err
	}

	// Check there's a call.
	if ast.call == nil {
		return nil, &RuntimeError{"cannot start a stage without a call statement"}
	}
	// Make sure it's a stage we're calling.
	if _, ok := ast.callables.table[ast.call.id].(*Stage); !ok {
		return nil, &RuntimeError{fmt.Sprintf("'%s' is not a declared stage", ast.call.id)}
	}

	// Instantiate stagestance.
	stagestance := NewStagestance(NewTopNode(self, "", stagestancePath), ast.call, ast.callables)
	if stagestance == nil {
		return nil, &RuntimeError{fmt.Sprintf("'%s' is not a declared stage", ast.call.id)}
	}

	// Create stagestance folder graph concurrently.
	var wg sync.WaitGroup
	stagestance.getNode().mkdirs(&wg)
	wg.Wait()

	return stagestance, nil
}

func (self *Runtime) GetSerialization(pipestancePath string) (interface{}, bool) {
	metadata := NewMetadata("", pipestancePath)
	metadata.loadCache()
	if metadata.exists("finalstate") {
		return metadata.read("finalstate"), true
	}
	return nil, false
}

/****************************************************************************
 * Used Only for MARSOC
 */
func (self *Runtime) buildVal(param Param, val interface{}) string {
	// MRO value expression syntax is identical to JSON. Just need to make
	// sure floats get printed with decimal points.
	switch {
	case param.getTname() == "float" && val != nil:
		return fmt.Sprintf("%f", val)
	default:
		indent := "    "
		if data, err := json.MarshalIndent(val, "", indent); err == nil {
			// Indent multi-line values (but not first line).
			sublines := strings.Split(string(data), "\n")
			for i, _ := range sublines[1:] {
				sublines[i+1] = indent + sublines[i+1]
			}
			return strings.Join(sublines, "\n")
		}
		return fmt.Sprintf("<ParseError: %v>", val)
	}
}

func (self *Runtime) BuildCallSource(incpaths []string, name string,
	args map[string]interface{}) (string, error) {
	// Make sure pipeline has been imported
	if _, ok := self.callableTable[name]; !ok {
		return "", &RuntimeError{fmt.Sprintf("'%s' is not a declared pipeline or stage", name)}
	}

	// Build @include statements.
	includes := []string{}
	for _, incpath := range incpaths {
		includes = append(includes, fmt.Sprintf("@include \"%s\"", incpath))
	}
	// Loop over the pipeline's in params and print a binding
	// whether the args bag has a value for it not.
	lines := []string{}
	for _, param := range self.callableTable[name].getInParams().list {
		valstr := self.buildVal(param, args[param.getId()])
		lines = append(lines, fmt.Sprintf("    %s = %s,", param.getId(), valstr))
	}
	return fmt.Sprintf("%s\n\ncall %s(\n%s\n)", strings.Join(includes, "\n"),
		name, strings.Join(lines, "\n")), nil
}
