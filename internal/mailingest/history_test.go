package mailingest

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

type historyBox struct {
	*fakeBox
	profile      string
	profiles     int
	historyCalls int
	history      func(string, string) (HistoryPage, error)
	messages     map[string]Message
	readError    string
	onRead       func()
}

func newHistoryBox() *historyBox {
	return &historyBox{fakeBox: newFake(2, 2), profile: "100", messages: map[string]Message{},
		history: func(string, string) (HistoryPage, error) { return HistoryPage{ID: "110"}, nil }}
}

func (f *historyBox) HistoryID() (string, error) {
	f.profiles++
	return f.profile, nil
}

func (f *historyBox) History(start, token string) (HistoryPage, error) {
	f.historyCalls++
	return f.history(start, token)
}

func (f *historyBox) Read(id string) (Message, error) {
	if f.onRead != nil {
		f.onRead()
	}
	f.reads++
	if id == f.readError {
		return Message{}, errors.New("injected read failure")
	}
	if m, ok := f.messages[id]; ok {
		return m, nil
	}
	if id == "gone" {
		return Message{}, ErrMessageGone
	}
	m := msg(id, "alice@example.com", "bob@example.com", "")
	m.Labels = []string{"INBOX"}
	return m, nil
}

func historyState(t *testing.T, s *corpus.Store, account, committed, bootstrap string) {
	t.Helper()
	a, b, err := s.GmailHistory(account)
	if err != nil || a != committed || b != bootstrap {
		t.Fatalf("history = %q/%q, err %v; want %q/%q", a, b, err, committed, bootstrap)
	}
}

func checkpoint(t *testing.T, s *corpus.Store, account string) {
	t.Helper()
	if err := s.BeginGmailBootstrap(account, "100"); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitGmailHistory(account, "100"); err != nil {
		t.Fatal(err)
	}
}

