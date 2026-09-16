package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/spec"
)

// The colour rules are a judgement about the reader's own mail, so the surface
// that changes them is the one that has to be able to say what the change costs
// before it is made — the same shape as the merge screen beside it, where the
// plan and the evidence are read before the apply. These tests go through the
// wire, over the fixture corpus, whose only two domains make both halves of the
// list legible: one domain with three messages and one with a single message.
func TestOpsOrgsShowsEveryDomainAndItsGrouping(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)

	res := srv.do(t, "GET", "/v1/ops/orgs", nil)
	if res.status != 200 {
		t.Fatalf("orgs: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OrgsResponse", res.body)
	got := decode[orgsResponse](t, res)
	if len(got.Domains) != 2 {
		t.Fatalf("domains = %v, want the fixture's two", got.Domains)
	}
	// Busiest first: the domain a rule is most likely to be about is at the top.
	if got.Domains[0].Domain != "loomworks.example" || got.Domains[1].Domain != "fjordline.example" {
		t.Fatalf("domains = %v,%v — want the busiest first", got.Domains[0].Domain, got.Domains[1].Domain)
	}
	first := got.Domains[0]
	if first.Messages != 3 || first.People != 1 {
		t.Errorf("loomworks = %d messages from %d people, want 3 from 1", first.Messages, first.People)
	}
	// Nothing is stored yet, so every label here is the domain's own name being
	// read back — and the answer has to say so, or a reader cannot tell a decision
	// they made from a guess the corpus made.
	if first.Stored {
		t.Error("a domain nobody has ruled on is reported as stored")
	}
	if first.Org != "Loomworks" || first.Guess != "Loomworks" {
		t.Errorf("loomworks grouping = %q (guess %q), want the guessed Loomworks", first.Org, first.Guess)
	}
}

// A rule is stored, read back, and cleared — and clearing is a different act
// from ruling a domain out, which the reader does by naming no organisation.
func TestOpsOrgsStoresReadsAndClearsARule(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)

	body, _ := json.Marshal(orgRuleRequest{Domain: "fjordline.example", Org: strptr("Loomworks")})
	res := srv.do(t, "POST", "/v1/ops/orgs", body)
	if res.status != 200 {
		t.Fatalf("setting a rule: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OrgsResponse", res.body)
	// The write answers with the rules as they now stand, so the screen has the
	// list its own change produced rather than the one it asked for.
	rules := decode[orgsResponse](t, res)
	if got := domainNamed(t, rules, "fjordline.example"); got.Org != "Loomworks" || !got.Stored {
		t.Errorf("after the write: %+v, want Fjordline's mail drawn as Loomworks", got)
	}

	// An empty label is a decision, so the JSON carries `stored` and no `org` at
	// all — which a screen has to be able to tell from a domain nobody has looked
	// at, because only one of those follows the domain's own name later.
	body, _ = json.Marshal(orgRuleRequest{Domain: "fjordline.example", Org: strptr("")})
	res = srv.do(t, "POST", "/v1/ops/orgs", body)
	if res.status != 200 {
		t.Fatalf("ruling a domain out: status = %d: %s", res.status, res.body)
	}
	rules = decode[orgsResponse](t, res)
	got := domainNamed(t, rules, "fjordline.example")
	if got.Org != "" || !got.Stored {
		t.Errorf("ruled out: %+v, want stored with no organisation", got)
	}
	if strings.Contains(string(res.body), `"org":""`) {
		t.Error("an empty organisation is served as an empty string rather than omitted")
	}

	// No org at all in the body is the third answer: drop the rule.
	body, _ = json.Marshal(orgRuleRequest{Domain: "fjordline.example"})
	res = srv.do(t, "POST", "/v1/ops/orgs", body)
	if res.status != 200 {
		t.Fatalf("clearing a rule: status = %d: %s", res.status, res.body)
	}
	rules = decode[orgsResponse](t, res)
	if got := domainNamed(t, rules, "fjordline.example"); got.Stored {
		t.Errorf("after clearing: %+v, want the domain back to its own name", got)
	}
}

// A rule with no domain is a rule about nothing, and a colour screen that
// accepted one would store a row no mail can match.
func TestOpsOrgsRefusesARuleAboutNothing(t *testing.T) {
	srv := testServer(t)
	for _, body := range []string{`{}`, `{"domain":"   "}`} {
		res := srv.do(t, "POST", "/v1/ops/orgs", []byte(body))
		if res.status != 400 {
			t.Fatalf("%s: status = %d, want a refusal: %s", body, res.status, res.body)
		}
		res.errText(t)
	}
	// And a field nobody documented is a caller reading a different contract.
	if res := srv.do(t, "POST", "/v1/ops/orgs", []byte(`{"domain":"a.example","organisation":"X"}`)); res.status != 400 {
		t.Errorf("an unknown field was accepted: %d %s", res.status, res.body)
	}
}

