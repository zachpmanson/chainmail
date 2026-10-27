package main

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/mailingest"
)

type undoMailbox struct{ *fakeMailbox }

func (f undoMailbox) Read(id string) (mailingest.Message, error) {
	if f.readErr != nil {
		return mailingest.Message{}, f.readErr
	}
	labels, ok := f.base[id]
	if !ok {
		return mailingest.Message{}, errors.New("message missing")
	}
	return mailingest.Message{Envelope: mailingest.Envelope{ID: id, Labels: append([]string{}, labels...)}}, nil
}
func (f undoMailbox) SetLabels(id string, add, remove []string) ([]string, error) {
	labels, err := f.fakeMailbox.SetLabels(id, add, remove)
	if err == nil {
		f.base[id] = labels
	}
	return labels, err
}
func (f undoMailbox) SetUnread(id string, unread bool) ([]string, error) {
	labels, err := f.fakeMailbox.SetUnread(id, unread)
	if err == nil {
		f.base[id] = labels
	}
	return labels, err
}
func undoServer(t *testing.T) (*harness, undoMailbox) {
	h, fake := mailServer(t)
	mb := undoMailbox{fake}
	h.openMailbox = func() (mailbox, error) { return mb, nil }
	return h, mb
}
func tokenFrom(t *testing.T, res *response) string {
	t.Helper()
	var data struct {
		UndoToken string `json:"undoToken"`
	}
	if err := json.Unmarshal(res.body, &data); err != nil {
		t.Fatal(err)
	}
	return data.UndoToken
}
func undoRequest(t *testing.T, h *harness, token string) *response {
	t.Helper()
	return h.do(t, "POST", "/v1/mail/undo", []byte(`{"token":"`+token+`"}`))
}

func TestUndoRestoresMixedFreshMailboxState(t *testing.T) {
	for _, action := range []string{"archive", "trash", "move", "read", "unread"} {
		t.Run(action, func(t *testing.T) {
			h, mb := undoServer(t)
			// Deliberately disagrees with corpus labels; preexisting Work must survive.
			mb.base["g-1"] = []string{"INBOX", "UNREAD", "Work"}
			mb.base["g-2"] = []string{"Work"}
			mb.base["g-3"] = []string{"SENT", "INBOX"}
			before := map[string][]string{}
			for id, labels := range mb.base {
				before[id] = slices.Clone(labels)
			}
			var res *response
			if action == "read" || action == "unread" {
				unread := "false"
				if action == "unread" {
					unread = "true"
				}
				res = h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","unread":`+unread+`,"undoable":true}`))
			} else {
				res = h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`","`+extAda1+`"],"action":"`+action+`","labels":["Work"],"undoable":true}`))
			}
			if res.status != 200 {
				t.Fatalf("forward %d: %s", res.status, res.body)
			}
			token := tokenFrom(t, res)
			if token == "" {
				t.Fatal("no receipt")
			}
			mb.base["g-1"] = append(mb.base["g-1"], "Added elsewhere")
			got := undoRequest(t, h, token)
			if got.status != 200 {
				t.Fatalf("undo %d: %s", got.status, got.body)
			}
			before["g-1"] = append(before["g-1"], "Added elsewhere")
			for id, want := range before {
				labels := slices.Clone(mb.base[id])
				slices.Sort(labels)
				slices.Sort(want)
				if !slices.Equal(labels, want) {
					t.Fatalf("%s: %v != %v", id, labels, want)
				}
			}
			// Response loss is retryable without replaying successful reversals.
			calls := len(mb.calls)
			if got := undoRequest(t, h, token); got.status != 200 {
				t.Fatal(string(got.body))
			}
			if len(mb.calls) != calls {
				t.Fatal("replayed consumed receipt")
			}
		})
	}
}

