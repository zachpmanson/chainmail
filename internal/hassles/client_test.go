package hassles

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

type rewriteTransport struct{ target *url.URL }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = rt.target.Scheme
	u.Host = rt.target.Host
	copy.URL = &u
	return http.DefaultTransport.RoundTrip(copy)
}
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	target, _ := url.Parse(s.URL)
	c, err := NewClient(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "offline-fake-token"}), "person@example.test")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = guarded{&oauth2.Transport{Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "offline-fake-token"}), Base: rewriteTransport{target}}}
	return c
}
func escaped(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func multistatus(href, body string) string {
	return `<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:href>` + escaped(href) + `</d:href><d:propstat><d:prop>` + body + `</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`
}

func TestRawReportAndConditionalHTTP(t *testing.T) {
	collection := "/caldav/v2/reminders@example.test/events/"
	o := object(t, collection+"not-the-uid.ics")
	raw, _ := Encode(o.Data)
	var calls int
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer offline-fake-token" {
			t.Error("OAuth missing")
		}
		b, _ := io.ReadAll(r.Body)
		switch r.Method {
		case "REPORT":
			if strings.Contains(string(b), "time-range") || strings.Contains(string(b), "expand") || !strings.Contains(string(b), "VEVENT") {
				t.Errorf("not all masters: %s", b)
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			_, _ = io.WriteString(w, multistatus(o.Path, `<d:getetag>"v1"</d:getetag><c:calendar-data>`+escaped(raw)+`</c:calendar-data>`))
		case "PUT":
			if r.URL.Path != o.Path || r.Header.Get("If-Match") != `"v1"` {
				t.Errorf("unsafe update %+v", r)
			}
			w.WriteHeader(412)
		case "DELETE":
			if r.URL.Path != o.Path || r.Header.Get("If-Match") != `"v1"` {
				t.Errorf("unsafe delete %+v", r)
			}
			w.WriteHeader(204)
		}
	})
	objects, err := c.Objects(context.Background(), collection)
	if err != nil || len(objects) != 1 || objects[0].Path != o.Path || objects[0].ETag != `"v1"` {
		t.Fatalf("raw objects %+v %v", objects, err)
	}
	if err = c.Write(context.Background(), collection, o.Path, `"v1"`, raw); !errors.Is(err, ErrConflict) {
		t.Fatal("expected conditional conflict", err)
	}
	if err = c.Write(context.Background(), collection, o.Path, `"v1"`, ""); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
func TestDiscoveryAndCreateCondition(t *testing.T) {
	calls := 0
	collection := "/caldav/v2/overlay@example.test/events/"
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/xml")
		switch calls {
		case 1:
			if r.URL.Path != "/caldav/v2/person@example.test/user" {
				t.Errorf("undocumented discovery path: %s", r.URL.Path)
			}
			w.WriteHeader(207)
			io.WriteString(w, multistatus("/caldav/v2/person@example.test/user", `<c:calendar-home-set><d:href>/caldav/v2/person@example.test/</d:href></c:calendar-home-set>`))
		case 2:
			w.WriteHeader(207)
			io.WriteString(w, multistatus(collection, `<d:resourcetype><d:collection/><c:calendar/></d:resourcetype><d:displayname>Reminders</d:displayname><c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set>`))
		case 3:
			if r.Method != "PUT" || r.Header.Get("If-None-Match") != "*" || r.Header.Get("If-Match") != "" {
				t.Error("missing create condition")
			}
			w.WriteHeader(201)
		}
	})
	calendars, err := c.Calendars(context.Background())
	if err != nil || len(calendars) != 1 || calendars[0].ID != collection {
		t.Fatalf("discovery %+v %v", calendars, err)
	}
	if err = c.Write(context.Background(), collection, collection+"new.ics", "*", "calendar"); err != nil {
		t.Fatal(err)
	}
}
func TestHrefConfinementAndNoRedirects(t *testing.T) {
	collection := "/caldav/v2/a@example.test/events/"
	for _, href := range []string{"https://attacker.test/caldav/v2/a@example.test/events/x", "//attacker.test/caldav/v2/a@example.test/events/x", "/caldav/v2/other/events/x", "/caldav/v2/a@example.test/events/../other/x", "/caldav/v2/a@example.test/events/%252e%252e", "/caldav/v2/a@example.test/events/x?token=1", "/caldav/v2/a@example.test/events/x#frag", "/caldav/v2/a@example.test/events/x/y", "/caldav/v2/a@example.test/events/%2e%2e/x", "http://apidata.googleusercontent.com/caldav/v2/a@example.test/events/x"} {
		if _, err := Resource(collection, href); err == nil {
			t.Errorf("accepted %s", href)
		}
	}
	if _, err := Resource(collection, "https://apidata.googleusercontent.com"+collection+"x.ics"); err != nil {
		t.Fatal(err)
	}
	if c, err := Collection("/caldav/v2/a%40example.test/events"); err != nil || c != collection {
		t.Fatal(c, err)
	}
	reached := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.Header().Set("Location", "https://attacker.test/")
		w.WriteHeader(302)
	})
	if _, err := c.Objects(context.Background(), collection); err == nil || reached != 1 {
		t.Fatal("followed redirect", err, reached)
	}
	req, _ := http.NewRequest("GET", "https://attacker.test/", nil)
	if _, err := c.http.Do(req); err == nil || reached != 1 {
		t.Fatal("foreign OAuth request escaped", err)
	}
}
func TestReturnedResourceCannotEscapeCollection(t *testing.T) {
	o := object(t, "/caldav/v2/other/events/x.ics")
	raw, _ := Encode(o.Data)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(207)
		io.WriteString(w, multistatus(o.Path, `<d:getetag>"1"</d:getetag><c:calendar-data>`+escaped(raw)+`</c:calendar-data>`))
	})
	if _, err := c.Objects(context.Background(), "/caldav/v2/selected/events/"); err == nil {
		t.Fatal("accepted foreign object")
	}
}

