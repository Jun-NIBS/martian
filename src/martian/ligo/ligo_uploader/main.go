// Copyright (c) 2016 10X Genomics, Inc. All rights reserved.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"martian/core"
	"martian/ligo/ligolib"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var flag_pipestance_path = flag.String("path", "", "path to pipestance")
var flag_extras = flag.String("extras", "", "extra data to upload NAME:path,NAME:path...")
var flag_test_group = flag.String("testgroup", "", "Set the testgroup column in the database")

/* Look up some basic info about a pipestance. This returns a triple of
 * |sample id|, |sample description|, |pipeline name|.
 * If we can't get the sample id or pipeline name, we give up and
 * terminate the program.
 */
func LookupCallInfo(basepath string) (string, string, string) {

	_, _, ast, err := core.Compile(basepath+"/_mrosource", []string{}, false)
	if err != nil {
		log.Printf("Could not compile your _mrosource file: %v", err)
		os.Exit(1)
	}

	call := ast.Call.Id

	sampleid := core.SearchPipestanceParams(ast, "sample_id")
	if sampleid == nil {
		log.Printf("Could not find a sample_id parameter. Giving up.")
		os.Exit(1)
	}

	desc := core.SearchPipestanceParams(ast, "sample_desc")
	if desc == nil {
		desc = ""
	}

	return sampleid.(string), desc.(string), call
}

/*
 * Connect to the lena server (via MARSOC) and get the sample def to incorporate
 * into our upload to lig9.
 */
func GrabFromLena(host string, lena_id int) string {

	url := ("http://" + host + "/api/shimulate/" + fmt.Sprintf("%v", lena_id))
	log.Printf("Requesting lena data from: %v", url)
	req, err := http.Get(url)
	defer req.Body.Close()

	if err != nil {
		log.Printf("ERR: %v", err)
		return ""
	}

	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("ERR: %v", err)
		return ""
	}

	/* Check that we got valid JSON back (but don't do anything with it.)*/
	var as_json interface{}
	err = json.Unmarshal(data, &as_json)
	if err != nil {
		log.Printf("ERR: %v", err)
		return ""
	}

	return string(data)
}

/*
 * Get the testgroup for a pipestance.  This looks in the tags file
 * for an item matching "testgroup:XXXXXX" and returns the XXXXXX part.
 */
func GuessTestGroup(pipestance_path string) (string, error) {
	tags := []string{}

	data, err := ioutil.ReadFile(pipestance_path + "/_tags")

	if err != nil {
		log.Printf("Cannot open: %v: %v", pipestance_path, err)
		return "", err
	}

	err = json.Unmarshal(data, &tags)
	if err != nil {
		log.Printf("Tags file does not look like valid JSON: %v", err)
		return "", err
	}

	prefix := "testgroup:"
	for _, tag := range tags {
		if len(tag) > len(prefix) && tag[0:len(prefix)] == prefix {
			return tag[len(prefix):], nil
		}
	}

	return "", nil
}

func main() {
	/* Connect to the ligo database */
	c := ligolib.Setup(os.Getenv("LIGO_DB"))

	var rr ligolib.ReportRecord

	flag.Parse()

	if *flag_pipestance_path == "" {
		panic("bad args")
	}

	version, err := ligolib.GetPipestanceVersion(*flag_pipestance_path)

	if err != nil {
		panic(err)
	}

	/* Fill out a test record structure from the data we can find in the pipestance */
	rr.SHA = version
	rr.Branch = version
	rr.SampleId, rr.Comments, rr.Project = LookupCallInfo(*flag_pipestance_path)
	rr.UserId = os.Getenv("USER")
	rr.FinishDate = ligolib.GetPipestanceDate(*flag_pipestance_path)
	rr.Success = ligolib.GetPipestanceSuccess(*flag_pipestance_path)
	rr.TestGroup = *flag_test_group

	/* If we didn't specify a test group on the command line,
	 * we try to guess one by looking at the pipestance tags.
	 * Recent versions of SERE will automatically populate
	 * the pipestance with a tag that looks like
	 * testgroup:%prgoram.%cycle.%round
	 */
	if *flag_test_group == "" {
		tg, err := GuessTestGroup(*flag_pipestance_path)
		if err != nil {
			return
		}
		rr.TestGroup = tg
	}

	log.Printf("SAMPLE DEFINITION: %v", rr)

	/* Start a database transaction */
	err = c.Begin()
	if err != nil {
		panic(err)
	}

	/* insert the test_report. Then link a bunch of report sumamries to it*/
	id, err := c.InsertRecord("test_reports", rr)
	if err != nil {
		panic(err)
	}

	/* Upload every summary.json file from the whole pipestance. */
	ligolib.InsertPipestanceSummaries(c, id, *flag_pipestance_path)

	/* upload the _perf and _tags files */
	ligolib.InsertSummary(c, id, *flag_pipestance_path+"/_perf", "_perf")
	ligolib.InsertSummary(c, id, *flag_pipestance_path+"/_tags", "_tags")

	/* Does this look like it came from LENA? Try to upload the LENA sample
	 * info.
	 */
	sampleid_int, err := strconv.Atoi(rr.SampleId)
	if err == nil {

		lena_invocation_data_as_str := GrabFromLena("marsoc", sampleid_int)
		if lena_invocation_data_as_str == "" {
			log.Printf("Didn't get any decent LENA data.")
		} else {
			_, err = c.InsertRecord("test_report_summaries",
				ligolib.ReportSummaryFile{0, id, lena_invocation_data_as_str, "_lena"})
			if err != nil {
				panic(err)
			}
		}

	}

	/* Upload any extra files */
	if *flag_extras != "" {
		extras_list := strings.Split(*flag_extras, ",")
		for _, e := range extras_list {
			parts := strings.Split(e, ":")
			if len(parts) != 2 {
				log.Printf("Cannot parse -extras flag.")
				return
			}
			name := parts[0]
			path := parts[1]
			ligolib.InsertSummary(c, id, *flag_pipestance_path+"/"+path, name)
		}
	}

	c.Commit()
}
