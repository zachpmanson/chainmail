package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/media"
	"github.com/zachpmanson/chainmail/internal/spec"
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

// The two ways a pull used to be lost, both of them a reader leaving the page:
// the browser cancels its own fetch on a reload, and the refresh that made the
// files visible was the client's to run afterwards. Together they meant a press
// could spend mailbox round trips, store half a message's files, and leave the
// page reading exactly as it did before — nothing recorded, nothing to see.

func TestAPullOutlivesTheReaderWhoAskedForIt(t *testing.T) {
	h := testServer(t)
	h.mediaEnabled = true

	entered := make(chan struct{})
	release := make(chan struct{})
	var cancelled bool
	h.runMediaPull = func(ctx context.Context, _ string) (media.Result, error) {
		close(entered)
		<-release
		// The fetch takes as long as a mailbox takes. What matters is whether
		// the context it was handed is still alive after the reader has gone.
		cancelled = ctx.Err() != nil
		return media.Result{Wanted: 1, Pulled: 1, Bytes: 4096}, nil
	}

	// The reader's own context: cancelled below, which is what a reload or a
	// closed tab does to the request's context.
	ctx, leave := context.WithCancel(context.Background())
	done := make(chan *response, 1)
	go func() {
		done <- h.doWithContext(t, ctx, "POST", "/v1/media/pull", []byte(`{"entry":"`+extAda1+`"}`))
	}()
	<-entered
	leave()        // the page is reloaded mid-fetch
	close(release) // the fetch finishes anyway
	res := <-done

	if cancelled {
		t.Error("the pull saw the request's cancellation: a reload throws the mailbox round trips away")
	}
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.status, res.body)
	}
}

func TestAPullBringsThePageItWasAskedForUpToDate(t *testing.T) {
	h := testServer(t)
	h.mediaEnabled = true
	// The pull itself is the test's fixture here: the bytes are already linked
	// in the corpus, so reporting them is all a completed fetch leaves behind.
	h.runMediaPull = func(context.Context, string) (media.Result, error) {
		return media.Result{Wanted: 1, Pulled: 1, Bytes: int64(len(shedBytes))}, nil
	}

	// The page a reader has open, as it was saved before the files were
	// fetched: a real refreshable page, with the chip's digest wiped so the
	// page reads the way it does on a corpus that has not pulled yet.
	built := decode[spec.Spec](t, h.do(t, "POST", "/v1/spec",
		[]byte(`{"chains":["`+extAda1+`"],"name":"solar"}`)))
	stripBlobSHA(t, &built)
	writePage(t, h, "solar", built)
	if got := attachmentSHA(t, built, extAda1, "shed.csv"); got != "" {
		t.Fatalf("fixture page carries %q before the pull, want no digest", got)
	}

	res := h.do(t, "POST", "/v1/media/pull",
		[]byte(`{"entry":"`+extAda1+`","name":"solar"}`))
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.status, res.body)
	}
	got := decode[mediaResponse](t, res)
	if got.Spec == nil || got.Report == nil {
		t.Fatal("the pull answered without the page: a reader who reloads would land on the stale one")
	}
	if sha := attachmentSHA(t, *got.Spec, extAda1, "shed.csv"); sha != shedSHA {
		t.Errorf("the page in the response carries %q, want the digest the pull stored, %q", sha, shedSHA)
	}
	// And the saved copy is that same page, which is what a reload lands on:
	// the point of naming the page is that the server, not the browser, is the
	// one that makes the files visible.
	saved := decode[spec.Spec](t, h.do(t, "GET", "/v1/specs/solar", nil))
	if sha := attachmentSHA(t, saved, extAda1, "shed.csv"); sha != shedSHA {
		t.Errorf("the saved page carries %q, want %q — a reload would still show the stale page", sha, shedSHA)
	}
}

func TestAPullRefusesAPageNameThatIsNotOne(t *testing.T) {
	h := testServer(t)
	h.mediaEnabled = true
	h.runMediaPull = func(context.Context, string) (media.Result, error) {
		t.Error("a pull ran for a page name that is a path segment")
		return media.Result{}, nil
	}
	// Checked before any bytes are spent, because the name is a path on the
	// way back out.
	res := h.do(t, "POST", "/v1/media/pull",
		[]byte(`{"entry":"`+extAda1+`","name":"../escape"}`))
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", res.status, res.body)
	}
	if got := res.errText(t); !strings.Contains(got, "use letters, digits") {
		t.Errorf("error = %q, want the name rule it broke", got)
	}
}

// stripBlobSHA clears every stored digest, so a spec reads as a page that was
// saved before the files were fetched.
func stripBlobSHA(t *testing.T, sp *spec.Spec) {
	t.Helper()
	for i := range sp.Messages {
		for j := range sp.Messages[i].Attachments {
			sp.Messages[i].Attachments[j].BlobSHA = ""
		}
	}
}

// writePage puts a page in the server's specs dir, the way a save would.
func writePage(t *testing.T, h *harness, name string, sp spec.Spec) {
	t.Helper()
	blob, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshalling the page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.specs, name+".json"), blob, 0o644); err != nil {
		t.Fatalf("writing the page: %v", err)
	}
}

// attachmentSHA is the digest a page carries for one named attachment, which is
// how a test asks whether the files are visible on the page.
func attachmentSHA(t *testing.T, sp spec.Spec, extID, name string) string {
	t.Helper()
	for _, m := range sp.Messages {
		if m.ExtID != extID {
			continue
		}
		for _, a := range m.Attachments {
			if a.Name == name {
				return a.BlobSHA
			}
		}
	}
	t.Fatalf("page has no %s on %s", name, extID)
	return ""
}
