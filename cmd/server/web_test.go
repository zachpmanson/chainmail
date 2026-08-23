package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// The server ships the web client embedded, so one loopback port serves both
// the API and the UI. The app shell is at / and /index.html, built assets are
// served by their hashed names, and every other non-/v1/ path is the shell
// too — the client owns the routes, so a deep link or a mistyped path lands
// on the client, which renders the page or its own 404 view. Only the /v1/
// surface keeps the JSON 404 shape, so an operator command can never be
// reachable as a page.
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

	// Every client route is the shell too: a deep link to a saved page, a bare
	// route prefix, an unknown path (the client's 404 view answers that one),
	// and a look-alike that used to 404 server-side.
	for _, p := range []string{"/view", "/view/demo", "/view/solar-install-quote", "/viewfoo", "/nope", "/viwe/typo"} {
		res := srv.do(t, "GET", p, nil)
		if res.status != 200 {
			t.Errorf("GET %s = %d, want 200 (every non-/v1 path is the shell)", p, res.status)
		}
		if !strings.Contains(string(res.body), "<div id=\"root\"></div>") {
			t.Errorf("GET %s does not serve the app shell", p)
		}
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

	// An undocumented API path keeps the API error shape: an operator command
	// must never be reachable as a page.
	res = srv.do(t, "GET", "/v1/ingest", nil)
	if res.status != 404 {
		t.Errorf("GET /v1/ingest = %d, want 404 (operator command must not be a page)", res.status)
	}
	res.errText(t)
}

// The render route is only as good as the read half of it: a page saved by
// POST /v1/spec under a name must come back from GET /v1/specs/{name} exactly
// as it was built, so /view/<name> is reloadable after a refresh or a reboot.
func TestASavedSpecRoundTripsByName(t *testing.T) {
	srv := testServer(t)

	var req specRequest
	if err := json.Unmarshal(specBody(extAda1), &req); err != nil {
		t.Fatal(err)
	}
	req.Name = "solar-install-quote"
	blob, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	built := srv.do(t, "POST", "/v1/spec", blob)
	if built.status != 200 {
		t.Fatalf("POST /v1/spec with a name = %d, want 200:\n%s", built.status, built.body)
	}

	got := srv.do(t, "GET", "/v1/specs/solar-install-quote", nil)
	if got.status != 200 {
		t.Fatalf("GET /v1/specs/solar-install-quote = %d, want 200:\n%s", got.status, got.body)
	}
	if !bytes.Equal(got.body, built.body) {
		t.Error("the saved page differs from what the build returned — the render route would show a different page than the one just built")
	}

	// A name that was never saved is a 404 naming the missing page.
	if res := srv.do(t, "GET", "/v1/specs/never-built", nil); res.status != 404 {
		t.Errorf("GET /v1/specs/never-built = %d, want 404", res.status)
	}

	// A name that would escape the specs dir is refused, not written or read.
	req.Name = "../escape"
	if blob, err = json.Marshal(req); err != nil {
		t.Fatal(err)
	}
	if res := srv.do(t, "POST", "/v1/spec", blob); res.status != 400 {
		t.Errorf("POST /v1/spec with name %q = %d, want 400", req.Name, res.status)
	}

	// A blank name still builds without saving: the old behaviour is the default.
	req.Name = ""
	if blob, err = json.Marshal(req); err != nil {
		t.Fatal(err)
	}
	if res := srv.do(t, "POST", "/v1/spec", blob); res.status != 200 {
		t.Errorf("POST /v1/spec without a name = %d, want 200", res.status)
	}
}
