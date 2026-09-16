package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/media"
)

// The mailbox-reaching half of the media design, at the handler. Every test
// here injects runMediaPull, because what is worth asserting is the wiring —
// the switch, the entry, the shape of the answer — and the walk itself is
// covered against a fake transport in internal/media.

func TestMediaPullDisabledDefaultsToForbidden(t *testing.T) {
	// Same posture as -slurp, and for the same reason: a server that was not
	// told -media must not expose a door that spends mailbox round trips. No
	// override here — a fresh harness leaves mediaEnabled=false, which is the
	// state every deployed unit starts in.
	h := testServer(t)
	res := h.do(t, "POST", "/v1/media/pull", []byte(`{"entry":"`+extAda1+`"}`))
	if res.status != http.StatusForbidden {
		t.Fatalf("disabled media pull status = %d, want 403", res.status)
	}
	if got := res.errText(t); !strings.Contains(got, "-media") {
		t.Errorf("error = %q, want it to name the -media switch that would enable it", got)
	}
}

func TestMediaPullAsksForTheEntryItWasGivenAndReturnsTheOutcome(t *testing.T) {
	h := testServer(t)
	h.mediaEnabled = true
	calls := 0
	h.runMediaPull = func(_ context.Context, entry string) (media.Result, error) {
		calls++
		if entry != extAda1 {
			t.Errorf("runMediaPull got entry = %q, want the ext id the caller named, %q", entry, extAda1)
		}
		return media.Result{
			Wanted: 3, Pulled: 1, Skipped: 1, Failed: 1, Bytes: 4096,
			Items: []media.Item{
				{Name: "shed.csv", Source: "mail", SHA: "sha-of-the-bytes", Bytes: 4096},
				{Name: "roof.mp4", Source: "mail", Reason: "too_large"},
				{Name: "gone.pdf", Source: "mail", Err: errors.New("gmail: part not found")},
			},
		}, nil
	}
	res := h.do(t, "POST", "/v1/media/pull", []byte(`{"entry":"`+extAda1+`"}`))
	if res.status != http.StatusOK {
		t.Fatalf("media pull status = %d, want 200: %s", res.status, res.body)
	}
	if calls != 1 {
		t.Fatalf("runMediaPull called %d times, want once", calls)
	}
	got := decode[mediaResponse](t, res)
	if got.Wanted != 3 || got.Pulled != 1 || got.Skipped != 1 || got.Failed != 1 || got.Bytes != 4096 {
		t.Errorf("counts = %+v, want 3 wanted / 1 pulled / 1 skipped / 1 failed / 4096 bytes", got)
	}
	if len(got.Files) != 3 {
		t.Fatalf("files = %d rows, want one per file considered", len(got.Files))
	}
	if got.Files[0].SHA != "sha-of-the-bytes" {
		t.Errorf("pulled file sha = %q, want the digest the store wrote", got.Files[0].SHA)
	}
	// The two ways a file does not arrive are separate fields, because only one
	// of them means "do not ask again".
	if got.Files[1].Reason != "too_large" || got.Files[1].Error != "" {
		t.Errorf("skipped file = %+v, want a recorded reason and no error", got.Files[1])
	}
	if !strings.Contains(got.Files[2].Error, "part not found") || got.Files[2].Reason != "" {
		t.Errorf("failed file = %+v, want an error and no recorded reason", got.Files[2])
	}
	// An empty walk still has to say `files: []` rather than null: the contract
	// requires the array, and a client iterating it must not have to guard.
	loadAPI(t).assert(t, "MediaPullResponse", res.body)
}

func TestMediaPullWithNothingToFetchIsAnEmptyAnswer(t *testing.T) {
	// Every file stored, or every file already declined: the pull considers
	// nothing and says so, rather than reporting an error for having no work.
	h := testServer(t)
	h.mediaEnabled = true
	h.runMediaPull = func(_ context.Context, _ string) (media.Result, error) {
		return media.Result{}, nil
	}
	res := h.do(t, "POST", "/v1/media/pull", []byte(`{"entry":"`+extAda1+`"}`))
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.status, res.body)
	}
	got := decode[mediaResponse](t, res)
	if got.Wanted != 0 || len(got.Files) != 0 {
		t.Errorf("result = %+v, want nothing wanted and no rows", got)
	}
	if !strings.Contains(string(res.body), `"files":[]`) {
		t.Errorf("body = %s, want an empty array rather than a null", res.body)
	}
}

func TestMediaPullRefusals(t *testing.T) {
	h := testServer(t)
	h.mediaEnabled = true
	h.runMediaPull = func(_ context.Context, _ string) (media.Result, error) {
		t.Error("a refused pull must not reach the mailbox")
		return media.Result{}, nil
	}
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"no entry", `{}`, http.StatusBadRequest},
		{"a blank entry", `{"entry":"   "}`, http.StatusBadRequest},
		// A misspelled field is a caller reading a different contract, and this
		// call spends mailbox round trips: refusing is cheaper than guessing.
		{"an unknown field", `{"entry":"` + extAda1 + `","images":true}`, http.StatusBadRequest},
		{"an entry the corpus has never seen", `{"entry":"` + extNone + `"}`, http.StatusNotFound},
	} {
		res := h.do(t, "POST", "/v1/media/pull", []byte(tc.body))
		if res.status != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", tc.name, res.status, tc.want, res.body)
		}
	}
}

func TestMediaPullFailureIsBadGatewayWithTheError(t *testing.T) {
	h := testServer(t)
	h.mediaEnabled = true
	h.runMediaPull = func(_ context.Context, _ string) (media.Result, error) {
		return media.Result{}, errors.New("opening gmail: no token on disk")
	}
	res := h.do(t, "POST", "/v1/media/pull", []byte(`{"entry":"`+extAda1+`"}`))
	if res.status != http.StatusBadGateway {
		t.Fatalf("failed pull status = %d, want 502", res.status)
	}
	// The credential is the thing to look at, so the words have to reach the
	// caller: a 502 that only said "media pull failed" would send the reader to
	// the server's logs to learn it.
	if got := res.errText(t); !strings.Contains(got, "no token on disk") {
		t.Errorf("error = %q, want it to carry the transport's own refusal", got)
	}
	loadAPI(t).assert(t, "Error", res.body)
}
