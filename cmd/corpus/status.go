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
	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/chainmail/internal/slackingest"
	"github.com/zachpmanson/chainmail/internal/status"
)

type statusOpts struct {
	bin, archive, url, model string
	out                      string
}

// probeTimeout bounds every per-backend probe. Enough for a local daemon and a
// local binary to answer, and short enough that a hung one does not stall the
// rest: the whole point of the screen is that it is quick to load.
const probeTimeout = 5 * time.Second

func runStatus(path string, o statusOpts) error {
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
	oo := statusOpts{bin: o.bin, archive: o.archive, url: o.embedURL, model: o.embedModel}
	_, err := writeStatusSnapshot(path, oo)
	return err
}

// probeMail asks docket, the Gmail proxy, for one message's envelope. A reply
// proves the OAuth session still authorises; a refusal is logged as needs-auth
// with docket's own words underneath, which is where the operator's next step
// ("run docket, authorise there") is named.
func probeMail(o statusOpts) status.Service {
	svc := status.Service{ID: "mail", Label: "Gmail (docket)"}
	if _, _, err := (mailingest.Client{Bin: o.bin}).Search("in:anywhere", 1, ""); err != nil {
		svc.Status = status.Needs
		svc.Detail = "docket could not answer — " + err.Error()
	} else {
		svc.Status = status.OK
		svc.Detail = "docket answered; mail session is authenticated"
	}
	return svc
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
