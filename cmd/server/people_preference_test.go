package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestEditPersonResetsOriginalPreference(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	ada := personOf(t, srv, "ada@loomworks.example")
	bo := personOf(t, srv, "bo@fjordline.example")
	path := fmt.Sprintf("/v1/people/%d", ada)
	post := func(path, body string, want int) {
		t.Helper()
		res := srv.do(t, "POST", path, []byte(body))
		if res.status != want {
			t.Fatalf("POST %s %s: status = %d, want %d: %s", path, body, res.status, want, res.body)
		}
		if want == 200 {
			api.assert(t, "PersonResponse", res.body)
		} else {
			api.assert(t, "Error", res.body)
		}
	}
	assertPreference := func(ext string, id int64, want, set bool) {
		t.Helper()
		res := srv.do(t, "GET", entryPath("/v1/entries/", ext), nil)
		if res.status != 200 {
			t.Fatalf("GET entry: status = %d: %s", res.status, res.body)
		}
		api.assert(t, "CorpusEntry", res.body)
		got := decode[corpusEntry](t, res)
		if got.PersonID != id || got.PreferOriginal != want || got.PreferOriginalSet != set {
			t.Fatalf("entry preference = (%d, %v, %v), want (%d, %v, %v)",
				got.PersonID, got.PreferOriginal, got.PreferOriginalSet, id, want, set)
		}
	}
	post(fmt.Sprintf("/v1/people/%d", bo), `{"preferOriginal":true}`, 200)
	before := decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	for _, forced := range []bool{false, true} {
		post(path, fmt.Sprintf(`{"preferOriginal":%v}`, forced), 200)
		assertPreference(extAda1, ada, forced, true)
		for _, body := range []string{
			`{"displayName":"Ada Okoye"}`,
			`{"displayName":"Ada Okoye","preferOriginal":null}`,
			`{"displayName":"Ada Okoye","resetOriginalPreference":false}`,
			`{"displayName":"Ada Okoye","resetOriginalPreference":null}`,
		} {
			post(path, body, 200)
			assertPreference(extAda1, ada, forced, true)
		}
		for range 2 {
			post(path, `{"resetOriginalPreference":true,"preferOriginal":null}`, 200)
			assertPreference(extAda1, ada, false, false)
			assertPreference(extAda3, ada, false, false)
		}
	}
	after := decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("reset changed other fields or people: before %+v, after %+v", before, after)
	}
	assertPreference(extBo2, bo, true, true)

	post(path, `{"preferOriginal":true}`, 200)
	before = decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	for _, forced := range []bool{false, true} {
		post(path, fmt.Sprintf(`{"displayName":"Changed Name","addIdentities":["email:new@example.com"],"removeIdentities":["email:ada@loomworks.example"],"resetOriginalPreference":true,"preferOriginal":%v}`, forced), 400)
		assertPreference(extAda1, ada, true, true)
	}
	after = decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("conflict partially applied: before %+v, after %+v", before, after)
	}
	// Reset composes with the existing editor without replacing its other fields.
	post(path, `{"displayName":"Ada Nwosu","addIdentities":["email:new@example.com"],"resetOriginalPreference":true}`, 200)
	assertPreference(extAda1, ada, false, false)
	people := decode[peopleResponse](t, srv.do(t, "GET", "/v1/people", nil))
	for _, p := range people.People {
		if p.PersonID == ada && (p.DisplayName != "Ada Nwosu" || !hasIdentity(p, "email:new@example.com") || !hasIdentity(p, "email:ada@loomworks.example")) {
			t.Fatalf("reset lost other edit fields: %+v", p)
		}
	}
}

func TestEditPersonRejectsInvalidPreferenceResets(t *testing.T) {
	srv, api := testServer(t), loadAPI(t)
	ada := personOf(t, srv, "ada@loomworks.example")
	for _, tc := range []struct {
		id   string
		body string
		want int
	}{
		{fmt.Sprint(ada), `{"resetOriginalPreference":false}`, 400},
		{fmt.Sprint(ada), `{"resetOriginalPreference":null}`, 400},
		{fmt.Sprint(ada), `{"preferOriginal":null}`, 400},
		{fmt.Sprint(ada), `{"resetOriginalPreference":"true"}`, 400},
		{fmt.Sprint(ada), `{"resetOriginalPreference":1}`, 400},
		{fmt.Sprint(ada), `{"resetOriginalPreference":true,"unknown":true}`, 400},
		{"0", `{"resetOriginalPreference":true}`, 400},
		{"-1", `{"resetOriginalPreference":true}`, 400},
		{"ada", `{"resetOriginalPreference":true}`, 400},
		{"9999", `{"resetOriginalPreference":true}`, 404},
	} {
		res := srv.do(t, "POST", "/v1/people/"+tc.id, []byte(tc.body))
		if res.status != tc.want {
			t.Errorf("id %s body %s: status = %d, want %d: %s", tc.id, tc.body, res.status, tc.want, res.body)
		}
		api.assert(t, "Error", res.body)
	}
}
