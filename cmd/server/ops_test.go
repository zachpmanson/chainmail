package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// The persons, merges and audit rows the ops surface reviews are people data,
// so the fixture convention is load-bearing here rather than stylistic: every
// name, address and domain below is invented, and every domain is a reserved
// example one. No assertion in this file may name a real human's address.
//
// The store is opened per test as a :memory: corpus behind the same handler the
// rest of the server suite exercises, so the ops surface is tested through the
// wire, not by calling corpus.Dedupe directly.

// personBy resolves one of the fixture's invented accounts to its person id.
func personBy(t *testing.T, s *corpus.Store, addr string) int64 {
	t.Helper()
	id, err := corpus.PersonByIdentity(s, corpus.KindEmail, addr)
	if err != nil {
		t.Fatalf("resolving %s: %v", addr, err)
	}
	return id
}

// ghostOf adds a name-only participant to an entry: someone cc'd as a bare
// name, exactly as a folded quote records one. Returns the new person id.
func ghostOf(t *testing.T, s *corpus.Store, entryID int64, name string) int64 {
	t.Helper()
	ids, err := corpus.RecordHeader(s, entryID, corpus.RoleCc, name)
	if err != nil {
		t.Fatalf("recording %q as cc: %v", name, err)
	}
	if len(ids) != 1 {
		t.Fatalf("a bare name should resolve to exactly one person, got %v", ids)
	}
	return ids[0]
}

// siteVisit is a fresh entry in the fixture's solar thread, so a ghost can
// share it with the human it is named after.
func siteVisit(t *testing.T, s *corpus.Store, author int64, from, to string) int64 {
	t.Helper()
	return putMail(t, s, mailFixture{
		ext: "mail:<c0ffee-7@loomworks.example>",
		ts:  "2026-03-04T09:00:00+11:00", tz: "AEDT", offset: mins(660),
		person: author, container: "T1", subject: "Solar install quote: site visit",
		messageID: "<c0ffee-7@loomworks.example>",
		from:      from,
		to:        to,
		text:      "Site visit booked.",
		atts:      []corpus.Attachment{},
	})
}

