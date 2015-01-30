#
# Copyright (c) 2014 10X Genomics, Inc. All rights reserved.
#
# Angular controllers for martian runner main UI.
#

app = angular.module('app', ['ui.bootstrap'])

callApiWithConfirmation = ($scope, $url) ->
    $scope.showbutton = false
    id = window.prompt("Please type the sample ID to confirm")
    if id == scope.selsample?.id.toString()
        callApi($scope, $url)
    else
        window.alert("Incorrect sample id")

callApi = ($scope, $url) ->
    $scope.showbutton = false
    $http.post($url, { id: $scope.selsample?.id.toString() }).success((data) ->
        $scope.refreshSamples()
        if data then window.alert(data.toString())
    )

app.controller('MartianRunCtrl', ($scope, $http, $interval) ->
    $scope.admin = admin
    $scope.urlprefix = if admin then '/admin' else ''

    $scope.selsample = null
    $scope.showbutton = true
   
    $http.get('/api/get-metasamples').success((data) ->
        $scope.samples = data
    )

    $scope.refreshSamples = () ->
        $http.get('/api/get-metasamples').success((data) ->
            $scope.samples = data
        )

    $scope.selectSample = (sample) ->
        $scope.selsample = sample
        for s in $scope.samples
            s.selected = false
        $scope.selsample.selected = true
        $http.post('/api/get-metasample-callsrc', { id: $scope.selsample?.id.toString() }).success((data) ->
            if $scope.selsample? then  _.assign($scope.selsample, data)
        )

    $scope.invokeAnalysis = () ->
        callApi($scope, '/api/invoke-metasample-analysis')

    $scope.archiveSample = () ->
        callApi($scope, '/api/archive-metasample')

    $scope.unfailSample = () ->
        callApi($scope, '/api/restart-metasample-analysis')

    $scope.wipeSample = () ->
        callApiWithConfirmation($scope, '/api/wipe-metasample')

    $scope.killSample = () ->
        callApiWithConfirmation($scope, '/api/kill-metasample')

    # Only admin pages get auto-refresh.
    if admin then $interval((() -> $scope.refreshSamples()), 5000)
)
