package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"martian/core"
	"os"
	"path"
	"sync"
	"time"
)

type PipestanceManager struct {
	psPaths       []string
	exploredPaths map[string]bool
	cache         map[string]bool
	rt            *core.Runtime
	db            *DatabaseManager
}

func makeInvocationPath(root string) string {
	return path.Join(root, "_invocation")
}

func makePerfPath(root string) string {
	return path.Join(root, "_perf")
}

func makeVersionsPath(root string) string {
	return path.Join(root, "_versions")
}

func makeTagsPath(root string) string {
	return path.Join(root, "_tags")
}

func read(path string) string {
	bytes, _ := ioutil.ReadFile(path)
	return string(bytes)
}

func NewPipestanceManager(psPaths []string, db *DatabaseManager, rt *core.Runtime) *PipestanceManager {
	self := &PipestanceManager{}
	self.psPaths = psPaths
	self.rt = rt
	self.db = db
	self.loadCache()
	return self
}

func (self *PipestanceManager) loadCache() {
	self.exploredPaths = map[string]bool{}
	self.cache = map[string]bool{}

	pipestances, err := self.db.GetPipestances()
	if err != nil {
		core.LogError(err, "keplerd", "Failed to load pipestance caches: %s", err.Error())
		os.Exit(1)
	}

	for _, ps := range pipestances {
		path := ps["path"]
		fqname := ps["fqname"]
		self.exploredPaths[path] = true
		self.cache[fqname] = true
	}
}

func (self *PipestanceManager) recursePath(root string) []string {
	newPsPaths := []string{}
	if _, ok := self.exploredPaths[root]; ok {
		// This directory has already been explored
		return newPsPaths
	}

	invocationPath := makeInvocationPath(root)
	perfPath := makePerfPath(root)
	if _, err := os.Stat(invocationPath); err == nil {
		// This directory is a pipestance
		if _, err := os.Stat(perfPath); err == nil {
			core.LogInfo("keplerd", "Found pipestance %s", root)
			newPsPaths = append(newPsPaths, root)
			self.exploredPaths[root] = true
		}
		return newPsPaths
	}

	// Otherwise recurse until we find a pipestance directory
	infos, _ := ioutil.ReadDir(root)
	for _, info := range infos {
		if info.IsDir() {
			newPsPaths = append(newPsPaths, self.recursePath(
				path.Join(root, info.Name()))...)
		}
	}
	return newPsPaths
}

func (self *PipestanceManager) parseVersions(path string) (string, string) {
	var v map[string]string
	if err := json.Unmarshal([]byte(read(path)), &v); err == nil {
		return v["martian"], v["pipelines"]
	}
	return "", ""
}

func (self *PipestanceManager) parseInvocation(path string) (string, map[string]interface{}) {
	if v, err := self.rt.BuildCallJSON(read(path), path); err == nil {
		return v["call"].(string), v["args"].(map[string]interface{})
	}
	return "", map[string]interface{}{}
}

func (self *PipestanceManager) parseTags(path string) []string {
	var v []string
	if err := json.Unmarshal([]byte(read(path)), &v); err == nil {
		return v
	}
	return []string{}
}

func (self *PipestanceManager) InsertPipestance(psPath string) error {
	perfPath := makePerfPath(psPath)
	invocationPath := makeInvocationPath(psPath)
	versionsPath := makeVersionsPath(psPath)
	tagsPath := makeTagsPath(psPath)

	martianVersion, pipelinesVersion := self.parseVersions(versionsPath)
	call, args := self.parseInvocation(invocationPath)
	tags := self.parseTags(tagsPath)

	var nodes []*core.NodePerfInfo
	err := json.Unmarshal([]byte(read(perfPath)), &nodes)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return &core.MartianError{fmt.Sprintf("Pipestance %s has empty _perf file", psPath)}
	}

	// Check cache
	fqname := nodes[0].Fqname
	if _, ok := self.cache[fqname]; ok {
		return nil
	}

	// Wrap database insertions in transaction
	tx := NewDatabaseTx()
	tx.Begin()
	defer tx.End()

	// Insert pipestance with its metadata
	err = self.db.InsertPipestance(tx, psPath, fqname, martianVersion,
		pipelinesVersion, call, args, tags)
	if err != nil {
		return err
	}

	// First pass: Insert all forks, chunks, splits, joins
	for _, node := range nodes {
		for _, fork := range node.Forks {
			self.db.InsertFork(tx, node.Name, node.Fqname, node.Type, fork.Index, fork.ForkStats)
			if fork.SplitStats != nil {
				err := self.db.InsertSplit(tx, node.Fqname, fork.Index, fork.SplitStats)
				if err != nil {
					return err
				}
			}
			if fork.JoinStats != nil {
				err := self.db.InsertJoin(tx, node.Fqname, fork.Index, fork.JoinStats)
				if err != nil {
					return err
				}
			}
			for _, chunk := range fork.Chunks {
				err := self.db.InsertChunk(tx, node.Fqname, fork.Index, chunk.ChunkStats, chunk.Index)
				if err != nil {
					return err
				}
			}
		}
	}

	// Second pass: Insert relationships between pipelines and stages
	for _, node := range nodes {
		for _, fork := range node.Forks {
			for _, stage := range fork.Stages {
				err := self.db.InsertRelationship(tx, node.Fqname, fork.Index, stage.Fqname, stage.Forki)
				if err != nil {
					return err
				}
			}
		}
	}

	// Insert pipestance into cache
	self.cache[fqname] = true

	return nil
}

func (self *PipestanceManager) InsertPipestances(newPsPaths []string) {
	for _, newPsPath := range newPsPaths {
		core.LogInfo("keplerd", "Adding pipestance %s", newPsPath)
		if err := self.InsertPipestance(newPsPath); err != nil {
			core.LogError(err, "keplerd", "Failed to add pipestance %s: %s",
				newPsPath, err.Error())
			delete(self.exploredPaths, newPsPath)
		}
	}
}

func (self *PipestanceManager) Start() {
	go func() {
		var wg sync.WaitGroup
		for _, psPath := range self.psPaths {
			wg.Add(1)
			go func(psPath string) {
				defer wg.Done()
				newPsPaths := self.recursePath(psPath)
				self.InsertPipestances(newPsPaths)
			}(psPath)
		}
		wg.Wait()
		time.Sleep(time.Minute * time.Duration(5))
	}()
}
