// Copyright (c) 2016 10X Genomics, Inc. All rights reserved.

package main

import (
	"flag"
	"log"
	"martian/core"
	"martian/ligolib"
	"os"
)

var flag_pipestance_path = flag.String("path", "", "path to pipestance")

func LookupCallInfo(basepath string) (string, string, string) {

	_, _, ast, err := core.Compile(basepath+"/_mrosource", []string{}, false)
	if err != nil {
		panic(err)
	}

	call := ast.Call.Id

	sampleid := core.SearchPipestanceParams(ast, "sample_id")
	if sampleid == nil {
		panic("WTF2")
	}

	desc := core.SearchPipestanceParams(ast, "sample_desc")
	if desc == nil {
		panic("WTF3")
	}

	return sampleid.(string), desc.(string), call
}

func main() {
	c := ligolib.Setup()
	//c.Dump()

	var rr ligolib.ReportRecord

	flag.Parse()

	if *flag_pipestance_path == "" {
		panic("bad args")
	}

	version, err := ligolib.GetPipestanceVersion(*flag_pipestance_path)

	if err != nil {
		panic(err)
	}

	rr.SHA = version
	rr.Branch = version
	rr.SampleId, rr.Comments, rr.Project = LookupCallInfo(*flag_pipestance_path)
	rr.UserId = os.Getenv("USER")
	rr.FinishDate = ligolib.GetPipestanceDate(*flag_pipestance_path)
	log.Printf("%v", rr)

	/*
		jsondata, err := ioutil.ReadFile(*flag_pipestance_path + "/" + project.SummaryJSONPath)
		if err != nil {
			panic(err)
		}
	*/

	//rr.SummaryJSON = string(jsondata)
	id, err := c.InsertRecord("test_reports", rr)
	if err != nil {
		panic(err)
	}

	ligolib.CheckinSummaries(c, id, *flag_pipestance_path)
	ligolib.CheckinOne(c, id, *flag_pipestance_path+"/_perf", "_perf")
	ligolib.CheckinOne(c, id, *flag_pipestance_path+"/_tags", "_tags")
}
