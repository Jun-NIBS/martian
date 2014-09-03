//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// Marsoc argshim shim.
//
package main

import (
	"bufio"
	"encoding/json"
	"mario/core"
	"os/exec"
	"sync"
)

type ArgShim struct {
	cmdPath            string
	samplePipelinesMap map[string]string
	writer             *bufio.Writer
	reader             *bufio.Reader
	mutex              *sync.Mutex
}

func NewArgShim(argshimPath string) *ArgShim {
	self := &ArgShim{}
	self.cmdPath = argshimPath
	self.mutex = &sync.Mutex{}

	cmd := exec.Command(self.cmdPath)
	stdin, _ := cmd.StdinPipe()
	self.writer = bufio.NewWriterSize(stdin, 1000000)
	stdout, _ := cmd.StdoutPipe()
	self.reader = bufio.NewReaderSize(stdout, 1000000)
	cmd.Start()

	self.samplePipelinesMap = self.getSamplePipelinesMap()

	return self
}

func (self *ArgShim) invoke(function string, arguments []interface{}) interface{} {
	input := map[string]interface{}{
		"function":  function,
		"arguments": arguments,
	}
	bytes, _ := json.Marshal(input)

	self.mutex.Lock()
	self.writer.Write([]byte(string(bytes) + "\n"))
	self.writer.Flush()

	line, _, _ := self.reader.ReadLine()
	self.mutex.Unlock()

	var v interface{}
	json.Unmarshal(line, &v)
	return v
}

func (self *ArgShim) getSamplePipelinesMap() map[string]string {
	// Just capture the sample to pipeline map and do lookups locally.
	// No sense in invoking Node.js each time.
	v := self.invoke("getSamplePipelinesMap", []interface{}{})
	if tv, ok := v.(map[string]interface{}); ok {
		ntv := map[string]string{}
		for k, v := range tv {
			ntv[k] = v.(string)
		}
		return ntv
	}
	return map[string]string{}
}

func (self *ArgShim) getPipelineForSample(sample *Sample) string {
	return self.samplePipelinesMap[sample.Workflow.Name]
}

func (self *ArgShim) buildArgsForRun(run *Run) map[string]interface{} {
	v := self.invoke("buildArgsForRun", []interface{}{
		run,
	})
	if tv, ok := v.(map[string]interface{}); ok {
		return tv
	}
	return map[string]interface{}{}
}

func (self *ArgShim) buildArgsForSample(run *Run, sample interface{}, preprocessOuts interface{}) map[string]interface{} {
	v := self.invoke("buildArgsForSample", []interface{}{
		run,
		sample,
		preprocessOuts,
	})
	if tv, ok := v.(map[string]interface{}); ok {
		return tv
	}
	return map[string]interface{}{}
}

func (self *ArgShim) buildCallSourceForRun(rt *core.Runtime, run *Run) string {
	shimout := self.buildArgsForRun(run)
	pipeline, ok := shimout["pipeline"].(string)
	if !ok {
		return ""
	}
	args, ok := shimout["args"].(map[string]interface{})
	if !ok {
		return ""
	}
	return rt.BuildCallSource(pipeline, args)
}

func (self *ArgShim) buildCallSourceForSample(rt *core.Runtime, preprocPipestance *core.Pipestance, run *Run, sample interface{}) string {
	var preprocessOuts interface{}
	if preprocPipestance != nil {
		preprocessOuts = preprocPipestance.GetOuts(0)
	} else {
		preprocessOuts = map[string]interface{}{}
	}
	shimout := self.buildArgsForSample(run, sample, preprocessOuts)
	pipeline, ok := shimout["pipeline"].(string)
	if !ok {
		return ""
	}
	args, ok := shimout["args"].(map[string]interface{})
	if !ok {
		return ""
	}
	return rt.BuildCallSource(pipeline, args)

}
