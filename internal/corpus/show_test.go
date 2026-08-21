package corpus

import (
	"errors"
	"testing"
	"time"
)

// Search reports the entry that MATCHED, not the chain root, so naming any
// message must return the whole conversation. Resolving only downwards from the
// named entry would return a fragment and look like the whole thing.
func TestChainIsReachableFromAnyMemberNotJustTheRoot(t *testing.T) {
	s := open(t)
	mk := func(ext string, min int, parent int64, body string) int64 {
		e := Entry{
			Source: SourceMail, ExtID: ext, Kind: "message",
			TS: time.Date(2026, 8, 1, 9, min, 0, 0, time.UTC), BodyText: body,
		}
		res, err := s.Put(e, &Mail{MessageID: ext}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if parent != 0 {
			if err := s.SetParent(res.ID, parent); err != nil {
				t.Fatal(err)
			}
		}
		return res.ID
	}
	root := mk("a", 0, 0, "the first")
	mid := mk("b", 1, root, "the second")
	mk("c", 2, mid, "the third")

	for _, from := range []string{"a", "b", "c"} {
		got, err := s.Chain(from)
		if err != nil {
			t.Fatalf("from %s: %v", from, err)
		}
		if len(got) != 3 {
			t.Errorf("from %s: got %d entries, want the whole chain of 3", from, len(got))
		}
		// Time order, not insertion or traversal order.
		for i := 1; i < len(got); i++ {
			if got[i].TS.Before(got[i-1].TS) {
				t.Errorf("from %s: entry %d is out of time order", from, i)
			}
		}
	}
}

// The provenance is the reason this view exists: a message quoted in three
// forwards has three sightings, and that is the evidence it mattered.
func TestShowCarriesEverySighting(t *testing.T) {
	s := open(t)
	host, err := s.Put(Entry{
		Source: SourceMail, ExtID: "host", Kind: "message",
		TS: time.Unix(1_700_000_100, 0), BodyText: "the forward",
	}, &Mail{MessageID: "host"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, created, err := s.PutQuoted(Entry{
		Source: SourceMail, ExtID: "quote:abc", Kind: "message",
		TS: time.Unix(1_700_000_000, 0), BodyText: "the original",
	})
	if err != nil || !created {
		t.Fatalf("PutQuoted: created=%v err=%v", created, err)
	}
	if err := s.Sight(id, host.ID, "quoted", "depth 2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Show("quote:abc")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Quoted {
		t.Error("an entry recovered from quoted text must report itself as such")
	}
	if len(got.Sightings) != 1 {
		t.Fatalf("sightings = %d, want 1", len(got.Sightings))
	}
	if g := got.Sightings[0]; g.Kind != "quoted" || g.SeenIn != "host" || g.Detail != "depth 2" {
		t.Errorf("sighting = %+v", g)
	}
}

// An unknown id must be distinguishable from an empty result, so a caller can
// tell "no such thing" from "nothing to say about it".
func TestShowReportsAMissingIDAsSuch(t *testing.T) {
	s := open(t)
	if _, err := s.Show("mail:<nope@x>"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := s.Chain("mail:<nope@x>"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Chain err = %v, want ErrNotFound", err)
	}
}
