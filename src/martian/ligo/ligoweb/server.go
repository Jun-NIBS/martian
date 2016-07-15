// Copyright (c) 2016 10X Genomics, Inc. All rights reserved.

/*
 * This implements the core of the "ligo" webserver for viewing information
 * in the ligo db.
 */
package ligoweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-martini/martini"
	"github.com/joker/jade"
	"io/ioutil"
	"log"
	"martian/ligo/ligolib"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type LigoServer struct {
	DB *ligolib.CoreConnection
	//WebService * martini.Martini
	WebBase  string
	v        http.Handler
	Projects *ligolib.ProjectsCache
}

type GenericResponse struct {
	ERROR *string
	STUFF interface{}
}

/*
 * Setup a server.
 * |port| is the port to run on
 * db is an instance of the database connection (and other config)
 * webbase is the root directory of the web routes and assets.  Relative to the
 *   git root, it is web/ligo
 */
func SetupServer(port int, db *ligolib.CoreConnection, webbase string) {
	ls := new(LigoServer)
	ls.DB = db
	ls.WebBase = webbase
	ls.Projects = ligolib.LoadAllProjects(webbase + "/metrics")

	martini.Root = webbase
	m := martini.Classic()
	//ls.WebService = m;

	m.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/views/unified.jade", 302)
	})
	/* Serve static assets ouf of the assets directory */
	m.Get("/assets/**", http.StripPrefix("/assets/", http.FileServer(http.Dir(webbase+"/assets"))))

	/* Process and serve views from here. We match view names to file names */
	m.Get("/views/**", ls.Viewer)

	/* API endpoints to do useful things */

	m.Get("/api/plot", ls.MakeWrapper(ls.Plot))

	m.Get("/api/compare", ls.MakeWrapper(ls.Compare))

	m.Get("/api/plotall", ls.MakeWrapper(ls.PlotAll))

	//m.Get("/api/list_metrics", ls.ListProjects)
	m.Get("/api/list_metrics", ls.MakeWrapper(ls.ListMetrics))

	m.Get("/api/list_metric_sets", ls.ListProjects)

	m.Get("/api/reload_metrics", ls.Reload)

	m.Get("/api/details", ls.MakeWrapper(ls.Details))
	m.Get("/api/error", ls.MakeWrapper(ls.NeverWorks))

	m.Post("/api/tmpproject", ls.UploadTempProject)
	m.Get("/api/downloadproject", ls.DownloadProject)

	/* Start it up! */
	m.Run()
}

/*
 * This is a simple interface to serve jade templates out of the "views"
 * directory.
 */
func (s *LigoServer) Viewer(w http.ResponseWriter, r *http.Request) {
	psplit := strings.Split(r.URL.Path, "/")

	viewfile := psplit[len(psplit)-1]

	buf, err := ioutil.ReadFile(s.WebBase + "/views/" + viewfile)

	if err != nil {
		panic(err)
	}

	j, err := jade.Parse("jade_tp", string(buf))

	if err != nil {
		panic(err)
	}

	w.Write([]byte(j))
}

/* This makes a closure suitable for passing to the martini framework */
func (s *LigoServer) MakeWrapper(method func(p *ligolib.Project, v url.Values) (interface{}, error)) func(w http.ResponseWriter, r *http.Request) {

	return func(w http.ResponseWriter, r *http.Request) {
		s.APIWrapper(method, w, r)
	}
}

/*
 * This is a wrapper function useful for most of the API endpoints:
 * it parses the "metrics_def" CGI parameter and grabs tries to grab the
 * right project object for the metric. This it calls |method| as a callback
 * and translates the results of |method| into JSON.
 */
