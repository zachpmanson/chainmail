package hassles

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func groupTransportCalendar(t *testing.T) string {
	t.Helper()
	_, o := groupReceiptFixture(t)
	raw, err := Encode(o.Data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func groupReportProps(raw string) string {
	return `<d:getetag>"actual"</d:getetag><c:calendar-data>` + escaped(raw) + `</c:calendar-data>`
}
func TestGroupClientFullSnapshotAndActualUnconditionalProbe(t *testing.T) {
	collection := dailyGroupLocation.CalendarID
	href := collection + "actual-native.ics"
	raw := groupTransportCalendar(t)
	todo := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VTODO\r\nDESCRIPTION:opaque todo without UID\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	count := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Header.Get("Authorization") != "Bearer offline-fake-token" {
			t.Error("OAuth missing")
		}
		if r.URL.Scheme != "" || r.Host != "apidata.googleusercontent.com" {
			t.Error("request lost fixed origin")
		}
		switch r.Method {
		case "REPORT":
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "VEVENT") || strings.Contains(string(body), "time-range") || strings.Contains(string(body), "expand") || !strings.Contains(string(body), `name="VCALENDAR"`) {
				t.Errorf("filtered snapshot %s", body)
			}
			if r.Header.Get("Depth") != "1" {
				t.Error("wrong depth")
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			report := multistatus(href, groupReportProps(raw))
			todoRow := multistatus(collection+"todo.ics", groupReportProps(todo))
			index := strings.Index(todoRow, "<d:response>")
			report = strings.Replace(report, "</d:multistatus>", todoRow[index:], 1)
			_, _ = io.WriteString(w, report)
		case "GET":
			if r.URL.Path != href || r.Header.Get("If-Match") != "" || r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
				t.Error("not actual unconditional href GET")
			}
			w.Header().Set("ETag", `"actual"`)
			_, _ = io.WriteString(w, raw)
		default:
			t.Error("unexpected method", r.Method)
		}
	})
	snapshot, err := c.Snapshot(context.Background(), collection)
	if err != nil || len(snapshot.Objects) != 2 || snapshot.Objects[1].Data.Children[0].Name != "VTODO" {
		t.Fatal(snapshot, err)
	}
	probe, err := c.Probe(context.Background(), collection, href)
	if err != nil || probe.Absent || !groupSame(probe.Object, snapshot.Objects[0]) || count != 2 {
		t.Fatal(probe, err, count)
	}
}
func TestGroupClientProbeOnly404IsAbsent(t *testing.T) {
	raw := groupTransportCalendar(t)
	collection := dailyGroupLocation.CalendarID
	href := collection + "original.ics"
	for _, tc := range []struct {
		name          string
		status        int
		tag, body     string
		absent, valid bool
	}{
		{"404", 404, "", "", true, true},
		{"403", 403, "", "", false, false},
		{"304", 304, "", "", false, false},
		{"500", 500, "", "", false, false},
		{"200", 200, `"actual"`, raw, false, true},
		{"200todo", 200, `"actual"`, strings.ReplaceAll(raw, "VEVENT", "VTODO"), false, true},
		{"200noUID", 200, `"actual"`, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VTODO\r\nEND:VTODO\r\nEND:VCALENDAR\r\n", false, true},
		{"missing-etag", 200, "", raw, false, false},
		{"weak-etag", 200, `W/"weak"`, raw, false, false},
		{"unquoted-etag", 200, "notquoted", raw, false, false},
		{"native-obs-text-etag", 200, `"é"`, raw, false, true},
		{"missing-body", 200, `"actual"`, "", false, false},
		{"malformed", 200, `"actual"`, "BEGIN:VCALENDAR\r\nX-MALFORMED;X=abc\r\nEND:VCALENDAR\r\n", false, false},
		{"double-calendar", 200, `"actual"`, raw + raw, false, false},
		{"trailing", 200, `"actual"`, raw + "junk\r\n", false, false},
		{"trailing-no-newline", 200, `"actual"`, raw + "junk", false, false},
		{"oversize", 200, `"actual"`, strings.Repeat("x", (1<<20)+1), false, false},
		{"deep", 200, `"actual"`, "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEGIN;X=Y:VALARM\r\n", 65), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", tc.tag)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			result, err := c.Probe(context.Background(), collection, href)
			if (err == nil) != tc.valid || result.Absent != tc.absent {
				t.Fatalf("probe %+v err=%v", result, err)
			}
		})
	}
	// Requests never follow redirects or attempt a foreign/other-collection href.
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "https://attacker.test/")
		w.WriteHeader(302)
	})
	if p, err := c.Probe(context.Background(), collection, href); err == nil || p.Absent || calls != 1 {
		t.Fatal("redirect accepted", p, err, calls)
	}
	for _, h := range []string{"https://attacker.test" + href, "/caldav/v2/other@example.test/events/x.ics", href + "?token=leak", href + "#fragment"} {
		if _, err := c.Probe(context.Background(), collection, h); err == nil || calls != 1 {
			t.Fatal("href guard bypass", h, err)
		}
	}
	if _, err := c.Snapshot(context.Background(), collection); err == nil || calls != 2 {
		t.Fatal("snapshot followed redirect", err, calls)
	}
	if _, err := c.Snapshot(context.Background(), "https://attacker.test"+collection); err == nil || calls != 2 {
		t.Fatal("foreign snapshot origin", err, calls)
	}
	c.http.Transport = groupFailTransport{}
	if p, err := c.Probe(context.Background(), collection, href); err == nil || p.Absent {
		t.Fatal("transport failure means absence", p, err)
	}
}

