#
# Copyright (c) 2014 10X Technologies, Inc. All rights reserved.
#
# Angular controllers for mario editor main UI.
#

app = angular.module('app', ['ui.bootstrap','ngClipboard'])
app.filter('shorten',  () -> (str) ->
    if length(str) < 61
        str
    else
        str.substr(0, 30) #+ "..." + str.substr(length(str) - 30)
)

renderGraph = ($scope, $compile) ->
    g = new dagreD3.Digraph()
    for node in _.values($scope.nodes)
        node.label = node.name
        g.addNode(node.name, node)
    for node in _.values($scope.nodes)
        for edge in node.edges
            g.addEdge(null, edge.from, edge.to, {})
    (new dagreD3.Renderer()).run(g, d3.select("g"))
    maxX = 0.0
    d3.selectAll("g.node").each((id) ->
        d3.select(this).classed(g.node(id).type, true)
        d3.select(this).attr('ng-click', "selectNode('#{id}')")
        d3.select(this).attr('ng-class', "[node.name=='#{id}'?'seled':'',nodes['#{id}'].state]")
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
    $scope.urlprefix = if admin then '/admin' else '/'

    $http.get("/api/get-nodes/#{container}/#{pname}/#{psid}").success((nodes) ->
        $scope.nodes = _.indexBy(nodes, 'name')
        renderGraph($scope, $compile)
    )

    $scope.id = null
    $scope.forki = 0
    $scope.chunki = 0
    $scope.mdviews = { fork:'', split:'', join:'', chunk:'' }
    $scope.showRestart = true

    # Only admin pages get auto-refresh.
    if admin then $interval((() -> $scope.refresh()), 5000)

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
            console.log(data)
        )

    $scope.selectMetadata = (view, name, path) ->
        $http.post("/api/get-metadata/#{container}/#{pname}/#{psid}", { path:path, name:name }, { transformResponse: (d) -> d }).success((metadata) ->
            $scope.mdviews[view] = metadata
        )

    $scope.step = () ->
        $http.get('/step').success((nodes) ->
            if $scope.id then $scope.selectNode($scope.id)
        )

    $scope.refresh = () ->
        $http.get("/api/get-nodes/#{container}/#{pname}/#{psid}").success((nodes) ->
            $scope.nodes = _.indexBy(nodes, 'name')
            if $scope.id then $scope.node = $scope.nodes[$scope.id]
            $scope.showRestart = true
        )
)
