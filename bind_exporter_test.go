// Copyright 2020 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus-community/bind_exporter/bind"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

var (
	serverStats = []string{
		`bind_boot_time_seconds 1.626325868e+09`,
		`bind_incoming_queries_total{type="A"} 128417`,
		`bind_incoming_requests_total{opcode="QUERY"} 37634`,
		`bind_responses_total{result="Success"} 29313`,
		`bind_query_duplicates_total 216`,
		`bind_query_errors_total{error="Dropped"} 237`,
		`bind_query_errors_total{error="Failure"} 2950`,
		`bind_query_recursions_total 60946`,
		`bind_zone_transfer_rejected_total 3`,
		`bind_zone_transfer_success_total 25`,
		`bind_zone_transfer_failure_total 1`,
		`bind_recursive_clients 76`,
		`bind_tcp_clients_total 150`,
		`bind_tcp_high_water 200`,
		`bind_config_time_seconds 1.626325868e+09`,
		`bind_response_rcodes_total{rcode="NOERROR"} 989812`,
		`bind_response_rcodes_total{rcode="NXDOMAIN"} 33958`,
	}
	viewStats = []string{
		`bind_resolver_cache_rrsets{type="A",view="_default"} 34324`,
		`bind_resolver_queries_total{type="CNAME",view="_default"} 28`,
		`bind_resolver_response_errors_total{error="FORMERR",view="_bind"} 0`,
		`bind_resolver_response_errors_total{error="FORMERR",view="_default"} 42906`,
		`bind_resolver_response_errors_total{error="NXDOMAIN",view="_bind"} 0`,
		`bind_resolver_response_errors_total{error="NXDOMAIN",view="_default"} 16707`,
		`bind_resolver_response_errors_total{error="OtherError",view="_bind"} 0`,
		`bind_resolver_response_errors_total{error="OtherError",view="_default"} 20660`,
		`bind_resolver_response_errors_total{error="SERVFAIL",view="_bind"} 0`,
		`bind_resolver_response_errors_total{error="SERVFAIL",view="_default"} 7596`,
		`bind_resolver_response_lame_total{view="_default"} 9108`,
		`bind_resolver_query_duration_seconds_bucket{view="_default",le="0.01"} 38334`,
		`bind_resolver_query_duration_seconds_bucket{view="_default",le="0.1"} 113122`,
		`bind_resolver_query_duration_seconds_bucket{view="_default",le="0.5"} 182658`,
		`bind_resolver_query_duration_seconds_bucket{view="_default",le="0.8"} 187375`,
		`bind_resolver_query_duration_seconds_bucket{view="_default",le="1.6"} 188409`,
		`bind_resolver_query_duration_seconds_bucket{view="_default",le="+Inf"} 227755`,
		`bind_zone_serial{view="_default",zone_name="TEST_ZONE"} 123`,
		`bind_resolver_response_errors_total{error="REFUSED",view="_bind"} 17`,
		`bind_resolver_response_errors_total{error="REFUSED",view="_default"} 5798`,
	}
	taskStats = []string{
		`bind_tasks_running 8`,
		`bind_worker_threads 16`,
	}
)

func TestBindExporterJSONClient(t *testing.T) {
	bindExporterTest{
		server:  newJSONServer(),
		groups:  []bind.StatisticGroup{bind.ServerStats, bind.ViewStats, bind.TaskStats},
		version: "json",
		include: combine([]string{`bind_up 1`}, serverStats, viewStats, taskStats),
	}.run(t)
}

func TestBindExporterV3Client(t *testing.T) {
	bindExporterTest{
		server:  newV3Server(),
		groups:  []bind.StatisticGroup{bind.ServerStats, bind.ViewStats, bind.TaskStats},
		version: "xml.v3",
		include: combine([]string{`bind_up 1`}, serverStats, viewStats, taskStats),
	}.run(t)
}

func TestBindExporterBindFailure(t *testing.T) {
	bindExporterTest{
		server:  httptest.NewServer(http.HandlerFunc(http.NotFound)),
		version: "xml.v3",
		include: []string{`bind_up 0`},
		exclude: serverStats,
	}.run(t)
}

type bindExporterTest struct {
	server  *httptest.Server
	groups  []bind.StatisticGroup
	version string
	include []string
	exclude []string
}

func (b bindExporterTest) run(t *testing.T) {
	defer b.server.Close()

	o, err := collect(NewExporter(log.NewNopLogger(), b.version, b.server.URL, time.Second, b.groups))
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range b.include {
		if !bytes.Contains(o, []byte(m)) {
			t.Errorf("expected to find metric %q in output\n%s", m, o)
		}
	}
	for _, m := range b.exclude {
		if bytes.Contains(o, []byte(m)) {
			t.Errorf("expected to not find metric %q in output\n%s", m, o)
		}
	}
}

func combine(s ...[]string) []string {
	r := []string{}
	for _, i := range s {
		r = append(r, i...)
	}
	return r
}

func collect(c prometheus.Collector) ([]byte, error) {
	r := prometheus.NewRegistry()
	if err := r.Register(c); err != nil {
		return nil, err
	}
	m, err := r.Gather()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := expfmt.NewEncoder(&b, expfmt.FmtText)
	for _, f := range m {
		if err := enc.Encode(f); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

func newV3Server() *httptest.Server {
	m := map[string]string{
		"/xml/v3/server": "fixtures/xml/server.xml",
		"/xml/v3/status": "fixtures/xml/status.xml",
		"/xml/v3/tasks":  "fixtures/xml/tasks.xml",
		"/xml/v3/zones":  "fixtures/xml/zones.xml",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := m[r.RequestURI]; ok {
			http.ServeFile(w, r, f)
		} else {
			http.NotFound(w, r)
		}
	}))
}

func newJSONServer() *httptest.Server {
	m := map[string]string{
		"/json/v1/server": "fixtures/json/server.json",
		"/json/v1/tasks":  "fixtures/json/tasks.json",
		"/json/v1/zones":  "fixtures/json/zones.json",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := m[r.RequestURI]; ok {
			http.ServeFile(w, r, f)
		} else {
			http.NotFound(w, r)
		}
	}))
}
