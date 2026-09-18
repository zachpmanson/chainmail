package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The people editor is the corpus's only hand-made identity write, so what these
// tests pin down is what it refuses: a name or an address another person already
// answers to is a merge, and a merge is a decision the plan screen makes with the
// evidence in front of it. Everything it does write is done in one transaction —
// a half-applied edit is a person whose name and addresses disagree.
func TestEditPersonRenamesAndAnswersWithThePerson(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	ada := personOf(t, srv, "ada@loomworks.example")

	body, _ := json.Marshal(personEditRequest{DisplayName: "Ada Nwosu"})
	res := srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", ada), body)
	if res.status != 200 {
		t.Fatalf("renaming: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "PersonResponse", res.body)
	got := decode[personResponse](t, res)
	if got.Person.PersonID != ada || got.Person.DisplayName != "Ada Nwosu" {
		t.Fatalf("the write answered with %+v, want Ada Nwosu at %d", got.Person, ada)
	}
	// The name the corpus read from headers is still one of her identities: the
	// screen said what to call her, not which spellings she has been written as.
	if !hasIdentity(got.Person, "email:ada@loomworks.example") {
		t.Errorf("identities = %v, want the address she was read by", got.Person.Identities)
	}
	// And the person the list serves is the one the write served.
	list := decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	var found bool
	for _, p := range list.People {
		if p.PersonID == ada {
			found = true
			if p.DisplayName != "Ada Nwosu" {
				t.Errorf("the list still says %q", p.DisplayName)
			}
		}
	}
	if !found {
		t.Fatalf("ada is missing from the list: %+v", list.People)
	}
}

func TestEditPersonAttachesAndDetachesIdentities(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	bo := personOf(t, srv, "bo@fjordline.example")

	// An address written with the case of a human hand still lands as the
	// corpus's own spelling: the box cannot make a second identity.
	body, _ := json.Marshal(personEditRequest{AddIdentities: []string{"email:Bo@Halvorsen.example"}})
	res := srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", bo), body)
	if res.status != 200 {
		t.Fatalf("adding: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "PersonResponse", res.body)
	got := decode[personResponse](t, res)
	if !hasIdentity(got.Person, "email:bo@halvorsen.example") {
		t.Fatalf("identities = %v, want the added, folded address", got.Person.Identities)
	}

	body, _ = json.Marshal(personEditRequest{RemoveIdentities: []string{"email:bo@halvorsen.example"}})
	got = decode[personResponse](t, srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", bo), body))
	if hasIdentity(got.Person, "email:bo@halvorsen.example") {
		t.Fatalf("the address survived being detached: %v", got.Person.Identities)
	}
}

func TestEditPersonRefusesToMoveAnIdentityBetweenPeople(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	ada := personOf(t, srv, "ada@loomworks.example")
	bo := personOf(t, srv, "bo@fjordline.example")

	body, _ := json.Marshal(personEditRequest{AddIdentities: []string{"email:ada@loomworks.example"}})
	res := srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", bo), body)
	if res.status != 409 {
		t.Fatalf("taking another person's address: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "Error", res.body)
	// The refusal names the holder, because the reader's next move is to go and
	// look at the pair on the merge screen.
	if !strings.Contains(string(res.body), "Ada Okoye") {
		t.Errorf("the refusal does not name who holds it: %s", res.body)
	}
	if !strings.Contains(string(res.body), "merge instead") {
		t.Errorf("the refusal does not say what to do instead: %s", res.body)
	}
	// And nothing moved.
	list := decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	var holder int64
	for _, p := range list.People {
		if hasIdentity(p, "email:ada@loomworks.example") {
			holder = p.PersonID
		}
	}
	if holder != ada {
		t.Fatalf("the address now belongs to %d, want ada at %d", holder, ada)
	}
}

func TestEditPersonRejectsJunkBodiesAndUnknownPeople(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	ada := personOf(t, srv, "ada@loomworks.example")

	cases := []struct {
		name string
		id   int64
		body []byte
		want int
	}{
		{"an identity that is not kind:value", ada, jsonBody(t, personEditRequest{AddIdentities: []string{"ada@loomworks.example"}}), 400},
		{"an unknown identity kind", ada, jsonBody(t, personEditRequest{AddIdentities: []string{"handle:ada"}}), 400},
		{"a body that says nothing", ada, jsonBody(t, personEditRequest{}), 400},
		{"a misspelled field", ada, []byte(`{"displayNmae":"Ada"}`), 400},
		{"a person who does not exist", 9999, jsonBody(t, personEditRequest{DisplayName: "Nobody"}), 404},
		{"an id that is not a number", 0, jsonBody(t, personEditRequest{DisplayName: "Nobody"}), 400},
	}
	for _, c := range cases {
		path := fmt.Sprintf("/v1/people/%d", c.id)
		if c.name == "an id that is not a number" {
			path = "/v1/people/ada"
		}
		res := srv.do(t, "POST", path, c.body)
		if res.status != c.want {
			t.Errorf("%s: status = %d, want %d: %s", c.name, res.status, c.want, res.body)
			continue
		}
		if c.want != 200 {
			api.assert(t, "Error", res.body)
		}
	}
	// The people path is read-only in its collection form, and a person is a
	// resource you POST an edit to, not one you PUT or DELETE.
	if res := srv.do(t, "GET", fmt.Sprintf("/v1/people/%d", ada), nil); res.status != 405 {
		t.Errorf("GET on one person: status = %d, want 405", res.status)
	}
}

// The reading style is the one thing this endpoint writes that is about the
// reader rather than about who somebody is, and it lives here because it is a
// fact about a person rather than a preference of the browser: the reader who has
// decided how to read a run of notifications has decided it on every device they
// read it on, and a merge of two spellings of that person cannot lose it.
func TestEditPersonRemembersHowTheirMailIsRead(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	ada := personOf(t, srv, "ada@loomworks.example")

	// Absent from the body, the style is left where it stands — the rule every
	// other field here follows, and the reason it is a pointer on the wire: a
	// screen that renamed somebody must not switch their reading style off by
	// leaving the field out.
	res := srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", ada),
		jsonBody(t, personEditRequest{DisplayName: "Ada Nwosu"}))
	if res.status != 200 {
		t.Fatalf("renaming: status = %d: %s", res.status, res.body)
	}
	if got := decode[personResponse](t, res); got.Person.PreferOriginal {
		t.Fatalf("a rename turned the style on: %+v", got.Person)
	}

	on := true
	res = srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", ada),
		jsonBody(t, personEditRequest{PreferOriginal: &on}))
	if res.status != 200 {
		t.Fatalf("turning it on: status = %d: %s", res.status, res.body)
	}
	api.assert(t, "PersonResponse", res.body)
	if got := decode[personResponse](t, res); !got.Person.PreferOriginal {
		t.Fatalf("the write answered with %+v, want the reading style on", got.Person)
	}

	// The list carries the same answer, so the people screen and the bubble can
	// never disagree about one sender.
	list := decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	var found bool
	for _, p := range list.People {
		if p.PersonID != ada {
			continue
		}
		found = true
		if !p.PreferOriginal {
			t.Errorf("the list still says the style is off: %+v", p)
		}
	}
	if !found {
		t.Fatalf("ada is missing from the list: %+v", list.People)
	}

	// And every entry she sent carries it, because that is where the bubble reads
	// it from: the control is drawn from the chain read alone, with no second
	// request per message.
	got := decode[chainResponse](t, srv.do(t, "GET", entryPath("/v1/chains/", extAda1), nil))
	var sawAda bool
	for _, e := range got.Entries {
		if e.ExtID != extAda1 && e.ExtID != extAda3 {
			if e.PreferOriginal {
				t.Errorf("%s: a style set on ada reached this entry", e.ExtID)
			}
			continue
		}
		sawAda = true
		if e.PersonID != ada || !e.PreferOriginal {
			t.Errorf("%s reads personId=%d preferOriginal=%v, want %d and true",
				e.ExtID, e.PersonID, e.PreferOriginal, ada)
		}
	}
	if !sawAda {
		t.Fatalf("the chain holds none of ada's messages: %+v", got.Entries)
	}

	// Off is a claim rather than an absence: the reader asking for the
	// transcript's rendering again is an answer, and it is served as one.
	off := false
	res = srv.do(t, "POST", fmt.Sprintf("/v1/people/%d", ada),
		jsonBody(t, personEditRequest{PreferOriginal: &off}))
	if res.status != 200 {
		t.Fatalf("turning it off: status = %d: %s", res.status, res.body)
	}
	if got := decode[personResponse](t, res); got.Person.PreferOriginal {
		t.Fatalf("turning the style off answered with %+v", got.Person)
	}
}

func jsonBody(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hasIdentity(p personSummary, want string) bool {
	for _, id := range p.Identities {
		if id == want {
			return true
		}
	}
	return false
}