func TestUndoNoopAndSnapshotFailure(t *testing.T) {
	h, mb := undoServer(t)
	for id := range mb.base {
		mb.base[id] = []string{"SENT"}
	}
	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive","undoable":true}`))
	if res.status != 200 || tokenFrom(t, res) != "" {
		t.Fatalf("noop: %s", res.body)
	}
	mb.readErr = errors.New("offline")
	calls := len(mb.calls)
	res = h.do(t, "POST", "/v1/read", []byte(`{"chain":"`+extAda1+`","undoable":true}`))
	if res.status != 502 || len(mb.calls) != calls {
		t.Fatalf("capture failure wrote mail: %s", res.body)
	}
}

func TestUndoPartialFailureAndRetry(t *testing.T) {
	h, mb := undoServer(t)
	mb.fail = map[string]error{"g-2": errors.New("offline")}
	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive","undoable":true}`))
	if res.status != 502 {
		t.Fatalf("%d: %s", res.status, res.body)
	}
	token := tokenFrom(t, res)
	if token == "" {
		t.Fatal("confirmed partial changes lost")
	}
	if got := undoRequest(t, h, token); got.status != 200 {
		t.Fatal(string(got.body))
	}
	if !slices.Contains(mb.base["g-1"], "INBOX") {
		t.Fatal("partial change not restored")
	}
	delete(mb.fail, "g-2")
	res = h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"trash","undoable":true}`))
	token = tokenFrom(t, res)
	mb.fail["g-2"] = errors.New("offline")
	if got := undoRequest(t, h, token); got.status != 502 {
		t.Fatal(string(got.body))
	}
	calls := len(mb.calls)
	delete(mb.fail, "g-2")
	if got := undoRequest(t, h, token); got.status != 200 {
		t.Fatal(string(got.body))
	}
	if len(mb.calls)-calls != 2 {
		t.Fatal("retry replayed already restored messages")
	}
}

func TestUndoRefusesConflictsExpiredTokensAndRevokedGrants(t *testing.T) {
	h, mb := undoServer(t)
	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"move","labels":["Work"],"undoable":true}`))
	token := tokenFrom(t, res)
	calls := len(mb.calls)
	// Neither the prior INBOX nor the resulting Work state.
	mb.base["g-1"] = []string{"UNREAD"}
	if got := undoRequest(t, h, token); got.status != 409 || len(mb.calls) != calls {
		t.Fatal("conflict wrote mail")
	}
	h.mailWriteEnabled = false
	if got := undoRequest(t, h, token); got.status != 403 {
		t.Fatal("grant bypass")
	}
	if got := undoRequest(t, h, "made-up"); got.status != 409 {
		t.Fatal("unknown receipt accepted")
	}
}

func TestUndoOrdersOverlappingReceiptsAndUnrecordedWrites(t *testing.T) {
	for _, newer := range []string{"receipt", "unrecorded", "noop", "ambiguous"} {
		t.Run(newer, func(t *testing.T) {
			h, mb := undoServer(t)
			first := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive","undoable":true}`))
			token := tokenFrom(t, first)
			action, undoable := "trash", "true"
			if newer == "unrecorded" {
				undoable = "false"
			}
			if newer == "noop" {
				action = "archive"
			}
			if newer == "ambiguous" {
				mb.fail = map[string]error{"g-1": errors.New("unknown outcome")}
			}
			later := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"`+action+`","undoable":`+undoable+`}`))
			delete(mb.fail, "g-1")
			calls := len(mb.calls)
			if got := undoRequest(t, h, token); got.status != 409 || len(mb.calls) != calls {
				t.Fatalf("old receipt accepted: %d %s", got.status, got.body)
			}
			if newer == "receipt" {
				if got := undoRequest(t, h, tokenFrom(t, later)); got.status != 200 {
					t.Fatal(string(got.body))
				}
				if got := undoRequest(t, h, token); got.status != 200 {
					t.Fatalf("LIFO did not restore prior version: %s", got.body)
				}
			}
		})
	}
}

func TestUndoChecksLabelsAbsentInBothStates(t *testing.T) {
	h, mb := undoServer(t)
	for id := range mb.base {
		mb.base[id] = []string{"UNREAD"}
	}
	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"move","labels":["Work"],"undoable":true}`))
	mb.base["g-1"] = append(mb.base["g-1"], "INBOX")
	calls := len(mb.calls)
	if got := undoRequest(t, h, tokenFrom(t, res)); got.status != 409 || len(mb.calls) != calls {
		t.Fatal("absent touched label conflict missed")
	}
}