// The motivating shape end to end: the plan shows the name-only placeholder
// folded into the human who holds the address, POST /v1/ops/merge applies it,
// the refetched plan is empty, and the trail records the merge with the reason
// the CLI would have recorded.
func TestOpsPlanShowsAnApplicableSameNameMergeAndAppliesIt(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	s := srv.server.store
	ada := personBy(t, s, "ada@loomworks.example")
	entry := siteVisit(t, s, ada, "Ada Okoye <ada@loomworks.example>",
		"Bo Halvorsen <bo@fjordline.example>")
	ghost := ghostOf(t, s, entry, "Ada Okoye")

	res := srv.do(t, "GET", "/v1/ops/plan", nil)
	if res.status != 200 {
		t.Fatalf("plan: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OpsPlanResponse", res.body)
	got := decode[opsPlanResponse](t, res)
	if got.People != 3 {
		t.Errorf("people = %d, want the fixture's 3", got.People)
	}
	if len(got.Merges) != 1 {
		t.Fatalf("merges = %v, want the one ghost->real pair", got.Merges)
	}
	if len(got.TwinsDeclined) != 0 {
		t.Errorf("twinsDeclined = %v, want none in the fixture", got.TwinsDeclined)
	}
	if len(got.Trail) != 0 {
		t.Errorf("trail = %v, want an empty trail before any merge", got.Trail)
	}
	m := got.Merges[0]
	if m.Rule != corpus.RuleSameName {
		t.Fatalf("rule = %q, want %q", m.Rule, corpus.RuleSameName)
	}
	if m.KeepID != ada || m.DropID != ghost {
		t.Fatalf("pair = %d<-%d, want the address-holder %d to keep the placeholder %d",
			m.KeepID, m.DropID, ada, ghost)
	}
	if !m.Applicable {
		t.Error("a same-name merge must be applicable — it is the tier the screen exists for")
	}
	if m.Evidence == "" {
		t.Error("a plan a human is asked to approve must carry the evidence")
	}
	if len(m.KeepIdentities) == 0 {
		t.Error("the keeper must hold an address — that is why it is the keeper")
	}
	// A name-only person's one identity is the display name itself; what they
	// must lack is any address, and that absence is the finding.
	if len(m.DropIdentities) != 1 || !strings.HasPrefix(m.DropIdentities[0], "display_name:") {
		t.Errorf("drop identities = %v, want only the display-name identity", m.DropIdentities)
	}

	body, _ := json.Marshal(opsMergeRequest{KeepID: ada, DropID: ghost})
	res = srv.do(t, "POST", "/v1/ops/merge", body)
	if res.status != 200 {
		t.Fatalf("merge: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OpsMergeResponse", res.body)
	applied := decode[opsMergeResponse](t, res).Merge
	if applied.DropID != ghost || applied.KeepID != ada {
		t.Errorf("response names %d<-%d, want %d<-%d", applied.KeepID, applied.DropID, ada, ghost)
	}
	if !strings.HasPrefix(applied.Reason, corpus.RuleSameName+" (") {
		t.Errorf("reason = %q, want the rule plus the evidence, as the CLI records it", applied.Reason)
	}
	if applied.MergedAt == "" {
		t.Error("the record must carry when it happened")
	}

	res = srv.do(t, "GET", "/v1/ops/plan", nil)
	after := decode[opsPlanResponse](t, res)
	if len(after.Merges) != 0 {
		t.Errorf("plan after = %v, want nothing left to merge", after.Merges)
	}
	if after.People != 2 {
		t.Errorf("people after = %d, want 2", after.People)
	}
	if len(after.Trail) != 1 || after.Trail[0].DropID != ghost {
		t.Errorf("trail after = %v, want the one record naming %d", after.Trail, ghost)
	}
	if after.Trail[0].Reason != applied.Reason {
		t.Errorf("the trail and the response must show the same reason: %q vs %q",
			after.Trail[0].Reason, applied.Reason)
	}
}

// A pair the plan would not make is refused, not retried: applying it again
// after it landed, a swapped keep/drop, and a pair the plan never made are all
// 409s naming the current plan. A body that is not a pair at all is a 400.
func TestOpsMergeRefusesPairsThePlanDoesNotMake(t *testing.T) {
	srv := testServer(t)
	s := srv.server.store
	ada := personBy(t, s, "ada@loomworks.example")
	bo := personBy(t, s, "bo@fjordline.example")
	entry := siteVisit(t, s, ada, "Ada Okoye <ada@loomworks.example>",
		"Bo Halvorsen <bo@fjordline.example>")
	ghost := ghostOf(t, s, entry, "Ada Okoye")

	body, _ := json.Marshal(opsMergeRequest{KeepID: ada, DropID: ghost})
	if res := srv.do(t, "POST", "/v1/ops/merge", body); res.status != 200 {
		t.Fatalf("first merge: status = %d: %s", res.status, res.body)
	}
	// The same pair again: the drop row is gone, so the plan can no longer contain it.
	if res := srv.do(t, "POST", "/v1/ops/merge", body); res.status != 409 {
		t.Fatalf("repeat merge = %d, want 409: %s", res.status, res.body)
	} else if msg := res.errText(t); !strings.Contains(msg, "current dedupe plan") {
		t.Errorf("the refusal must send the client back to the plan: %q", msg)
	}
	// Swapped direction is a different pair and equally refused.
	swapped, _ := json.Marshal(opsMergeRequest{KeepID: ghost, DropID: ada})
	if res := srv.do(t, "POST", "/v1/ops/merge", swapped); res.status != 409 {
		t.Errorf("swapped pair = %d, want 409: %s", res.status, res.body)
	}
	// Two confirmed humans the plan never paired.
	stray, _ := json.Marshal(opsMergeRequest{KeepID: bo, DropID: ada})
	if res := srv.do(t, "POST", "/v1/ops/merge", stray); res.status != 409 {
		t.Errorf("unplanned pair = %d, want 409: %s", res.status, res.body)
	}

	for _, bad := range []struct {
		body   []byte
		needle string
	}{
		{[]byte(`{}`), "keepId"},
		{[]byte(`{"keepId":1}`), "dropId"},
		{[]byte(`{"keepId":1,"dropId":2,"bonkers":true}`), "unknown field"},
		{[]byte(`not json`), "reading the request body"},
	} {
		res := srv.do(t, "POST", "/v1/ops/merge", bad.body)
		if res.status != 400 {
			t.Errorf("bad body = %d, want 400: %s", res.status, res.body)
			continue
		}
		if msg := res.errText(t); !strings.Contains(msg, bad.needle) {
			t.Errorf("message %q does not name %q", msg, bad.needle)
		}
	}
}

// The method shape, pinned the way the other POST endpoints pin theirs: GET on
// the merge is a 405 naming the verb, and the read-only plan takes no body.
func TestOpsEndpointsObeyTheMethodShape(t *testing.T) {
	srv := testServer(t)
	if res := srv.do(t, "GET", "/v1/ops/merge", nil); res.status != 405 {
		t.Errorf("GET /v1/ops/merge = %d, want 405", res.status)
	} else if got := res.header.Get("Allow"); got != "POST" {
		t.Errorf("Allow = %q, want POST", got)
	}
	if res := srv.do(t, "POST", "/v1/ops/plan", nil); res.status != 405 {
		t.Errorf("POST /v1/ops/plan = %d, want 405", res.status)
	} else if got := res.header.Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q, want GET", got)
	}
}

// The first-name-and-org tier is shown, not applied: the plan lists its merge
// with applicable=false, and posting it is a 409 that says so. The boundary is
// the server's, so a hand-rolled request cannot talk past the screen.
func TestOpsMergeRefusesTheTiersTheScreenOnlyShows(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	s := srv.server.store
	old := putPerson(t, s, "Camille Vaughn", "camille@quarry.fed")
	cur := putPerson(t, s, "Camille Vaughn", "camille.vaughn@millrace.fed")
	if _, err := corpus.AddDomainAlias(s, "quarry.fed", "millrace.fed", "rebrand"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	_ = putMail(t, s, mailFixture{
		ext: "mail:<c0ffee-8@millrace.example>",
		ts:  "2026-03-05T10:00:00+11:00", tz: "AEDT", offset: mins(660),
		person: cur, container: "T2", subject: "Mill handover",
		messageID: "<c0ffee-8@millrace.example>",
		from:      "Camille Vaughn <camille.vaughn@millrace.fed>",
		to:        "Bo Halvorsen <bo@fjordline.example>",
		text:      "Handover notes attached.",
		atts:      []corpus.Attachment{},
	})

	res := srv.do(t, "GET", "/v1/ops/plan", nil)
	if res.status != 200 {
		t.Fatalf("plan: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OpsPlanResponse", res.body)
	got := decode[opsPlanResponse](t, res)
	var tier *opsMerge
	for i := range got.Merges {
		if got.Merges[i].Rule == corpus.RuleFirstNameOrg {
			tier = &got.Merges[i]
		}
	}
	if tier == nil {
		t.Fatalf("no first-name-and-org merge in the plan: %+v", got.Merges)
	}
	if tier.Applicable {
		t.Error("the first-name-and-org tier must be shown read-only, not applicable")
	}
	// The account still in use survives, as the CLI's own plan chooses it.
	if tier.KeepID != cur || tier.DropID != old {
		t.Errorf("pair = %d<-%d, want the busier account %d to keep %d",
			tier.KeepID, tier.DropID, cur, old)
	}

	body, _ := json.Marshal(opsMergeRequest{KeepID: tier.KeepID, DropID: tier.DropID})
	res = srv.do(t, "POST", "/v1/ops/merge", body)
	if res.status != 409 {
		t.Fatalf("posting the tier = %d, want 409: %s", res.status, res.body)
	}
	if msg := res.errText(t); !strings.Contains(msg, "read-only") {
		t.Errorf("the refusal must say the tier is read-only: %q", msg)
	}
}

// Two same-named humans the evidence fits equally is the refusal the CLI
// prints, shown read-only: no apply button can exist for it because no merge
// request would take it.
func TestOpsPlanShowsRefusalsReadOnly(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	s := srv.server.store
	ada := personBy(t, s, "ada@loomworks.example")
	second := putPerson(t, s, "Ada Okoye", "ada.okoye@millrace.example")
	entry := putMail(t, s, mailFixture{
		ext: "mail:<c0ffee-9@loomworks.example>",
		ts:  "2026-03-06T09:00:00+11:00", tz: "AEDT", offset: mins(660),
		person: ada, container: "T3", subject: "Roof access",
		messageID: "<c0ffee-9@loomworks.example>",
		from:      "Ada Okoye <ada@loomworks.example>",
		to:        "Ada Okoye <ada.okoye@millrace.example>",
		text:      "Roof access confirmed.",
		atts:      []corpus.Attachment{},
	})
	ghost := ghostOf(t, s, entry, "Ada Okoye")

	res := srv.do(t, "GET", "/v1/ops/plan", nil)
	if res.status != 200 {
		t.Fatalf("plan: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OpsPlanResponse", res.body)
	got := decode[opsPlanResponse](t, res)
	if len(got.Merges) != 0 {
		t.Errorf("merges = %+v, want none where the evidence fits both equally", got.Merges)
	}
	var refusal *opsRefusal
	for i := range got.Refusals {
		if got.Refusals[i].Rule == corpus.RuleSameName {
			refusal = &got.Refusals[i]
		}
	}
	if refusal == nil {
		t.Fatalf("no same-name refusal in the plan: %+v", got.Refusals)
	}
	if refusal.Subject != "ada okoye" {
		t.Errorf("subject = %q, want the normalised name", refusal.Subject)
	}
	if refusal.Reason == "" {
		t.Error("a refusal must carry why it refused")
	}
	if !samePeople(refusal.People, []int64{ghost, ada, second}) {
		t.Errorf("people = %v, want the ghost and both candidates %v",
			refusal.People, []int64{ghost, ada, second})
	}
}

func samePeople(a, b []int64) bool {
	// Ids come from different creations; compare as sets (they are all distinct).
	for _, x := range a {
		seen := false
		for _, y := range b {
			if y == x {
				seen = true
			}
		}
		if !seen {
			return false
		}
	}
	return len(a) == len(b)
}
