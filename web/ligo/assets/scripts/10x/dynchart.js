// Copyright (c) 2016 10X Genomics, Inc. All rights reserved.

"use strict";

var global_chart;
var global_table;
var global_table_data;
var global_compare;
var global_metrics_db;
var global_metrics_table;

var global_view_state;


function main() {
	global_view_state = new(ViewState);
	console.log("HELLO WORLD"); 
	google.charts.load('current', {'packages':['corechart', 'table']});
	google.charts.setOnLoadCallback(function() {

		global_chart= new google.visualization.LineChart(document.getElementById('plot1'));
		global_table = new google.visualization.Table(document.getElementById('table1'));
		global_compare = new google.visualization.Table(document.getElementById('compare1'));
		global_metrics_table= new google.visualization.Table(document.getElementById('list1'));
		google.visualization.events.addListener(global_metrics_table, 'select', global_view_state.metrics_list_click);

		//pickwindow("table")
		var p = getParameterByName("params");
		if (p != null && p != "") {
			global_view_state.ReconstituteFromURL(p);

		}
		global_view_state.render();
	});
	

	setup_project_dropdown();

}

function project_dropdown_click(x) {
	console.log(this);
	console.log(event)

	//document.getElementById("metricset").value = event.target.textContent
	changeproject(event.target.textContent);

}


function setup_project_dropdown() {
	$.getJSON("/api/list_metric_sets", function(data) {
		var pd = $("#projects_dropdown")

		for (var i = 0; i < data.length; i++) {

			var ng = document.createElement('li');
			ng.textContent = data[i];
			ng.onclick = project_dropdown_click;
			console.log(ng.textContent);
			pd.append(ng);
		}
	})
}

function changeproject(p) {
	global_view_state.project = p;
	update_model_from_ui();
	global_view_state.render();
}

function changetablemode(mode) {
	update_model_from_ui()
	global_view_state.table_mode = mode;
	global_view_state.render();

}
function pickwindow(mode) {
	update_model_from_ui()
	global_view_state.mode = mode;
	global_view_state.render();
}

function update() {
	update_model_from_ui()
	global_view_state.render();

}



function ViewState() {
	this.mode = "table";
	this.table_mode = "";
	this.where = "";
	this.project = "Default.json";
	this.compareidnew= null;
	this.compareidold= null;
	this.chartx = null;
	this.charty = null;
	this.sample_search = null;

	return this;
}


ViewState.prototype.GetURL = function() {
	var url = document.location;
	var str = url.host + url.pathname + "?params=" +
		encodeURIComponent(JSON.stringify(this));
	return str;
}
ViewState.prototype.ReconstituteFromURL = function(p) {
	var p = decodeURIComponent(p);

	var parsed = JSON.parse(p);
	
	var ks = Object.keys(parsed);

	for (var i = 0; i < ks.length; i++) {
		this[ks[i]] = parsed[ks[i]]
	}
}



ViewState.prototype.render = function() {
	$("#table").hide();
	$("#compare").hide();
	$("#plot").hide();
	$("#help").hide();
	clear_error_box();

	var w = this.mode;

	/* Special logic for handling the compare button. If you don't have exactly two
	 * rows selected, don't compare. If you have run row selected, redo the table view
	 * with selecting rows with the same sampleid
	 */
	if (w == "compare") {
		if (!this.compareidnew || !this.compareidold) {
			set_error_box("Please select two rows to compare. Then click compare again.")
			//var wc=(get_data_at_row(global_table_data, "sampleid", selected[0].row));
			if (this.compareidold) {
				this.where = "sampleid=" + this.sample_search
				console.log(this.where);
			}

			this.table_update();
			$("#table").show();
		} else{
			this.compare_update();
			$("#compare").show();
		}
	}
	else {

		$("#" + w).show();

		if (w == "table") {
			this.table_update();
		}

		if (w == "plot") {
			$.getJSON("/api/list_metrics?metrics_def=" + this.project, function(data) {
				global_metrics_db = data.ChartData;
				var mdata = google.visualization.arrayToDataTable(global_metrics_db);
				global_metrics_table.draw(mdata, {})
			})

			this.chart_update();

		}
	}
	$("#project_cur").text(this.project);
	$("#myurl").text(this.GetURL());
}

/*
 * Render the compare view */
