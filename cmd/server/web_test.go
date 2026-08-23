package main

import (
	"strings"
	"testing"
)

// The server ships the web client embedded, so one loopback port serves both
// the API and the UI. The app shell is at / and /index.html, built assets are
// served by their hashed names, and every other path keeps the API's JSON 404
// shape — the client uses no router, so a non-file path is not a frontend
// route, and an operator command must never be reachable as a page.
func TestWebClientIsServedFromTheSamePort(t *testing.T) {
	srv := testServer(t)

	// The app shell at the root.
	res := srv.do(t, "GET", "/", nil)
	if res.status != 200 {
		t.Fatalf("GET / = %d, want 200:\n%s", res.status, res.body)
	}
	if ct := res.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(string(res.body), "<div id=\"root\"></div>") {
		t.Error("GET / does not contain the app mount point — this does not look like the client")
	}

	// /index.html is the same shell.
	if res := srv.do(t, "GET", "/index.html", nil); res.status != 200 {
		t.Errorf("GET /index.html = %d, want 200", res.status)
	}

	// A hashed asset from the build is served with its real bytes. The name is
	// discovered from the embedded index.html rather than hardcoded: vite's
	// content hash changes with every build.
	root := srv.do(t, "GET", "/", nil)
	asset := ""
	for _, f := range strings.Fields(string(root.body)) {
		if strings.Contains(f, "/assets/") {
			// f is src="/assets/index-....js"></script> — take the attribute
			// value: after src=" and up to the next quote.
			if a, ok := strings.CutPrefix(f, "src=\""); ok {
				if a, _, ok := strings.Cut(a, "\""); ok {
					asset = a
				}
			}
			break
		}
	}
	if asset == "" {
		t.Fatal("no /assets/ reference in the served index.html")
	}
	res = srv.do(t, "GET", asset, nil)
	if res.status != 200 {
		t.Fatalf("GET %s = %d, want 200", asset, res.status)
	}
	if ct := res.header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("asset Content-Type = %q, want javascript", ct)
	}

	// A path that is neither a file nor an API route keeps the API error shape.
	res = srv.do(t, "GET", "/v1/ingest", nil)
	if res.status != 404 {
		t.Errorf("GET /v1/ingest = %d, want 404 (operator command must not be a page)", res.status)
	}
	res.errText(t)

	// An unknown non-API path is also the JSON 404, not a served page.
	res = srv.do(t, "GET", "/nope", nil)
	if res.status != 404 {
		t.Errorf("GET /nope = %d, want the API 404 shape", res.status)
	}
	res.errText(t)
}
