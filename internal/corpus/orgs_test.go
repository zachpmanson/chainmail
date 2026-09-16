package corpus

import "testing"

// The three states a domain can be in are three, not two: a rule naming an
// organisation, a rule naming none — the reader has looked and there is nothing
// there — and no rule at all, which leaves the domain to be read from its own
// name. A store that folded the second into the third would make "this is not an
// organisation" impossible to say, and a colour map that cannot say that ends up
// calling everybody's ISP an organisation.
func TestAnEmptyOrgIsAStoredDecision(t *testing.T) {
	s := open(t)
	if err := s.PutOrgRule("bigpond.com", ""); err != nil {
		t.Fatal(err)
	}
	rules, err := s.OrgRules()
	if err != nil {
		t.Fatal(err)
	}
	org, ok := rules["bigpond.com"]
	if !ok || org != "" {
		t.Fatalf("a rule about nothing came back as %q, %v; want it stored and empty", org, ok)
	}

	if err := s.ClearOrgRule("bigpond.com"); err != nil {
		t.Fatal(err)
	}
	rules, err = s.OrgRules()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rules["bigpond.com"]; ok {
		t.Errorf("clearing left a rule behind: %v", rules)
	}
}

// A domain is case-insensitive, so "Termina.IO" and "termina.io" are one rule and
// not two — a pair that exists only to get out of step with each other. The label
// beside it is prose the reader chose, so it is kept as written.
func TestOrgRulesNormaliseTheDomainNotTheLabel(t *testing.T) {
	s := open(t)
	if err := s.PutOrgRule("  Termina.IO ", "Termina"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutOrgRule("termina.io", "The Termina Co"); err != nil {
		t.Fatal(err)
	}
	rules, err := s.OrgRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %v, want one row for one domain", rules)
	}
	if got := rules["termina.io"]; got != "The Termina Co" {
		t.Errorf("org = %q, want the replacement", got)
	}
}

// A rule with no domain has nothing to be about, and storing one would put a row
// in the table that no mail can ever match.
func TestARuleNeedsADomain(t *testing.T) {
	s := open(t)
	if err := s.PutOrgRule("  ", "Termina"); err == nil {
		t.Error("a rule with no domain was accepted")
	}
	if err := s.PutOrgRule("termina.io", "Termina"); err != nil {
		t.Fatal(err)
	}
	if rules, _ := s.OrgRules(); len(rules) != 1 {
		t.Errorf("a refused rule was stored anyway: %v", rules)
	}
}