ViewState.prototype.compare_update = function() {

	//var selected = global_table.getSelection();
	//console.log(selected)
	//var idold = get_data_at_row(global_table_data, "test_reports.id", selected[0].row);
	//var idnew= get_data_at_row(global_table_data, "test_reports.id", selected[1].row);

	
	/*var url = "/api/compare?base=" + document.getElementById("idold").value +
		"&new=" + document.getElementById("idnew").value +
		"&metrics_def=met1.json"
		*/
	var url = "/api/compare?base=" + this.compareidold +
		"&new=" + this.compareidnew+ 
		"&metrics_def=" + this.project;
	
	console.log(url)
	$.getJSON(url, function(data) {
		console.log(data);
		var gdata = google.visualization.arrayToDataTable(data.ChartData);
		var options = {allowHtml:true};
		colorize_table(data.ChartData,gdata)
		global_compare.draw(gdata, options)


	})
}

/* Render the metric view */
ViewState.prototype.table_update = function()  { 
	var where = this.where;

	var mode = this.table_mode;
	if (mode=="metrics") {
		var url = "/api/plotall?where=" + where 
	} else {
		var url = "/api/plot?where=" + where + "&columns=test_reports.id,SHA,userid,finishdate,sampleid,comments"
	}

	url += "&metrics_def=" + this.project;

	$.getJSON(url, function(data) {
		global_table_data = data;
		console.log(data);
		var gdata = google.visualization.arrayToDataTable(data.ChartData);
		var options = {width: 1200};
		global_table.draw(gdata, options)
	})
}

/*
 * Handle a click on the table of metrics in the chart page
 */
ViewState.prototype.metrics_list_click = function() {
	var y = document.getElementById("charty");
	
	var sel = global_metrics_table.getSelection();
	var v = "";
	for (var i = 0; i < sel.length; i++) {
		if (v != "") {
			v = v + ",";
		}
		var idx = global_metrics_table.getSelection()[i].row;

		v = v  +global_metrics_db[idx+1];
	}
	y.value = v;
}

function update_model_from_ui() {
	var v = global_view_state;
	v.chartx = document.getElementById("chartx").value;
	v.charty = document.getElementById("charty").value;
	v.where = document.getElementById("where").value;
	var selected = global_table.getSelection();
	if (selected[0]) {
		v.sample_search=(get_data_at_row(global_table_data, "sampleid", selected[0].row));

		v.compareidold= get_data_at_row(global_table_data, "test_reports.id", selected[0].row);
	} else {
		v.compareidold = null;
	}
	if (selected[1]) {
		v.compareidnew= get_data_at_row(global_table_data, "test_reports.id", selected[1].row);
	} else {
		v.compareidnew = null;
	}

}

/*
 * Update the chart.
 */
ViewState.prototype.chart_update = function() {
	var x = this.chartx;
	var y = this.charty;
	var where = this.where

	var url = "/api/plot?where="+encodeURIComponent(where)+
		"&columns=" + encodeURIComponent(x) + "," + encodeURIComponent(y) +
		"&metrics_def=" + this.project;

	console.log(url);
	$.getJSON(url, function(data) {
		console.log("GOTDATA!");
		console.log(data)
		var gdata = google.visualization.arrayToDataTable(data.ChartData);
		var options = {title:data.Name};
		global_chart.draw(gdata, options);


	})
}

/*
 * Extract data from a specific row and a named columns from a chartdata-like
 * object.
 */
function get_data_at_row(data, columnname, rownumber) {
	var labels = data.ChartData[0];

	var index;
	for (var i = 0; i < labels.length; i++) {
		if (labels[i] == columnname) {
			index = i;
			break;
		}
	}

	return data.ChartData[rownumber+1][index];
}

/*
 * Set colorization for the comparison page.
 */
function colorize_table(data, datatable) {
	var diff_index;
	var labels = data[0];

	/* Figure out which column is called "diff" */
	for (var i = 0; i < labels.length; i++) {
		if (labels[i] == 'Diff') {
			diff_index= i;
			break;
		}
	}

	/* Look at every row, if its diff column is falst, then color
	 * everything in that row red.
	 */
	for (var i = 1; i < data.length; i++) {
		var di = i - 1;
		
		if (data[i][diff_index] === false) {
			for (var j = 0; j < labels.length; j++) {
				datatable.setProperty(di, j, 'style', 'color:red;')
			}
		}
	}
}

function set_error_box(s) {
	$("#errortext").text(s);
	$("#errorbox").show();
}

function clear_error_box() {
	$("#errorbox").hide();
}


/* Shamelessly stolen from stackoverflow */
function getParameterByName(name, url) {
    if (!url) url = window.location.href;
    name = name.replace(/[\[\]]/g, "\\$&");
    var regex = new RegExp("[?&]" + name + "(=([^&#]*)|&|#|$)"),
        results = regex.exec(url);
    if (!results) return null;
    if (!results[2]) return '';
    return decodeURIComponent(results[2].replace(/\+/g, " "));
}
main();