func copyLabels(t *testing.T, s *corpus.Store, account, id string) []string {
	t.Helper()
	var entry int64
	if err := s.DB().QueryRow(`select entry_id from gmail_copies where account_id=? and gmail_id=?`, account, id).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	copies, err := s.GmailCopies(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range copies {
		if c.AccountID == account && c.GmailID == id {
			return c.Labels
		}
	}
	t.Fatal("missing copy")
	return nil
}

func seedMail(t *testing.T, s *corpus.Store, account, id string) {
	t.Helper()
	m := msg(id, "alice@example.com", "bob@example.com", "")
	m.Labels = []string{"INBOX", "Old folder", "UNREAD"}
	if _, err := PutForAccount(s, account, m); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryBootstrapCapturesBeforeScanAndReplaysMailboxChanges(t *testing.T) {
	s, f := store(t), newHistoryBox()
	f.onRead = func() {
		if f.profiles != 1 {
			t.Fatal("read before capture")
		}
	}
	f.history = func(start, token string) (HistoryPage, error) {
		if start != "100" || f.profiles != 1 || f.searches != 2 {
			t.Fatalf("not captured before complete scan: start=%s profiles=%d searches=%d", start, f.profiles, f.searches)
		}
		return HistoryPage{ID: "110", Changed: []string{"arrived-during-scan"}}, nil
	}
	r, err := Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Created != 5 || !r.History || r.Reads != 5 {
		t.Fatalf("result %+v", r)
	}
	historyState(t, s, "legacy", "110", "")
	f.history = func(start, token string) (HistoryPage, error) {
		if start != "110" {
			t.Fatalf("start %s", start)
		}
		return HistoryPage{ID: "120"}, nil
	}
	r, err = Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Reads != 0 || r.Pages != 1 || f.searches != 2 || f.profiles != 1 {
		t.Fatalf("repeat did extra work: %+v", r)
	}
}

func TestHistoryPaginationDeduplicatesAndReplacesLabelsWithoutBodyChanges(t *testing.T) {
	s, f := store(t), newHistoryBox()
	checkpoint(t, s, "legacy")
	seedMail(t, s, "legacy", "old")
	m := msg("old", "alice@example.com", "bob@example.com", "")
	m.Labels = []string{"New folder"}
	f.messages["old"] = m
	f.history = func(start, token string) (HistoryPage, error) {
		if start != "100" {
			t.Fatalf("start %s", start)
		}
		if token == "" {
			return HistoryPage{ID: "130", NextPageToken: "next", Changed: []string{"old", "new", "old"}}, nil
		}
		if token != "next" {
			t.Fatal(token)
		}
		return HistoryPage{ID: "130", Changed: []string{"old", "new"}}, nil
	}
	r, err := Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages != 2 || r.Reads != 2 || r.Created != 1 || r.Changed != 0 || f.searches != 0 {
		t.Fatalf("result %+v", r)
	}
	if got := copyLabels(t, s, "legacy", "old"); !reflect.DeepEqual(got, m.Labels) {
		t.Fatalf("labels %v", got)
	}
	var mirror string
	if err := s.DB().QueryRow(`select labels from mail_detail where gmail_id='old'`).Scan(&mirror); err != nil || mirror != "New folder" {
		t.Fatalf("legacy mirror %q: %v", mirror, err)
	}
	historyState(t, s, "legacy", "130", "")
}

func TestHistoryDeletionRetainsContentAndClearsFolders(t *testing.T) {
	s, f := store(t), newHistoryBox()
	checkpoint(t, s, "legacy")
	for _, id := range []string{"gone", "deleted-only", "survived"} {
		seedMail(t, s, "legacy", id)
	}
	f.history = func(string, string) (HistoryPage, error) {
		return HistoryPage{ID: "120", Changed: []string{"gone", "survived"}, Deleted: []string{"gone", "deleted-only", "survived", "unknown-draft"}}, nil
	}
	if _, err := Ingest(s, f, "in:anywhere", Bound{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gone", "deleted-only"} {
		if got := copyLabels(t, s, "legacy", id); len(got) != 0 {
			t.Fatalf("%s labels %v", id, got)
		}
	}
	var n int
	if err := s.DB().QueryRow(`select count(*) from entries`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("content deleted: %d %v", n, err)
	}
}

func TestHistoryFailureKeepsCheckpointAndReplayAfterRestartIsSafe(t *testing.T) {
	for _, failure := range []string{"page", "read", "upsert"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corpus.db")
			s, err := corpus.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint(t, s, "legacy")
			f := newHistoryBox()
			f.history = func(start, token string) (HistoryPage, error) {
				if token == "" {
					return HistoryPage{ID: "120", NextPageToken: "next", Changed: []string{"a", "b"}}, nil
				}
				if failure == "page" {
					return HistoryPage{}, errors.New("interrupted page")
				}
				return HistoryPage{ID: "120", Changed: []string{"a"}}, nil
			}
			if failure == "read" {
				f.readError = "b"
			}
			if failure == "upsert" {
				if _, err := s.DB().Exec(`create trigger fail_copy before insert on gmail_copies when new.gmail_id='b' begin select raise(abort, 'injected'); end`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Ingest(s, f, "in:anywhere", Bound{}); err == nil {
				t.Fatal("wanted failure")
			}
			historyState(t, s, "legacy", "100", "")
			s.Close()
			s, err = corpus.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.DB().Exec(`drop trigger if exists fail_copy`); err != nil {
				t.Fatal(err)
			}
			f.readError = ""
			f.history = func(start, token string) (HistoryPage, error) {
				if start != "100" {
					t.Fatalf("restart skipped prefix: %s", start)
				}
				return HistoryPage{ID: "120", Changed: []string{"a", "b", "a"}}, nil
			}
			if _, err := Ingest(s, f, "in:anywhere", Bound{}); err != nil {
				t.Fatal(err)
			}
			historyState(t, s, "legacy", "120", "")
			var n int
			if err := s.DB().QueryRow(`select count(*) from gmail_copies`).Scan(&n); err != nil || n != 2 {
				t.Fatalf("replay duplicated: %d %v", n, err)
			}
		})
	}
}

func TestExpiredHistoryReconcilesOldCopiesPastDateFrontier(t *testing.T) {
	s, f := store(t), newHistoryBox()
	checkpoint(t, s, "legacy")
	seedMail(t, s, "legacy", "p1m1")
	seedMail(t, s, "legacy", "deleted-before-history")
	if _, err := Ingest(s, f.fakeBox, "in:anywhere", Bound{}); err != nil {
		t.Fatal(err)
	}
	f.searches, f.reads = 0, 0
	m := msg("p1m1", "alice@example.com", "bob@example.com", "")
	m.Labels = []string{"Remote folder"}
	f.messages[m.ID] = m
	f.profile = "200"
	f.history = func(start, token string) (HistoryPage, error) {
		if start == "100" {
			return HistoryPage{}, ErrHistoryExpired
		}
		if start != "200" {
			t.Fatal(start)
		}
		return HistoryPage{ID: "210", Changed: []string{"arrived-during-recovery"}}, nil
	}
	r, err := Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	if f.searches != 2 || r.Reads != 5 {
		t.Fatalf("recovery stopped at frontier: %+v", r)
	}
	if got := copyLabels(t, s, "legacy", m.ID); !reflect.DeepEqual(got, m.Labels) {
		t.Fatal(got)
	}
	if got := copyLabels(t, s, "legacy", "deleted-before-history"); len(got) != 0 {
		t.Fatal(got)
	}
	historyState(t, s, "legacy", "210", "")
}

func TestInterruptedBootstrapResumesWithOriginalCapture(t *testing.T) {
	s, f := store(t), newHistoryBox()
	f.failAfter = 1
	if _, err := Ingest(s, f, "in:anywhere", Bound{}); err == nil {
		t.Fatal("wanted failure")
	}
	historyState(t, s, "legacy", "", "100")
	f.failAfter = -1
	f.profile = "999"
	f.history = func(start, token string) (HistoryPage, error) {
		if start != "100" {
			t.Fatal("lost bootstrap race window", start)
		}
		return HistoryPage{ID: "110"}, nil
	}
	r, err := Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Resumed || f.profiles != 1 || f.searches != 2 {
		t.Fatalf("not resumed: %+v", r)
	}
	for _, id := range []string{"p0m0", "p0m1", "p1m0", "p1m1"} {
		if got := copyLabels(t, s, "legacy", id); !reflect.DeepEqual(got, []string{"INBOX"}) {
			t.Fatalf("lost resumed scan labels for %s: %v", id, got)
		}
	}
	historyState(t, s, "legacy", "110", "")
}

func TestBoundedAndCustomQueriesDoNotEstablishOrUseHistory(t *testing.T) {
	for _, tc := range []struct {
		query string
		max   int
	}{{"in:anywhere", 1}, {"after:2026/01/01", 0}, {"label:Work", 0}, {"in:anywhere after:2026/01/01", 0}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			s, f := store(t), newHistoryBox()
			r, err := Ingest(s, f, tc.query, Bound{Max: tc.max})
			if err != nil {
				t.Fatal(err)
			}
			if f.profiles != 0 || f.historyCalls != 0 || r.History {
				t.Fatalf("used history: %+v", r)
			}
			historyState(t, s, "legacy", "", "")
		})
	}
}

func TestHistoryKeepsUnreadBadgesCurrentWithoutFullAudit(t *testing.T) {
	for _, account := range []string{"legacy", "other"} {
		t.Run(account, func(t *testing.T) {
			s, f := store(t), newHistoryBox()
			if err := s.PutGmailAccount(corpus.GmailAccount{ID: "other", DisplayName: "Other"}); err != nil {
				t.Fatal(err)
			}
			for _, a := range []string{"legacy", "other"} {
				checkpoint(t, s, a)
				seedMail(t, s, a, "same")
			}
			for i, unread := range []bool{false, true} {
				m := msg("same", "alice@example.com", "bob@example.com", "")
				m.Labels = []string{"INBOX", "Old folder"}
				if unread {
					m.Labels = append(m.Labels, "UNREAD")
				}
				f.messages[m.ID] = m
				end := fmt.Sprint(110 + i*10)
				f.history = func(string, string) (HistoryPage, error) {
					return HistoryPage{ID: end, Changed: []string{m.ID}}, nil
				}
				r, err := IngestForAccount(s, f, "in:anywhere", Bound{}, account)
				if err != nil {
					t.Fatal(err)
				}
				if !r.History || r.Reads != 1 || r.Changed != 0 || f.searches != 0 {
					t.Fatalf("label-only History update did unexpected work: %+v, searches=%d", r, f.searches)
				}
				if got := copyLabels(t, s, account, m.ID); !reflect.DeepEqual(got, m.Labels) {
					t.Fatalf("updated labels = %v, want %v", got, m.Labels)
				}
				for _, a := range []string{"legacy", "other"} {
					hits, err := s.SearchChains(corpus.Query{AccountID: a})
					want := 1
					if a == account && !unread {
						want = 0
					}
					if err != nil || len(hits) != 1 || hits[0].Unread != want {
						t.Fatalf("account %s unread badge: hits=%+v err=%v, want %d", a, hits, err, want)
					}
				}
				historyState(t, s, account, end, "")
			}
			f.history = func(string, string) (HistoryPage, error) { return HistoryPage{ID: "130"}, nil }
			r, err := IngestForAccount(s, f, "in:anywhere", Bound{}, account)
			if err != nil || r.Reads != 0 || !r.History || f.searches != 0 {
				t.Fatalf("unchanged History sync = %+v, %v; searches=%d", r, err, f.searches)
			}
		})
	}
}

func TestHistoryAccountsDoNotShareIDsCheckpointsOrLabels(t *testing.T) {
	s := store(t)
	if err := s.PutGmailAccount(corpus.GmailAccount{ID: "other", DisplayName: "Other"}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"legacy", "other"} {
		checkpoint(t, s, account)
		seedMail(t, s, account, "same")
	}
	f := newHistoryBox()
	m := msg("same", "alice@example.com", "bob@example.com", "")
	m.Labels = []string{"Other folder"}
	f.messages[m.ID] = m
	f.history = func(string, string) (HistoryPage, error) {
		return HistoryPage{ID: "900", Changed: []string{"same"}}, nil
	}
	if _, err := IngestForAccount(s, f, "in:anywhere", Bound{}, "other"); err != nil {
		t.Fatal(err)
	}
	historyState(t, s, "legacy", "100", "")
	historyState(t, s, "other", "900", "")
	if got := copyLabels(t, s, "legacy", "same"); len(got) != 3 {
		t.Fatal(got)
	}
	if got := copyLabels(t, s, "other", "same"); !reflect.DeepEqual(got, m.Labels) {
		t.Fatal(got)
	}
}

func TestMalformedHistoryNeverCommits(t *testing.T) {
	for _, mode := range []string{"loop", "empty", "regressed", "forbidden"} {
		t.Run(mode, func(t *testing.T) {
			s, f := store(t), newHistoryBox()
			checkpoint(t, s, "legacy")
			f.history = func(string, string) (HistoryPage, error) {
				switch mode {
				case "loop":
					return HistoryPage{ID: "120", NextPageToken: "loop"}, nil
				case "regressed":
					return HistoryPage{ID: "99"}, nil
				case "forbidden":
					return HistoryPage{}, errors.New("403")
				default:
					return HistoryPage{}, nil
				}
			}
			if _, err := Ingest(s, f, "in:anywhere", Bound{}); err == nil {
				t.Fatal("wanted error")
			}
			if f.searches != 0 {
				t.Fatal("non-404 triggered scan")
			}
			historyState(t, s, "legacy", "100", "")
		})
	}
}

func TestRepeatHistoryReducesListAndReadCalls(t *testing.T) {
	s, f := store(t), newHistoryBox()
	if _, err := Ingest(s, f, "in:anywhere", Bound{}); err != nil {
		t.Fatal(err)
	}
	before, err := Ingest(s, f.fakeBox, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := Ingest(s, f, "in:anywhere", Bound{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("repeat frontier: pages=%d full_reads=%d elapsed=%s; history: pages=%d full_reads=%d elapsed=%s", before.Pages, before.Reads, before.Elapsed, after.Pages, after.Reads, after.Elapsed)
	if after.Reads >= before.Reads || after.Pages >= before.Pages {
		t.Fatalf("not cheaper: before %+v after %+v", before, after)
	}
}
