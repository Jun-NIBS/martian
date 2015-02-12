(function() {
  var app, callApi, callApiWithConfirmation;

  app = angular.module('app', ['ui.bootstrap']);

  callApiWithConfirmation = function($scope, $http, $url) {
    var id, _ref;
    $scope.showbutton = false;
    id = window.prompt("Please type the sample ID to confirm");
    if (id === ((_ref = scope.selsample) != null ? _ref.id.toString() : void 0)) {
      return callApi($scope, $http, $url);
    } else {
      window.alert("Incorrect sample id");
      return $scope.showbutton = true;
    }
  };

  callApi = function($scope, $http, $url) {
    var _ref;
    $scope.showbutton = false;
    return $http.post($url, {
      id: (_ref = $scope.selsample) != null ? _ref.id.toString() : void 0
    }).success(function(data) {
      $scope.refreshSamples();
      if (data) {
        return window.alert(data.toString());
      }
    });
  };

  app.controller('MartianRunCtrl', function($scope, $http, $interval) {
    $scope.admin = admin;
    $scope.urlprefix = admin ? '/admin' : '';
    $scope.selsample = null;
    $scope.showbutton = true;
    $http.get('/api/get-metasamples').success(function(data) {
      return $scope.samples = data;
    });
    $scope.refreshSamples = function() {
      return $http.get('/api/get-metasamples').success(function(data) {
        $scope.samples = data;
        return $scope.showbutton = true;
      });
    };
    $scope.selectSample = function(sample) {
      var s, _i, _len, _ref, _ref1;
      $scope.selsample = sample;
      _ref = $scope.samples;
      for (_i = 0, _len = _ref.length; _i < _len; _i++) {
        s = _ref[_i];
        s.selected = false;
      }
      $scope.selsample.selected = true;
      return $http.post('/api/get-metasample-callsrc', {
        id: (_ref1 = $scope.selsample) != null ? _ref1.id.toString() : void 0
      }).success(function(data) {
        if ($scope.selsample != null) {
          return _.assign($scope.selsample, data);
        }
      });
    };
    $scope.invokeAnalysis = function() {
      return callApi($scope, $http, '/api/invoke-metasample-analysis');
    };
    $scope.archiveSample = function() {
      return callApiWithConfirmation($scope, $http, '/api/archive-metasample');
    };
    $scope.unfailSample = function() {
      return callApi($scope, $http, '/api/restart-metasample-analysis');
    };
    $scope.wipeSample = function() {
      return callApiWithConfirmation($scope, $http, '/api/wipe-metasample');
    };
    $scope.killSample = function() {
      return callApiWithConfirmation($scope, $http, '/api/kill-metasample');
    };
    if (admin) {
      return $interval((function() {
        return $scope.refreshSamples();
      }), 5000);
    }
  });

}).call(this);
