package main

import (
	"context"
	"errors"
	"testing"

	"github.com/zachpmanson/chainmail/internal/hassles"
)

func TestHasslesHTTPInvalidLeaseProviderReleasesAdmission(t *testing.T) {
	for _, kind := range []string{"error-with-release", "nil-store", "nil-release"} {
		t.Run(kind, func(t *testing.T) {
			h, f, loc := hassleHarness(t)
			base := injectHassleLease(t, h)
			chooseHassles(t, h, loc)
			in := groupHTTPCreate(t, h, loc, syntheticGroupSchedule())
			released := 0
			h.leaseHassles = func(context.Context) (hassles.GroupStore, func(), error) {
				release := func() { released++ }
				switch kind {
				case "error-with-release":
					return base, release, errors.New("unavailable")
				case "nil-store":
					return nil, release, nil
				default:
					return base, nil, nil
				}
			}
			if r := groupHTTPPost(t, h, in); r.status != 409 || f.writes != 0 {
				t.Fatal("invalid lease admitted", r.status, string(r.body))
			}
			want := 1
			if kind == "nil-release" {
				want = 0
			}
			if released != want {
				t.Fatal("provider release count", released, want)
			}
			if !h.hasslesMu.TryLock() {
				t.Fatal("process admission leaked")
			}
			h.hasslesMu.Unlock()
		})
	}
}
