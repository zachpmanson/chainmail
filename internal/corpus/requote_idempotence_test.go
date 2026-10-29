package corpus

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Additional hosts are invented forwards of the existing synthetic quote.
func addRepairHost(t *testing.T, s *Store, quotedID int64, tag, body string) {
	t.Helper()
	var key string
	if err := s.DB().QueryRow(`select ext_id from entries where id=?`, quotedID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	q := RecoverQuotes(body)
	if len(q.Distinct) > 1 || (len(q.Distinct) == 1 && q.Distinct[0].Key != key) {
		t.Fatalf("fixture must recover the same quote identity: %+v", q)
	}
	ext := fmt.Sprintf("<repair-host-%s@example.invalid>", tag)
	host, err := s.Put(Entry{
		Source: SourceMail, ExtID: "mail:" + ext, TS: time.Unix(1_700_000_000, 0),
		Subject: "Invented forward", Container: "fernbrook", BodyText: body,
	}, &Mail{MessageID: ext}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sight(quotedID, host.ID, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
}

func checkRepairIndexes(t *testing.T, s *Store) {
	t.Helper()
	for _, table := range []string{"entries_fts", "entries_ident"} {
		// rank=1 also compares an external-content index to its source rows.
		if _, err := s.DB().Exec("insert into " + table + "(" + table + ", rank) values('integrity-check', 1)"); err != nil {
			t.Errorf("%s external-content integrity: %v", table, err)
		}
	}
}

func TestRepairQuotedBodiesConflictingHostsDoesNotOscillate(t *testing.T) {
	s, _, _, quotedID, recovered := foldStored(t)
	old, _ := bodyOf(t, s, quotedID)
	if _, err := rewriteQuotedBody(s, quotedID, "Fernbrook sites", old, recovered); err != nil {
		t.Fatal(err)
	}
	alternate := strings.Replace(foldForwardBody(), foldMessage,
		"Invented alternate quote body: a different version of this message.", 1)
	if got := RecoverQuotes(alternate).Distinct[0].Block.Text; got == recovered {
		t.Fatal("fixture must contain conflicting quote text")
	}
	addRepairHost(t, s, quotedID, "alternate", alternate)

	var fixed []int
	var bodies []string
	for i := 0; i < 2; i++ {
		r, err := RepairQuotedBodies(s)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Ambiguous) != 1 {
			t.Errorf("want one ambiguous entry, got %+v", r)
		}
		fixed = append(fixed, r.Fixed)
		body, _ := bodyOf(t, s, quotedID)
		bodies = append(bodies, body)
	}
	if fixed[0] != 0 || fixed[1] != 0 || bodies[0] != recovered || bodies[1] != recovered {
		t.Fatalf("conflicting hosts must abstain, not alternate: fixed=%v firstIsOriginal=%t secondIsOriginal=%t sameFinalBody=%t",
			fixed, bodies[0] == recovered, bodies[1] == recovered, bodies[0] == bodies[1])
	}
	checkRepairIndexes(t, s)
}

func TestRepairQuotedBodiesAgreeingHostsRewriteOnce(t *testing.T) {
	s, mailboxID, _, quotedID, recovered := foldStored(t)
	mailboxBody, mailboxSHA := bodyOf(t, s, mailboxID)
	addRepairHost(t, s, quotedID, "agreement", foldForwardBody())
	r, err := RepairQuotedBodies(s)
	if err != nil {
		t.Fatal(err)
	}
	if r.Fixed != 1 || r.Hosts != 2 {
		t.Errorf("agreement should rewrite one entry once: %+v", r)
	}
	body, sha := bodyOf(t, s, quotedID)
	if body != recovered || sha != BodySHA(recovered, "Fernbrook sites") {
		t.Errorf("body/hash must agree with PutQuoted/EnrichQuoted's canonical order")
	}
	if gotBody, gotSHA := bodyOf(t, s, mailboxID); gotBody != mailboxBody || gotSHA != mailboxSHA {
		t.Error("authoritative mailbox body/hash changed")
	}
	checkRepairIndexes(t, s)
	again, err := RepairQuotedBodies(s)
	if err != nil || again.Fixed != 0 {
		t.Fatalf("second agreeing-host pass: %+v, %v", again, err)
	}
	checkRepairIndexes(t, s)
}
