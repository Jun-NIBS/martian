// Copyright (c) 2016 10X Genomics, Inc. All rights reserved.

/*
 * This implements the core of the "sere2" webserver for viewing information
 * int he sere2 db.
 */
package sere2web

import (
	"github.com/go-martini/martini"
	"log"
	"martian/sere2lib"
	"net/http"
)

type Sere2Server struct {
	DB *sere2lib.CoreConnection
	//WebService * martini.Martini
	WebBase string
	v       http.Handler
}

/*
 * Setup a server.
 * |port| is the port to run on
 * db is an instance of the database connection (and other config)
 * webbase is the root directory of the web routes and assets.  Relative to the
 *   git root, it is web/sere2
 */
func SetupServer(port int, db *sere2lib.CoreConnection, webbase string) {
	s2s := new(Sere2Server)
	s2s.DB = db
	s2s.WebBase = webbase

	martini.Root = webbase
	m := martini.Classic()
	//s2s.WebService = m;

	/* Serve static assets ouf of the assets directory */
	m.Get("/assets/**", http.StripPrefix("/assets/", http.FileServer(http.Dir(webbase+"/assets"))))

	/* Process and serve views from here. We match view names to file names */
	m.Get("/views/**", s2s.Viewer)

	/* API endpoints to do useful things */
	m.Get("/api/slice", s2s.Slice)

	/* Start it up! */
	m.Run()
}

func (s *Sere2Server) vv(w http.ResponseWriter, r *http.Request) {
	log.Printf("TRY: %v", r)
	s.v.ServeHTTP(w, r)
}

func (s *Sere2Server) Viewer(w http.ResponseWriter, r *http.Request) {

	w.Write([]byte("Hello!"))
}

func (s *Sere2Server) Slice(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Goodbye!"))
}
