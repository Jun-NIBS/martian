package sere2lib

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
)

type MetricDef struct {
	JSONPath     string
	HumanName    string
	Type         string
	Low          *float64
	High         *float64
	AbsDiffAllow *float64
	RelDiffAllow *float64
	Owner        string
}

type MetricsDef struct {
	Metrics map[string]*MetricDef
}

type MetricResult struct {
	BaseVal float64
	NewVal  float64
	OK      bool
	Diff    bool
	Def     *MetricDef
}

/*
 * Load a metrics file from disk and return a MetricsDef structure that
 * describes the listed metrics.
 * The loads a file in the prescribed JSON format and then munges the result.
 */
func LoadMetricsDef(path string) *MetricsDef {

	/* Load file and parse JSON */
	file_contents, err := ioutil.ReadFile(path)

	if err != nil {
		panic(err)
	}

	var m MetricsDef

	json.Unmarshal(file_contents, &m)

	/*
	 * Munge the result so that metricdef also knows the path to the metric
	 * (which is the key in the map that it is in
	 */
	for k, _ := range m.Metrics {
		m.Metrics[k].JSONPath = k
	}

	log.Printf("Loading metric from %v: %v", path, len(m.Metrics))
	return &m
}

func Abs(x float64) float64 {
	if x < 0 {
		return -x
	} else {
		return x
	}
}

/*
 * Decide if two numbers are different given a metric definition.
 */
func CheckDiff(m *MetricDef, oldguy float64, newguy float64) bool {

	log.Printf("Compare %v %v (%v)", oldguy, newguy, *m)

	/* If the new value is outside of an prescribed range, we claim it
	 * is different (Regardless of the old value).
	 */
	if m.High != nil && newguy > *m.High {
		return true
	}
	if m.Low != nil && newguy < *m.Low {
		return true
	}

	/* If an absolute different threshhold is specified, use it */
	if m.AbsDiffAllow != nil {
		if Abs(oldguy-newguy) > *m.AbsDiffAllow {
			return true
		}
	}

	var max_percent float64

	/* If a max relative difference (percentile) is specified use it.
	 * If nothing at all is specified then, assume a max difference of
	 * 1.0.
	 */
	if m.RelDiffAllow == nil {
		if m.AbsDiffAllow == nil && m.Low == nil && m.High == nil {
			max_percent = 1.0
		} else {
			/* If something else was specified, and RedDiffAllow was not
			 * specified, we're done.
			 */
			return false
		}
	} else {
		max_percent = *m.RelDiffAllow
	}

	/* Handle division by zero: if oldguy==newguy there is no difference
	 * even if oldguy is 0.  Otherwise, if oldguy==0 and newguy!=0, there is
	 * a difference.
	 */
	if newguy == oldguy {
		return false
	}

	if oldguy == 0.0 {
		return true
	}

	if Abs((newguy-oldguy)/oldguy) > max_percent/100.0 {
		return true
	}

	return false
}

/*
 * Compare two pipestance invocations, specified by pipestance invocation ID.
 */
func Compare2(db *CoreConnection, m *MetricsDef, base int, newguy int) []MetricResult {

	/* Flatten the list of metrics */
	list_of_metrics := make([]string, 0, len(m.Metrics))
	for k, _ := range m.Metrics {
		list_of_metrics = append(list_of_metrics, k)
	}

	/* Grab the metric for each pipestance */
	log.Printf("Comparing %v and %v", base, newguy)
	basedata := db.JSONExtract2(fmt.Sprintf("test_reports.id = %v", base), list_of_metrics)
	newdata := db.JSONExtract2(fmt.Sprintf("test_reports.id = %v", newguy), list_of_metrics)

	results := make([]MetricResult, 0, 0)

	/* Iterate over all metric definitions and compare the respective metrics */
	for _, one_metric := range list_of_metrics {
		newval := basedata[0][one_metric]
		baseval := newdata[0][one_metric]

		var mr MetricResult
		mr.Def = (m.Metrics[one_metric])

		newfloat, ok1 := strconv.ParseFloat(newval.(string), 64)
		basefloat, ok2 := strconv.ParseFloat(baseval.(string), 64)

		if ok1 == nil && ok2 == nil {

			mr.Diff = CheckDiff((m.Metrics[one_metric]), newfloat, basefloat)
			mr.BaseVal = basefloat
			mr.NewVal = newfloat
			mr.OK = true
		} else {
			log.Printf("Trouble at %v %v (%v %v)", newval, baseval, ok1, ok2)
			mr.OK = false
		}

		results = append(results, mr)
	}

	return results
}
