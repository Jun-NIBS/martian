//
// Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
//
// mrp webserver.
//
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/go-martini/martini"
	"github.com/martini-contrib/binding"
	"html/template"
	"io/ioutil"
	"margo/core"
	"net/http"
	"os"
	"path"
	"strings"
)

//=============================================================================
// Page and form structs.
//=============================================================================
type GraphPage struct {
	Container string
	Pname     string
	Psid      string
	Admin     bool
}

type MetadataForm struct {
	Path string
	Name string
}

func runWebServer(uiport string, rt *core.Runtime, pipestance *core.Pipestance) {
	//=========================================================================
	// Configure server.
	//=========================================================================
	m := martini.New()
	r := martini.NewRouter()
	m.Use(martini.Recovery())
	m.Use(martini.Static(core.RelPath("../web/res"), martini.StaticOptions{"", true, "index.html", nil}))
	m.Use(martini.Static(core.RelPath("../web/client"), martini.StaticOptions{"", true, "index.html", nil}))
	m.MapTo(r, (*martini.Routes)(nil))
	m.Action(r.Handle)
	app := &martini.ClassicMartini{m, r}

	//=========================================================================
	// Page renderers.
	//=========================================================================
	app.Get("/", func() string {
		tmpl, _ := template.New("graph.html").Delims("[[", "]]").ParseFiles(core.RelPath("../web/templates/graph.html"))
		var doc bytes.Buffer
		tmpl.Execute(&doc, &GraphPage{"runner", pipestance.Pname(), pipestance.Psid(), true})
		return doc.String()
	})

	//=========================================================================
	// API endpoints.
	//=========================================================================

	// Get graph nodes.
	app.Get("/api/get-nodes/:container/:pname/:psid",
		func(p martini.Params) string {
			data := []interface{}{}
			for _, node := range pipestance.Node().AllNodes() {
				data = append(data, node.Serialize())
			}
			bytes, _ := json.Marshal(data)
			return string(bytes)
		})

	// Get metadata file contents.
	app.Post("/api/get-metadata/:container/:pname/:psid", binding.Bind(MetadataForm{}),
		func(body MetadataForm, p martini.Params) string {
			if strings.Index(body.Path, "..") > -1 {
				return "'..' not allowed in path."
			}
			data, err := ioutil.ReadFile(path.Join(body.Path, "_"+body.Name))
			if err != nil {
				return err.Error()
			}
			return string(data)
		})

	// Restart failed stage.
	app.Post("/api/restart/:container/:pname/:psid/:fqname",
		func(p martini.Params) string {
			node := pipestance.Node().Find(p["fqname"])
			node.RestartFromFailed()
			return ""
		})

	//=========================================================================
	// Start webserver.
	//=========================================================================
	core.LogInfo("webserv", "Serving UI at http://localhost:%s", uiport)
	if err := http.ListenAndServe(":"+uiport, app); err != nil {
		// Don't continue starting if we detect another instance running.
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