type groupFailTransport struct{}

func (groupFailTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, groupTestError
}

func TestGroupClientSnapshotRejectsIncompleteOrIgnoredData(t *testing.T) {
	raw := groupTransportCalendar(t)
	collection := dailyGroupLocation.CalendarID
	href := collection + "actual.ics"
	good := multistatus(href, groupReportProps(raw))
	for _, tc := range []struct{ name, body string }{
		{"missing-data", multistatus(href, `<d:getetag>"actual"</d:getetag>`)},
		{"missing-tag", multistatus(href, `<c:calendar-data>`+escaped(raw)+`</c:calendar-data>`)},
		{"weak-tag", strings.Replace(good, `"actual"`, `W/"weak"`, 1)},
		{"double-data", multistatus(href, groupReportProps(raw)+`<c:calendar-data>`+escaped(raw)+`</c:calendar-data>`)},
		{"double-calendar", multistatus(href, groupReportProps(raw+raw))},
		{"trailing-calendar-text", multistatus(href, groupReportProps(raw+"ignored"))},
		{"nested-calendar-data", strings.Replace(good, "</c:calendar-data>", "<c:ignored>"+escaped(raw)+"</c:ignored></c:calendar-data>", 1)},
		{"trailing-xml", good + good},
		{"trailing-xml-text", good + "junk"},
		{"malformed-xml", good[:len(good)-20]},
		{"wrong-root", `<d:other xmlns:d="DAV:"/>`},
		{"propstat-gap", strings.Replace(good, "200 OK", "404 Not Found", 1)},
		{"foreign-href", multistatus("https://attacker.test"+href, groupReportProps(raw))},
		{"other-collection-href", multistatus("/caldav/v2/other@example.test/events/x.ics", groupReportProps(raw))},
		{"query-href", multistatus(href+"?access_token=leak", groupReportProps(raw))},
		{"duplicate-href", strings.Replace(good, "</d:multistatus>", good[strings.Index(good, "<d:response>"):], 1)},
		{"oversize-data", multistatus(href, groupReportProps(strings.Repeat("x", (1<<20)+1)))},
		{"oversize-transport", strings.Repeat("x", maxResponse+1)},
		{"deep-xml", `<d:multistatus xmlns:d="DAV:">` + strings.Repeat("<x>", 130) + strings.Repeat("</x>", 130) + `</d:multistatus>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(207)
				_, _ = io.WriteString(w, tc.body)
			})
			if _, err := c.Snapshot(context.Background(), collection); err == nil {
				t.Fatal("incomplete/ignored report admitted")
			}
		})
	}
	// Count the complete unfiltered resource set, not just VEVENTs.
	var b strings.Builder
	b.WriteString(`<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">`)
	small := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VTODO\r\nUID:unique\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	for i := 0; i < 2001; i++ {
		fmt.Fprintf(&b, `<d:response><d:href>%s%d.ics</d:href><d:propstat><d:prop>%s</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, collection, i, groupReportProps(small))
	}
	b.WriteString(`</d:multistatus>`)
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(207); _, _ = io.WriteString(w, b.String()) })
	if _, err := c.Snapshot(context.Background(), collection); err == nil {
		t.Fatal("2001-resource report accepted")
	}
}

func TestGroupDecodeCalendarBoundedSingleDocument(t *testing.T) {
	raw := groupTransportCalendar(t)
	for _, bad := range []string{raw + "BEGIN:VCALENDAR", raw + "x", strings.Repeat("BEGIN:VCALENDAR\r\n", 65), strings.Replace(raw, "BEGIN:VEVENT", "BEGIN;X=Y:VEVENT", 1) + strings.Repeat("junk", 1<<18)} {
		if _, err := groupDecodeCalendar(bad); err == nil {
			t.Fatal("ignored or unbounded calendar accepted")
		}
	}
	// Build directly; encoder's type requirements are not part of the bound test.
	var events strings.Builder
	events.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\n")
	for i := 0; i < 1001; i++ {
		fmt.Fprintf(&events, "BEGIN:VEVENT\r\nUID:%d\r\nEND:VEVENT\r\n", i)
	}
	events.WriteString("END:VCALENDAR\r\n")
	if _, err := groupDecodeCalendar(events.String()); err == nil {
		t.Fatal("event bound lost")
	}
}
