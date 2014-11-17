#
# Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
#
# Angular controllers for mario editor main UI.
#

app = angular.module('app', ['ui.bootstrap','ngClipboard'])
app.filter('shorten',  () -> (s) ->
    s = s + ""
    if s.length < 71 then return s
    else return s.substr(0, 30) + " ... " + s.substr(s.length - 50)
)

renderGraph = ($scope, $compile) ->
    g = new dagreD3.Digraph()
    for node in _.values($scope.nodes)
        node.label = node.name
        g.addNode(node.fqname, node)
    for node in _.values($scope.nodes)
        for edge in node.edges
            # This code supports display of old, non-fqname _finalstates.
            from = edge.from
            to = edge.to
            if from not in $scope.nodes
                from = $scope.simpleNodes[from].fqname
            if to not in $scope.nodes
                to = $scope.simpleNodes[to].fqname
            g.addEdge(null, from, to, {})
    (new dagreD3.Renderer()).zoom(false).run(g, d3.select("g"));
    maxX = 0.0
    d3.selectAll("g.node").each((id) ->
        d3.select(this).classed(g.node(id).type, true)
        d3.select(this).attr('ng-click', "selectNode('#{id}')")
        d3.select(this).attr('ng-class', "[node.fqname=='#{id}'?'seled':'',nodes['#{id}'].state]")
        xCoord = parseFloat(d3.select(this).attr('transform').substr(10).split(',')[0])
        if xCoord > maxX
            maxX = xCoord
    )
    maxX += 100
    if maxX < 750.0
        maxX = 750.0
    scale = 750.0 / maxX
    d3.selectAll("g#top").each((id) ->
        d3.select(this).attr('transform', 'translate(5,5) scale('+scale+')')
    )
    d3.selectAll("g.node.stage rect").each((id) ->
        d3.select(this).attr('rx', 20).attr('ry', 20))
    d3.selectAll("g.node.pipeline rect").each((id) ->
        d3.select(this).attr('rx', 0).attr('ry', 0))
    $compile(angular.element(document.querySelector('#top')).contents())($scope) 

# Main Controller.
app.controller('MarioGraphCtrl', ($scope, $compile, $http, $interval) ->
    $scope.pname = pname
    $scope.psid = psid
    $scope.admin = admin
    $scope.adminstyle = adminstyle
    $scope.urlprefix = if adminstyle then '/admin' else '/'

    $http.get("/api/get-state/#{container}/#{pname}/#{psid}").success((state) ->
        $scope.simpleNodes = _.indexBy(state.nodes, 'name')
        $scope.nodes = _.indexBy(state.nodes, 'fqname')
        $scope.info = state.info
        $scope.error = state.error
        renderGraph($scope, $compile)
    )

    $scope.id = null
    $scope.forki = 0
    $scope.chunki = 0
    $scope.mdviews = { fork:'', split:'', join:'', chunk:'' }
    $scope.showRestart = true
    $scope.showLog = false

    # Only admin pages get auto-refresh.
    if admin
        $scope.stopRefresh = $interval(() ->
            $scope.refresh()
        , 30000)

    $scope.copyToClipboard = () ->
        return ''

    $scope.selectNode = (id) ->
        $scope.id = id
        $scope.node = $scope.nodes[id]
        $scope.forki = 0
        $scope.chunki = 0
        $scope.mdviews = { fork:'', split:'', join:'', chunk:'' }

    $scope.restart = () ->
        $scope.showRestart = false
        $http.post("/api/restart/#{container}/#{pname}/#{psid}/#{$scope.node.fqname}").success((data) ->
            $scope.stopRefresh = $interval(() ->
                $scope.refresh()
            , 3000)
        ).error(() ->
            $scope.showRestart = true
            alert('mrp is no longer running.\n\nPlease run mrp again with the --noexit option to continue running the pipeline.')
        )

    $scope.selectMetadata = (view, name, path) ->
        $http.post("/api/get-metadata/#{container}/#{pname}/#{psid}", { path:path, name:name }, { transformResponse: (d) -> d }).success((metadata) ->
            $scope.mdviews[view] = metadata
        )

    $scope.refresh = () ->
        $http.get("/api/get-state/#{container}/#{pname}/#{psid}").success((state) ->
            $scope.simpleNodes = _.indexBy(state.nodes, 'name')
            $scope.nodes = _.indexBy(state.nodes, 'fqname')
            if $scope.id then $scope.node = $scope.nodes[$scope.id]
            $scope.info = state.info
            $scope.showRestart = true
            $scope.error = state.error
        ).error(() ->
            console.log('Server responded with an error for /api/get-state, so stopping auto-refresh.')
            $interval.cancel($scope.stopRefresh)
        )
)
