package mailingest

import (
	"errors"
	"testing"
)

func TestBoundedRunCannotAdvanceAnUnfinishedBootstrap(t *testing.T) {
	s, f := store(t), newHistoryBox()
	f.fakeBox = newFake(4, 1)
	f.failAfter = 1
	if _, err := Ingest(s, f, "in:anywhere", Bound{}); err == nil {
		t.Fatal("wanted interruption")
	}
	historyState(t, s, "legacy", "", "100")
	f.failAfter = -1
	if _, err := Ingest(s, f, "in:anywhere", Bound{Max: 1}); err != nil {
		t.Fatal(err)
	}
	historyState(t, s, "legacy", "", "")
	r, err := Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Reads != 4 {
		t.Fatalf("bootstrap reused bounded progress: %+v", r)
	}
	if got := copyLabels(t, s, "legacy", "p1m0"); len(got) != 1 || got[0] != "INBOX" {
		t.Fatalf("bounded-read copy lost its labels: %v", got)
	}
	historyState(t, s, "legacy", "110", "")
}

func TestRecoveryFailureRetainsOldCommittedCheckpoint(t *testing.T) {
	for _, failure := range []string{"scan", "replay"} {
		t.Run(failure, func(t *testing.T) {
			s, f := store(t), newHistoryBox()
			checkpoint(t, s, "legacy")
			f.profile = "200"
			f.history = func(start, token string) (HistoryPage, error) {
				if start == "100" {
					return HistoryPage{}, ErrHistoryExpired
				}
				if failure == "replay" {
					return HistoryPage{}, errors.New("interrupted replay")
				}
				return HistoryPage{ID: "210"}, nil
			}
			if failure == "scan" {
				f.readError = "p1m0"
			}
			if _, err := Ingest(s, f, "in:anywhere", Bound{}); err == nil {
				t.Fatal("wanted failure")
			}
			historyState(t, s, "legacy", "100", "200")
			f.readError = ""
			f.history = func(start, token string) (HistoryPage, error) {
				if start != "200" {
					t.Fatal("did not resume recovery capture", start)
				}
				return HistoryPage{ID: "210"}, nil
			}
			if _, err := Ingest(s, f, "in:anywhere", Bound{}); err != nil {
				t.Fatal(err)
			}
			historyState(t, s, "legacy", "210", "")
		})
	}
}

func TestRepeatedHistoryExpiryStopsWithoutCheckpoint(t *testing.T) {
	s, f := store(t), newHistoryBox()
	f.history = func(string, string) (HistoryPage, error) { return HistoryPage{}, ErrHistoryExpired }
	if _, err := Ingest(s, f, "in:anywhere", Bound{}); !errors.Is(err, ErrHistoryExpired) {
		t.Fatalf("error %v", err)
	}
	historyState(t, s, "legacy", "", "100")
	if f.profiles != 2 {
		t.Fatalf("unbounded recovery attempts: %d", f.profiles)
	}
}

func TestIngestRejectsOverlapBeforeAnyMailboxRequest(t *testing.T) {
	s, f := store(t), newHistoryBox()
	release, err := s.LockGmailSync("legacy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Ingest(s, f, "in:anywhere", Bound{}); err == nil {
		t.Fatal("overlap allowed")
	}
	if _, err := IngestIDs(s, f, []string{"old"}); err == nil {
		t.Fatal("ID ingest overlap allowed")
	}
	if f.profiles != 0 || f.searches != 0 || f.reads != 0 {
		t.Fatal("overlapping run contacted Gmail")
	}
}