func (s *LigoServer) APIWrapper(method func(p *ligolib.Project, v url.Values) (interface{}, error),
	w http.ResponseWriter, r *http.Request) {

	log.Printf("FULL QUERY: %v", r.URL.String())
	params := r.URL.Query()

	project := s.Projects.Get(params.Get("metrics_def"))

	var err error
	var result interface{}
	if project == nil {
		err = fmt.Errorf("No project: `%v`", params.Get("metrics_def"))
	} else {
		result, err = method(project, params)
	}

	FormatResponse(result, err, w)
}

func FormatResponse(result interface{}, err error, w http.ResponseWriter) {
	var resp GenericResponse
	if err == nil {
		resp.STUFF = result
	} else {
		e_str := fmt.Sprintf("%v", err)
		resp.ERROR = &e_str
		log.Printf("ERROR: %v", err)
	}
	js, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	w.Write(js)
}

func (s *LigoServer) NeverWorks(p *ligolib.Project, v url.Values) (interface{}, error) {
	return nil, errors.New("I'm sorry, dave, I can't do that.")
}

func (s *LigoServer) Details(p *ligolib.Project, v url.Values) (interface{}, error) {
	i, err := strconv.Atoi(v.Get("id"))

	if err != nil {
		return nil, err
	}

	return s.DB.AllDataForPipestance(ligolib.NewStringWhere(v.Get("where")), i)

}

/*
 * List ever metric in a given project.
 */

func (s *LigoServer) ListMetrics(p *ligolib.Project, v url.Values) (interface{}, error) {
	return s.DB.ListAllMetrics(p)
}

/* Produce a table for every defined metric */
func (s *LigoServer) PlotAll(p *ligolib.Project, params url.Values) (interface{}, error) {
	return s.DB.PresentAllMetrics(ligolib.NewStringWhere(params.Get("where")), p)
}

/* Produce data (suitable for table or plot) for a given set of metrics. */
func (s *LigoServer) Plot(p *ligolib.Project, params url.Values) (interface{}, error) {

	if params.Get("columns") == "" {
		return nil, errors.New("No columns to plot!")
	}

	variables := strings.Split(params.Get("columns"), ",")

	sortby := params.Get("sortby")
	if sortby == "" {
		sortby = "-finishdate"
	}

	plot, err := s.DB.GenericChartPresenter(ligolib.NewStringWhere(params.Get("where")),
		p,
		variables,
		sortby)

	return plot, err
}

/*
 * Produce comparison data for two pipestances
 */
func (s *LigoServer) Compare(p *ligolib.Project, params url.Values) (interface{}, error) {

	id1s := params.Get("base")
	id2s := params.Get("new")

	id1, _ := strconv.Atoi(id1s)
	id2, _ := strconv.Atoi(id2s)

	res, err := s.DB.GenericComparePresenter(id1, id2, p)
	return res, err
}

/* List every project. */
func (s *LigoServer) ListProjects(w http.ResponseWriter, r *http.Request) {

	v := GenericResponse{nil, s.Projects.List()}
	js, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	w.Write(js)

}

/* Reload projects from disk */
func (s *LigoServer) Reload(w http.ResponseWriter, r *http.Request) {
	s.Projects.Reload()
	http.Redirect(w, r, "/views/unified.jade", 302)

}

func (s *LigoServer) UploadTempProject(w http.ResponseWriter, r *http.Request) {

	//err := r.ParseMultipartForm(1024*1024);
	//if (err != nil) {
	//		log.Printf("UHOH: %v", err);
	//	}
	log.Printf("STUFFSTUFF: %v", *r)
	json_txt := r.PostFormValue("project_def")
	log.Printf("New project def: %v", json_txt)

	project_key, err := s.Projects.NewTempProject(json_txt)
	if err != nil {
		FormatResponse(nil, err, w)
		return
	}

	FormatResponse(map[string]interface{}{"project_id": project_key}, nil, w)
}

func (s *LigoServer) DownloadProject(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	project := s.Projects.Get(params.Get("metrics_def"))

	FormatResponse(map[string]interface{}{"project_def": project}, nil, w)
}
