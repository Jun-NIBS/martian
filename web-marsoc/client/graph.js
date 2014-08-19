(function() {
  var app, renderGraph;

  app = angular.module('app', ['ui.bootstrap', 'ngClipboard']);

  renderGraph = function($scope, $compile) {
    var edge, g, node, _i, _j, _k, _len, _len1, _len2, _ref, _ref1, _ref2;
    g = new dagreD3.Digraph();
    _ref = _.values($scope.nodes);
    for (_i = 0, _len = _ref.length; _i < _len; _i++) {
      node = _ref[_i];
      node.label = node.name;
      g.addNode(node.name, node);
    }
    _ref1 = _.values($scope.nodes);
    for (_j = 0, _len1 = _ref1.length; _j < _len1; _j++) {
      node = _ref1[_j];
      _ref2 = node.edges;
      for (_k = 0, _len2 = _ref2.length; _k < _len2; _k++) {
        edge = _ref2[_k];
        g.addEdge(null, edge.from, edge.to, {});
      }
    }
    (new dagreD3.Renderer()).run(g, d3.select("g"));
    d3.selectAll("g.node").each(function(id) {
      d3.select(this).classed(g.node(id).type, true);
      d3.select(this).attr('ng-click', "selectNode('" + id + "')");
      return d3.select(this).attr('ng-class', "[node.name=='" + id + "'?'seled':'',nodes['" + id + "'].state]");
    });
    d3.selectAll("g.node.stage rect").each(function(id) {
      return d3.select(this).attr('rx', 20).attr('ry', 20);
    });
    d3.selectAll("g.node.pipeline rect").each(function(id) {
      return d3.select(this).attr('rx', 0).attr('ry', 0);
    });
    return $compile(angular.element(document.querySelector('#top')).contents())($scope);
  };

  app.controller('MarioGraphCtrl', function($scope, $compile, $http, $interval) {
    $scope.pname = pname;
    $scope.psid = psid;
    $scope.admin = admin;
    $scope.urlprefix = admin ? '/admin' : '/';
    $http.get("/api/get-nodes/" + container + "/" + pname + "/" + psid).success(function(nodes) {
      $scope.nodes = _.indexBy(nodes, 'name');
      return renderGraph($scope, $compile);
    });
    $scope.id = null;
    $scope.forki = 0;
    $scope.chunki = 0;
    $scope.mdviews = {
      fork: '',
      split: '',
      join: '',
      chunk: ''
    };
    $scope.showRestart = true;
    if (admin) {
      $interval((function() {
        return $scope.refresh();
      }), 5000);
    }
    $scope.copyToClipboard = function() {
      return '';
    };
    $scope.selectNode = function(id) {
      $scope.id = id;
      $scope.node = $scope.nodes[id];
      $scope.forki = 0;
      $scope.chunki = 0;
      return $scope.mdviews = {
        fork: '',
        split: '',
        join: '',
        chunk: ''
      };
    };
    $scope.restart = function() {
      $scope.showRestart = false;
      return $http.post("/api/restart/" + container + "/" + pname + "/" + psid + "/" + $scope.node.fqname).success(function(data) {
        return console.log(data);
      });
    };
    $scope.selectMetadata = function(view, name, path) {
      return $http.post("/api/get-metadata/" + container + "/" + pname + "/" + psid, {
        path: path,
        name: name
      }, {
        transformResponse: function(d) {
          return d;
        }
      }).success(function(metadata) {
        return $scope.mdviews[view] = metadata;
      });
    };
    $scope.step = function() {
      return $http.get('/step').success(function(nodes) {
        if ($scope.id) {
          return $scope.selectNode($scope.id);
        }
      });
    };
    return $scope.refresh = function() {
      return $http.get("/api/get-nodes/" + container + "/" + pname + "/" + psid).success(function(nodes) {
        $scope.nodes = _.indexBy(nodes, 'name');
        if ($scope.id) {
          $scope.node = $scope.nodes[$scope.id];
        }
        return $scope.showRestart = true;
      });
    };
  });

}).call(this);
