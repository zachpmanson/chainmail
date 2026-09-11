package main

// The status row is the one place a broken credential shows up, so a probe that
// asks the wrong backend is worse than no probe: it reports a missing CLI as a
// mail problem, which is what this row did on every host whose mail had already
// moved to the library. These tests pin which backend is asked and how the answer
// is worded. The snapshot file itself is internal/status's to test.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/status"
)

// stubMail stands in for a transport. The probe's job is to ask the configured
// backend and word its answer, not to be any particular one of them, and neither
// real one can be a fixture: a docket that answers is a script, and a Gmail token
// is a live credential.
type stubMail struct{ err error }

func (s stubMail) Search(string, int, string) ([]mailingest.Envelope, mailingest.Page, error) {
	return nil, mailingest.Page{}, s.err
}

func (s stubMail) Read(string) (mailingest.Message, error) {
	return mailingest.Message{}, s.err
}

func TestTheMailProbeAsksTheBackendTheIngestReadsThrough(t *testing.T) {
	for _, tc := range []struct {
		why, backend, want, wantStatus string
		err                            error
	}{
		{
			why: "the library answers", backend: backendGmail, wantStatus: status.OK,
			want: "the in-process Gmail backend answered",
		},
		{
			why: "the library refuses", backend: backendGmail, wantStatus: status.Needs,
			err:  errors.New("no token on disk"),
			want: "the in-process Gmail backend could not answer — no token on disk",
		},
		{
			why: "docket answers", backend: backendDocket, wantStatus: status.OK,
			want: "docket answered; mail session is authenticated",
		},
		{
			why: "docket refuses", backend: backendDocket, wantStatus: status.Needs,
			err:  errors.New(`exec: "docket": executable file not found in $PATH`),
			want: `docket could not answer — exec: "docket": executable file not found in $PATH`,
		},
	} {
		t.Run(tc.why, func(t *testing.T) {
			got := probeMail(statusOpts{backend: tc.backend,
				openMail: func() (mailingest.Mailbox, error) { return stubMail{err: tc.err}, nil }})

			if got.ID != "mail" || got.Label != "Gmail" {
				t.Errorf("row = %+v, want the mail row labelled Gmail: the label names the service, and naming the transport is how the screen came to say docket on a host that does not use it", got)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if !strings.Contains(got.Detail, tc.want) {
				t.Errorf("detail = %q, want it to contain %q", got.Detail, tc.want)
			}
		})
	}
}

// A backend that cannot even be opened is the same finding as one that refuses a
// read, worded by the transport that failed. gmailclient.New is where a host with
// no token is caught, and reporting that as a docket problem is this row's bug.
func TestAFailedOpenIsWordedByTheBackendThatFailed(t *testing.T) {
	for _, tc := range []struct{ backend, want, notWant string }{
		{backendGmail, "the in-process Gmail backend could not answer", "could not answer — exec"},
		{backendDocket, "docket could not answer", "in-process"},
	} {
		got := probeMail(statusOpts{backend: tc.backend,
			openMail: func() (mailingest.Mailbox, error) { return nil, errors.New("no credential") }})

		if got.Status != status.Needs {
			t.Errorf("%s: status = %q, want %q", tc.backend, got.Status, status.Needs)
		}
		if !strings.Contains(got.Detail, tc.want) {
			t.Errorf("%s: detail = %q, want it to contain %q", tc.backend, got.Detail, tc.want)
		}
		if strings.Contains(got.Detail, tc.notWant) {
			t.Errorf("%s: detail = %q, want it not to contain %q", tc.backend, got.Detail, tc.notWant)
		}
	}
}

// The switch itself is the fix, so it is checked against the real opener as well
// as a stub: that the stores are empty is the test's own doing, so neither
// transport reaches anything, and each says what it is missing in its own words.
func TestTheRealOpenerPicksTheBackendItWasGiven(t *testing.T) {
	empty := t.TempDir()
	// Each of the three because the stores resolve XDG first and HOME second: a
	// variable left over from the developer's own session would hand the probe a
	// real token and make this test depend on who ran it.
	t.Setenv("HOME", empty)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(empty, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(empty, "state"))

	library := probeMail(statusOpts{backend: backendGmail})
	if library.Status != status.Needs {
		t.Fatalf("the library with no token: status = %q, want %q", library.Status, status.Needs)
	}
	if !strings.Contains(library.Detail, "the in-process Gmail backend could not answer") {
		t.Errorf("the library's detail = %q, want the library to own the failure", library.Detail)
	}
	if strings.Contains(library.Detail, "executable file not found") {
		t.Errorf("the library's detail blames a missing binary: %q", library.Detail)
	}

	legacy := probeMail(statusOpts{backend: backendDocket, bin: filepath.Join(empty, "no-such-docket")})
	if legacy.Status != status.Needs {
		t.Fatalf("docket with no binary: status = %q, want %q", legacy.Status, status.Needs)
	}
	if !strings.Contains(legacy.Detail, "docket could not answer") {
		t.Errorf("docket's detail = %q, want docket to own the failure", legacy.Detail)
	}
}

// A -backend that names neither transport is refused rather than read as one:
// "gmailx" silently becoming gmail is how a legacy host gets told its credential
// is missing instead of that it mistyped a flag.
func TestABackendThatIsNeitherTransportIsRefused(t *testing.T) {
	for _, b := range []string{"", backendGmail, backendDocket} {
		if err := validBackend(b); err != nil {
			t.Errorf("validBackend(%q) = %v, want nil", b, err)
		}
	}
	err := validBackend("gmailx")
	if err == nil {
		t.Fatal("validBackend(\"gmailx\") accepted a backend that does not exist")
	}
	for _, want := range []string{"gmailx", backendGmail, backendDocket} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
