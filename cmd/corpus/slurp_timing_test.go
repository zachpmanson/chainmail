package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func TestSlurpTimesEveryOutcomeAndContinues(t *testing.T) {
	broken := func() error { return errors.New("private failure detail") }
	for _, tc := range []struct {
		name, phase string
		outcome     outcome
		change      func(*slurpDeps)
	}{
		{"complete", "mail", outcomeDone, func(*slurpDeps) {}},
		{"incomplete", "mail", outcomeIncomplete, func(d *slurpDeps) {
			d.ingestMail = func(mailOpts) (mailingest.Result, error) {
				return mailingest.Result{Stop: mailingest.StopMax}, nil
			}
		}},
		{"account discovery", "mail", outcomeFailed, func(d *slurpDeps) {
			d.mailAccounts = func() ([]string, error) { return nil, broken() }
		}},
		{"mail failure", "mail", outcomeFailed, func(d *slurpDeps) {
			d.ingestMail = func(mailOpts) (mailingest.Result, error) { return mailingest.Result{}, broken() }
		}},
		{"twins failure", "twins", outcomeFailed, func(d *slurpDeps) { d.twins = func(bool) error { return broken() } }},
		{"repair failure", "repair", outcomeFailed, func(d *slurpDeps) { d.repair = broken }},
		{"dedupe failure", "dedupe", outcomeFailed, func(d *slurpDeps) { d.dedupe = func(bool) error { return broken() } }},
		{"unread failure", "unread", outcomeFailed, func(d *slurpDeps) { d.unread = broken }},
		{"embed failure", "embed", outcomeFailed, func(d *slurpDeps) { d.embed = broken }},
		{"embed skip", "embed", outcomeSkipped, func(d *slurpDeps) {
			d.embedReady = func() (bool, string) { return false, "daemon down" }
		}},
		{"status failure", "status", outcomeFailed, func(d *slurpDeps) { d.status = broken }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			d := (&recorder{mail: covered}).deps()
			// Deterministic clock: every phase costs 1.25s regardless of outcome.
			now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
			d.now = func() time.Time { now = now.Add(1250 * time.Millisecond); return now }
			original := d.ingestMail
			d.ingestMail = func(o mailOpts) (mailingest.Result, error) {
				if !strings.Contains(out.String(), "slurp timing: phase=mail outcome=started elapsed=0s\n") {
					t.Error("mail start not written before the work")
				}
				return original(o)
			}
			tc.change(&d)
			err := runSlurp(&out, opts(t), d)
			if (err != nil) != (tc.outcome == outcomeFailed) {
				t.Fatalf("error = %v, phase outcome = %s", err, tc.outcome)
			}
			for _, phase := range append(append([]slurpPhase{}, slurpOrder...), phaseStatus) {
				wantOutcome := outcomeDone
				if string(phase) == tc.phase {
					wantOutcome = tc.outcome
				}
				for _, want := range []string{
					fmt.Sprintf("slurp timing: phase=%s outcome=started elapsed=0s\n", phase),
					fmt.Sprintf("slurp timing: phase=%s outcome=%s elapsed=1.25s\n", phase, wantOutcome),
				} {
					if strings.Count(out.String(), want) != 1 {
						t.Errorf("want exactly one %q in:\n%s", want, out.String())
					}
				}
			}
			totalOutcome := tc.outcome
			if totalOutcome == outcomeSkipped {
				totalOutcome = outcomeDone
			}
			want := fmt.Sprintf("slurp timing: phase=total outcome=%s elapsed=21.25s\n", totalOutcome)
			if !strings.HasSuffix(out.String(), want) || strings.Count(out.String(), "(elapsed=1.25s)") != 8 {
				t.Errorf("missing total or summary durations:\n%s", out.String())
			}
		})
	}
}

func TestSlurpTimesOnlySelectedPhasesAndMandatoryStatus(t *testing.T) {
	out, err := slurp(t, slurpOpts{only: []string{"embed"}}, &recorder{})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"total", "embed", "status"} {
		if !strings.Contains(out, "slurp timing: phase="+phase+" outcome=started") {
			t.Errorf("missing %s timing", phase)
		}
	}
	if strings.Contains(out, "slurp timing: phase=mail") {
		t.Error("unselected mail phase was reported")
	}
}