type revocableSource struct {
	revoked bool
	calls   int
}

func (s *revocableSource) Token() (*oauth2.Token, error) {
	s.calls++
	if s.revoked {
		return nil, errors.New("revoked")
	}
	return &oauth2.Token{AccessToken: "fake-revocable"}, nil
}
func TestCalendarTransportRechecksTokenOnEveryRequest(t *testing.T) {
	reached := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { reached++; w.WriteHeader(204) })
	// Preserve the offline rewrite transport, swapping only the token source.
	transport := c.http.Transport.(guarded).next.(*oauth2.Transport)
	source := &revocableSource{}
	transport.Source = source
	href := "/caldav/v2/selected/events/x.ics"
	collection := "/caldav/v2/selected/events/"
	if err := c.Write(context.Background(), collection, href, `"1"`, ""); err != nil {
		t.Fatal(err)
	}
	source.revoked = true
	if err := c.Write(context.Background(), collection, href, `"1"`, ""); err == nil {
		t.Fatal("cached token bypassed revocation")
	}
	if reached != 1 || source.calls != 2 {
		t.Fatalf("requests=%d token reads=%d", reached, source.calls)
	}
}

func TestCalendarResponseBodyIsBounded(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxResponse+1))
	})
	if _, err := c.Objects(context.Background(), "/caldav/v2/selected/events/"); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("unbounded response: %v", err)
	}
}

func TestForeignHrefOriginIsRejectedBeforeLibraryStripsIt(t *testing.T) {
	collection := "/caldav/v2/selected/events/"
	o := object(t, collection+"x.ics")
	raw, _ := Encode(o.Data)
	for _, href := range []string{"https://attacker.test" + o.Path, Origin + o.Path + "?other=calendar", Origin + o.Path + "#fragment"} {
		t.Run(href, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(207)
				io.WriteString(w, multistatus(href, `<d:getetag>"1"</d:getetag><c:calendar-data>`+escaped(raw)+`</c:calendar-data>`))
			})
			if _, err := c.Objects(context.Background(), collection); err == nil {
				t.Fatal("library discarded a foreign href's origin/query before validation")
			}
		})
	}
}

func TestClientRequiresAnExplicitKnownGoogleEmail(t *testing.T) {
	for _, email := range []string{"", "not-an-email", "person@example.test/other", "Person <person@example.test>", "person%40example.test"} {
		if _, err := NewClient(nil, email); err == nil {
			t.Fatalf("accepted unknown/unsafe email %q", email)
		}
	}
}
