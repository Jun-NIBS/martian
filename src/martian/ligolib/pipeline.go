// Copyright (c) 2016 10X Genomics, Inc. All rights reserved.

/*
 * This implements functions for extracting various bits of metadata from a pipeline
 */
package ligolib

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ProjectInfo struct {
	TopLevel        string
	Name            string
	SummaryJSONPath string
}

var ProjectDefs = []ProjectInfo{
	{"PHASER_SVCALLER_EXOME_PD", "longranger-exome", "PHASER_SVCALLER_EXOME_PD/SUMMARIZE_REPORTS_PD/fork0/files/summary.json"},
	{"PHASER_SVCALLER_PD", "longranger-wgs", "PHASER_SVCALLER_PD/SUMMARIZE_REPORTS_PD/fork0/files/summary.json"},
}

/*
 * Guess what kind of project this is. We look for a top-level file
 * (or directory) that matches "TopLevel" in some project
 * definition.
 */
func GuessProject(path string) *ProjectInfo {

	for i := 0; i < len(ProjectDefs); i++ {
		try := &ProjectDefs[i]
		_, err := os.Stat(path + "/" + try.TopLevel)
		if err == nil {
			return try
		}
	}
	return nil
}

/*
 * Load JSON from a path
 */
func jsonload(path string) (map[string]interface{}, error) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		log.Printf("cannot load: %v %v", path, err)
		return nil, err
	}

	res := make(map[string]interface{})
	err = json.Unmarshal(contents, &res)

	if err != nil {
		log.Printf("can't parse json: %v", err)
		return nil, err
	}

	return res, nil

}

/*
 * Get the version of a pipestance by inspecting the _versions file.
 */
func GetPipestanceVersion(pipestance_path string) (string, error) {
	versions_file_path := pipestance_path + "/_versions"
	jsondata, err := jsonload(versions_file_path)

	if err != nil {
		return "", err
	}

	/* Is this always right? What about cellranger or supernova? */
	version := jsondata["pipelines"].(string)

	log.Printf("autodetect version of (%v): %v", pipestance_path, version)
	return version, nil

}

/*
 * Grab every summary.json file from a pipestance and upload it to the database.
 */
func CheckinSummaries(db *CoreConnection, test_report_id int, pipestance_path string) {

	filepath.Walk(pipestance_path+"/", func(path string, info os.FileInfo, e error) error {
		if len(info.Name()) > 4 && info.Name()[0:4] == "chnk" {
			/* Don't grab stuff that's inside a chunk. If we're in a chunk, forget
			 * about this entire subtree
			 */
			return filepath.SkipDir
		}
		if info.Name() == "summary.json" {
			/* Woohoo! found a summary file.*/
			log.Printf("Found summary at %v", path)

			/* Calculate the stage name for this file. XXX There should be a safer
			 * way to do this
			 */
			stagepath_array := strings.Split(path, "/")
			stage := stagepath_array[len(stagepath_array)-4]

			/* Grab the file */
			contents, err := ioutil.ReadFile(path)
			if err != nil {
				panic("Can't read a file that I found from filepath.Walk")
			}

			/* Check that the file is valid JSON. Don't try to upload invalid
			 * JSON*/
			var data_as_json interface{}
			if json.Unmarshal(contents, &data_as_json) != nil {
				log.Printf("file %v is not JSON!!!", path)
			} else {
				r := ReportSummaryFile{0, test_report_id, string(contents), stage}
				_, err = db.InsertRecord("test_report_summaries", r)
				if err != nil {
					panic("Trouble uploading file to DB")
				}
			}
		}
		return nil
	})
}

/*
 * Grab a specific JSON file and upload that to the database.
 */
func CheckinOne(db *CoreConnection, test_report_id int, path string, name string) error {
	contents, err := ioutil.ReadFile(path)

	if err != nil {
		panic(err)
	}

	var as_json interface{}
	err = json.Unmarshal(contents, &as_json)

	if err != nil {
		return err
	}

	report := ReportSummaryFile{0, test_report_id, string(contents), name}

	_, err = db.InsertRecord("test_report_summaries", report)
	if err != nil {
		panic(err)
	}
	return nil
}

/*
 * Get the date that the pipestance finished.
 */
func GetPipestanceDate(path string) time.Time {

	file_info, err := os.Stat(path + "/_finalstate")

	if err != nil {
		panic(err)
	}

	return file_info.ModTime()
}