func TestUndoRepeatedLabelVersionsAndMirrorFailure(t *testing.T) {
	h, mb := undoServer(t)
	first := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"archive","undoable":true}`))
	second := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"action":"move","labels":["Work","Work"],"undoable":true}`))
	if _, err := h.store.DB().Exec(`CREATE TRIGGER reject_labels BEFORE UPDATE ON gmail_copies BEGIN SELECT RAISE(ABORT,'mirror failure'); END`); err != nil {
		t.Fatal(err)
	}
	if got := undoRequest(t, h, tokenFrom(t, second)); got.status != 500 {
		t.Fatal(string(got.body))
	}
	if _, err := h.store.DB().Exec(`DROP TRIGGER reject_labels`); err != nil {
		t.Fatal(err)
	}
	if got := undoRequest(t, h, tokenFrom(t, second)); got.status != 200 {
		t.Fatal(string(got.body))
	}
	if got := undoRequest(t, h, tokenFrom(t, first)); got.status != 200 {
		t.Fatal(string(got.body))
	}
	if !slices.Contains(mb.base["g-1"], "INBOX") {
		t.Fatal("prior version stranded by duplicate labels")
	}
}

func TestUndoKeepsOriginalAccountAndExactMessageIDs(t *testing.T) {
	h, mb := undoServer(t)
	if err := h.store.PutGmailAccount(corpus.GmailAccount{ID: "work", Email: "work@example.test", DisplayName: "Work"}); err != nil {
		t.Fatal(err)
	}
	entries, err := h.store.ChainEntries(extAda1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Copies) > 0 {
			if err := h.store.PutGmailCopy(corpus.GmailCopy{AccountID: "work", GmailID: e.Copies[0].GmailID, EntryID: e.ID, Labels: []string{"INBOX"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	other := undoMailbox{&fakeMailbox{base: map[string][]string{"g-1": {"INBOX"}, "g-2": {"INBOX"}, "g-3": {"INBOX"}}}}
	account := ""
	h.openMailboxForAccount = func(id string) (mailbox, error) {
		account = id
		if id == "work" {
			return other, nil
		}
		return mb, nil
	}
	res := h.do(t, "POST", "/v1/mail", []byte(`{"chains":["`+extAda1+`"],"accountId":"work","action":"archive","undoable":true}`))
	if res.status != 200 {
		t.Fatalf("%d: %s", res.status, res.body)
	}
	// A later-arriving reply must not be pulled into the reversal.
	ada := putPerson(t, h.store, "New sender", "new@example.test")
	putMail(t, h.store, mailFixture{ext: "mail:new", ts: "2026-03-06T08:00:00+11:00", tz: "AEDT", person: ada, container: "T1", messageID: "<new@example.test>", inReplyTo: "<c0ffee-1@loomworks.example>", from: "new@example.test", to: "reader@example.test", labels: []string{"UNREAD"}, gmail: "g-new", text: "New reply"})
	newEntries, err := h.store.ChainEntries("mail:new")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range newEntries {
		if e.ExtID == "mail:new" {
			if err := h.store.PutGmailCopy(corpus.GmailCopy{AccountID: "work", GmailID: "g-new", EntryID: e.ID, Labels: []string{"UNREAD"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	other.base["g-new"] = []string{"UNREAD"}
	got := undoRequest(t, h, tokenFrom(t, res))
	if got.status != 200 || account != "work" {
		t.Fatalf("%d %s account=%s", got.status, got.body, account)
	}
	if !slices.Contains(other.base["g-2"], "INBOX") || len(mb.calls) != 0 || slices.Contains(other.base["g-new"], "INBOX") {
		t.Fatal("wrong message/account")
	}
}
