package main

import (
	"encoding/json"
	"testing"
	"time"
)

// The deploy stamp is the answer to "is what I merged live", so the test is about
// the two facts it has to carry rather than about the shape of the JSON: the
// revision the process was started with, and when it started.
//
// A build nobody labelled must report no revision rather than an empty string —
// the header shows nothing in that case, and `omitempty` is what makes absence
// distinguishable from a revision that happens to be blank.
func TestVersionServesTheRevisionAndTheStart(t *testing.T) {
	srv := testServer(t)

	res := srv.do(t, "GET", "/v1/version", nil)
	if res.status != 200 {
		t.Fatalf("GET /v1/version = %d, want 200:\n%s", res.status, res.body)
	}
	var got versionResponse
	if err := json.Unmarshal(res.body, &got); err != nil {
		t.Fatalf("GET /v1/version is not a version response: %v\n%s", err, res.body)
	}
	if got.Rev != srv.server.rev {
		t.Errorf("rev = %q, want the revision the process was started with (%q)", got.Rev, srv.server.rev)
	}
	at, err := time.Parse(time.RFC3339, got.StartedAt)
	if err != nil {
		t.Fatalf("startedAt = %q, want RFC 3339: %v", got.StartedAt, err)
	}
	if !at.Equal(srv.server.startedAt) {
		t.Errorf("startedAt = %s, want the process's own start (%s)", at, srv.server.startedAt)
	}
	// The stamp is compared against a deploy, so it has to be in a fixed zone
	// rather than whatever the machine's is: a reader in Melbourne and a reader
	// in London should read the same instant.
	if at.Location() != time.UTC {
		t.Errorf("startedAt zone = %s, want UTC", at.Location())
	}
}

// An unlabelled build — a `go run` under the devshell, a build outside nix —
// serves no revision at all, and the header then shows no stamp. A blank-but-
// present rev would make a client render an empty code span and a link to a
// commit named "", which is worse than showing nothing.
func TestVersionOmitsAnUnknownRevision(t *testing.T) {
	srv := testServer(t)
	srv.server.rev = ""

	var raw map[string]json.RawMessage
	res := srv.do(t, "GET", "/v1/version", nil)
	if res.status != 200 {
		t.Fatalf("GET /v1/version = %d, want 200", res.status)
	}
	if err := json.Unmarshal(res.body, &raw); err != nil {
		t.Fatalf("GET /v1/version is not JSON: %v", err)
	}
	if _, ok := raw["rev"]; ok {
		t.Errorf("rev is present for an unlabelled build: %s", res.body)
	}
	if _, ok := raw["startedAt"]; !ok {
		t.Errorf("startedAt is missing: %s", res.body)
	}
}
