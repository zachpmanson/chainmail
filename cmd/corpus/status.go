package main

// The probe behind `corpus status`. Each backend is asked, shallowly, whether
// it is logged in, and the answer is written to <corpus>.status.json for the
// server to serve. The probes stay shallow on purpose: mail and Slack are
// asked whether their current stored credential still answers, and the archive
// is opened and read, but nothing is fetched and nothing is written.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zachpmanson/chainmail/internal/embed"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/slackingest"
	"github.com/zachpmanson/chainmail/internal/status"
)

type statusOpts struct {
	backend, bin, archive, url, model string
	out                               string
	// openMail overrides how the mail backend is opened. Which transport the
	// probe asks is the part worth testing, and neither real one can stand in
	// for a test: a docket that answers is a fixture, and a Gmail token is a
	// live credential. Tests swap the opener; nothing else sets it.
	openMail func() (mailingest.Mailbox, error)
}

// probeTimeout bounds every per-backend probe. Enough for a local daemon and a
// local binary to answer, and short enough that a hung one does not stall the
// rest: the whole point of the screen is that it is quick to load.
const probeTimeout = 5 * time.Second

func runStatus(path string, o statusOpts) error {
	if err := validBackend(o.backend); err != nil {
		return err
	}
	snap, err := writeStatusSnapshot(path, o)
	if err != nil {
		return err
	}

	fmt.Printf("status: %s\n", o.out)
	for _, s := range snap.Services {
		fmt.Printf("  %-6s %-12s %s\n", s.ID, s.Status, s.Detail)
	}
	return nil
}

// writeStatusSnapshot probes every backend and writes the snapshot file beside
// the corpus, returning what it recorded. runStatus and the slurp's tail both
// reach the probes through here, so a probe is never implemented twice.
func writeStatusSnapshot(path string, o statusOpts) (status.Snapshot, error) {
	if o.out == "" {
		o.out = status.FileName(path)
	}
	snap := status.Snapshot{CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	snap.Services = append(snap.Services, probeMail(o), probeSlack(o), probeEmbed(o))

	if err := os.MkdirAll(filepath.Dir(o.out), 0o700); err != nil {
		return snap, err
	}
	if err := os.WriteFile(o.out, snap.Marshal(), 0o660); err != nil {
		return snap, err
	}
	return snap, nil
}

// runStatusTail gives the slurp a one-liner behind the same probes the
// standalone command uses: probe, write, report, and carry a write failure as
// the phase's outcome.
func runStatusTail(path string, o slurpOpts) error {
	oo := statusOpts{backend: o.backend, bin: o.bin, archive: o.archive,
		url: o.embedURL, model: o.embedModel}
	_, err := writeStatusSnapshot(path, oo)
	return err
}

// probeMail asks the mail backend this corpus is configured to read through for
// one message's envelope. A reply proves the stored credential still authorises;
// a refusal is logged as needs-auth with the backend's own words underneath,
// which is where the operator's next step is named.
//
// It asks what -backend names, the same switch `slurp` and `ingest mail` take,
// because both read one credential: a probe that asks the docket CLI on a host
// whose mail arrives through the library reports a missing binary as a mail
// problem, which is what this row used to do. The label names the service and
// the detail names the transport, so the screen says which one is in use without
// claiming Gmail is only reachable one way.
func probeMail(o statusOpts) status.Service {
	svc := status.Service{ID: "mail", Label: "Gmail"}
	open := o.openMail
	if open == nil {
		open = func() (mailingest.Mailbox, error) { return openMail(o) }
	}
	box, err := open()
	if err == nil {
		err = probeSearch(box)
	}
	if err != nil {
		svc.Status = status.Needs
		svc.Detail = mailRefusal(o, err)
		return svc
	}
	svc.Status = status.OK
	svc.Detail = mailAnswer(o)
	return svc
}

// openMail opens the transport -backend names: the shared docket library in this
// process for "gmail", or a docket subprocess for "docket" — the legacy path a
// machine that has not moved still reads its mail through.
func openMail(o statusOpts) (mailingest.Mailbox, error) {
	if o.backend == backendDocket {
		return mailingest.Client{Bin: o.bin}, nil
	}
	return gmailclient.New()
}

// probeSearch asks for one message and gives up at probeTimeout. The library
// transport takes its context from gmailclient.New (a background one), so there
// is no way to shorten it from here; the bound is imposed from outside instead.
// A backend that has not answered by then is reported as unreachable rather than
// left holding the slurp's tail open — the abandonment is deliberate, and it
// costs nothing, because the process that owns the goroutine is about to exit.
func probeSearch(box mailingest.Mailbox) error {
	done := make(chan error, 1)
	go func() {
		_, _, err := box.Search("in:anywhere", 1, "")
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(probeTimeout):
		return fmt.Errorf("no answer within %s", probeTimeout)
	}
}

// mailRefusal and mailAnswer name the transport, which the label no longer can:
// "Gmail" is the service, and which way the bytes move is the detail's job. The
// library's own words for a missing token are "run `docket auth login`", which is
// the wrong next step on a host whose grant comes from the served sign-in — the
// row exists to name that step, so it names this host's.
func mailRefusal(o statusOpts, err error) string {
	if o.backend == backendDocket {
		return "docket could not answer — " + err.Error()
	}
	return "the in-process Gmail backend could not answer — " + err.Error() +
		"; this host's grant is the served sign-in (the Sign in with Google bar), not a CLI"
}

func mailAnswer(o statusOpts) string {
	if o.backend == backendDocket {
		return "docket answered; mail session is authenticated"
	}
	return "the in-process Gmail backend answered; the stored token authorises a read"
}

// probeSlack reads the slackdump archive, the stored conversation itself. It
// owns the credentials, so no credential ever crosses this process: if the
// archive opens, the conversation is readable, which is the only claim an
// archive-based pipeline can make. A missing or unreadable archive is the
// needs-auth state — run slackdump's browser import once and let it.
func probeSlack(o statusOpts) status.Service {
	svc := status.Service{ID: "slack", Label: "Slack (slackdump)"}
	arc, err := slackingest.OpenArchive(o.archive)
	if err != nil {
		svc.Status = status.Needs
		svc.Detail = err.Error()
	} else {
		arc.Close()
		svc.Status = status.OK
		svc.Detail = "archive reads at " + o.archive
	}
	return svc
}

// probeEmbed asks the embedding daemon whether it answers and holds the
// requested model. "Logged in" is the wrong word for it, but it is the third
// backend a working screen needs, so it reports in the same row: reachable is
// ok, missing model is a setup step, and an unreachable daemon is down.
func probeEmbed(o statusOpts) status.Service {
	svc := status.Service{ID: "embed", Label: "Embedding daemon (ollama)"}
	e := &embed.Ollama{BaseURL: o.url, Name: o.model, Dimension: embed.DefaultDim,
		Client: &http.Client{Timeout: probeTimeout}}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(probeTimeout))
	defer cancel()
	ready, err := e.Available(ctx)
	switch {
	case err == nil && ready:
		svc.Status = status.OK
		svc.Detail = fmt.Sprintf("%s answers and holds %s", o.url, o.model)
	case err == nil:
		svc.Status = status.Needs
		svc.Detail = fmt.Sprintf("%s is running but has no %s: run `ollama pull %s`",
			o.url, o.model, o.model)
	default:
		svc.Status = status.Down
		svc.Detail = err.Error()
	}
	return svc
}