// The preview is the whole reason the write is safe: the consequence is counted
// by the resolver that will apply it, before anything is stored.
func TestOpsOrgsPreviewCountsTheChangeAndStoresNothing(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)

	body, _ := json.Marshal(orgRuleRequest{Domain: "fjordline.example", Org: strptr("Loomworks")})
	res := srv.do(t, "POST", "/v1/ops/orgs/preview", body)
	if res.status != 200 {
		t.Fatalf("preview: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "OrgShiftResponse", res.body)
	shift := decode[orgShiftResponse](t, res)
	// Grouping is nothing more than a shared name, so this moves Bo's one message
	// from Fjordline to Loomworks and touches nobody else's.
	if shift.Messages != 1 || shift.People != 1 || shift.Ambiguous != 0 {
		t.Errorf("shift = %+v, want one message from one person and nothing ambiguous", shift)
	}
	if shift.Domain != "fjordline.example" {
		t.Errorf("domain = %q, want the one asked about", shift.Domain)
	}

	// Nothing stored: a preview is a question, and a screen that has not been
	// agreed with must not be able to change the corpus by looking at it.
	res = srv.do(t, "GET", "/v1/ops/orgs", nil)
	if got := domainNamed(t, decode[orgsResponse](t, res), "fjordline.example"); got.Stored {
		t.Errorf("the preview stored a rule: %+v", got)
	}

	// A rule about a domain the corpus has no mail from is about mail that has
	// not arrived, which is a thing an operator may know first: it is accepted,
	// and it changes nothing today.
	body, _ = json.Marshal(orgRuleRequest{Domain: "notyet.example", Org: strptr("Not Yet")})
	res = srv.do(t, "POST", "/v1/ops/orgs/preview", body)
	if res.status != 200 {
		t.Fatalf("preview of a domain with no mail: status = %d: %s", res.status, res.body)
	}
	if shift := decode[orgShiftResponse](t, res); shift.Messages != 0 || shift.Ambiguous != 0 {
		t.Errorf("a rule about no mail reports %+v", shift)
	}
}

func domainNamed(t *testing.T, r orgsResponse, domain string) orgRuleResponse {
	t.Helper()
	for _, d := range r.Domains {
		if d.Domain == domain {
			return d
		}
	}
	t.Fatalf("%s is not in the domain list: %v", domain, r.Domains)
	return orgRuleResponse{}
}

func strptr(s string) *string { return &s }

// The pane is a key to the page, and the colour a bubble is drawn in is the one
// thing a reader would notice disagreeing: a page that groups a sender one way
// and a pane that groups them another are two renderers again. So this asserts
// the two surfaces on the same corpus, before and after a rule is stored.
func TestThePaneAndThePageColourASenderTheSameWay(t *testing.T) {
	srv := testServer(t)

	chainOrgs := func() map[string]string {
		t.Helper()
		res := srv.do(t, "GET", entryPath("/v1/chains/", extAda1), nil)
		if res.status != 200 {
			t.Fatalf("chain: status = %d: %s", res.status, res.body)
		}
		got := decode[struct {
			Entries []struct {
				ExtID string `json:"extId"`
				Org   string `json:"org"`
			} `json:"entries"`
		}](t, res)
		out := map[string]string{}
		for _, e := range got.Entries {
			out[e.ExtID] = e.Org
		}
		return out
	}
	pageOrgs := func() map[string]string {
		t.Helper()
		res := srv.do(t, "POST", "/v1/spec", specBody(extAda1))
		if res.status != 200 {
			t.Fatalf("spec: status = %d: %s", res.status, res.body)
		}
		out := map[string]string{}
		for _, m := range decode[spec.Spec](t, res).Messages {
			out[m.ExtID] = m.Org
		}
		return out
	}
	compare := func() {
		t.Helper()
		pane, page := chainOrgs(), pageOrgs()
		if len(pane) != 3 {
			t.Fatalf("the pane drew %d entries, want the fixture's chain of 3", len(pane))
		}
		for ext, org := range pane {
			if page[ext] != org {
				t.Errorf("%s: pane says %q, the page says %q", ext, org, page[ext])
			}
		}
	}

	// The guessed names first, then the reader's own answer about one domain —
	// which has to move both surfaces at once, or the rule is half applied.
	compare()
	body, _ := json.Marshal(orgRuleRequest{Domain: "loomworks.example", Org: strptr("The Loom")})
	if res := srv.do(t, "POST", "/v1/ops/orgs", body); res.status != 200 {
		t.Fatalf("storing a rule: status = %d: %s", res.status, res.body)
	}
	compare()

	// And the rule is actually visible on both, so agreeing about nothing would
	// not pass this either.
	page := pageOrgs()
	pane := chainOrgs()
	if page[extAda1] != "The Loom" || pane[extAda3] != "The Loom" {
		t.Errorf("after the rule: page %q, pane %q, want The Loom", page[extAda1], pane[extAda3])
	}
	if page[extBo2] != "Fjordline" {
		t.Errorf("between %q, want Fjordline left alone", page[extBo2])
	}
}
