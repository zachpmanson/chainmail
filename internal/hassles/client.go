package hassles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-webdav/caldav"
	"golang.org/x/oauth2"
)

const Origin = "https://apidata.googleusercontent.com"
const maxResponse = 16 << 20

var ErrConflict = errors.New("calendar changed; refresh before trying again")

type Calendar struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Location struct {
	AccountID  string `json:"accountId"`
	CalendarID string `json:"calendarId"`
}

type Backend interface {
	Calendars(context.Context) ([]Calendar, error)
	Objects(context.Context, string) ([]caldav.CalendarObject, error)
	Write(context.Context, string, string, string, string) error
}

type Client struct {
	http      *http.Client
	dav       *caldav.Client
	principal string
}

// The guard runs BEFORE OAuth, including on library-generated discovery requests.
// No redirect or returned href can send a bearer token outside the fixed origin.
type guarded struct{ next http.RoundTripper }

func (g guarded) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != "apidata.googleusercontent.com" || r.URL.User != nil || r.URL.RawQuery != "" || r.URL.Fragment != "" {
		return nil, errors.New("refused calendar origin")
	}
	res, err := g.next.RoundTrip(r)
	if err != nil {
		return nil, errors.New("calendar connection/authentication failed; reconnect the selected account if needed")
	}
	// Buffer bounded XML before go-webdav decodes it.
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || len(b) > maxResponse {
		return nil, errors.New("calendar response exceeded safety limit")
	}
	if res.StatusCode == http.StatusMultiStatus || strings.Contains(res.Header.Get("Content-Type"), "xml") {
		if err := validateResponseHrefs(b, r); err != nil {
			return nil, err
		}
	}
	res.Body = io.NopCloser(strings.NewReader(string(b)))
	return res, nil
}

func NewClient(source oauth2.TokenSource, accountEmail string) (*Client, error) {
	address, err := mail.ParseAddress(accountEmail)
	if err != nil || address.Address != accountEmail || strings.ContainsAny(accountEmail, "/\\%?#\r\n") {
		return nil, errors.New("selected account needs a known Google email; reconnect it before using Hassles")
	}
	principal := "/caldav/v2/" + accountEmail + "/user"
	h := &http.Client{Transport: guarded{&oauth2.Transport{Source: source}}, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("calendar redirects refused") }}
	d, err := caldav.NewClient(h, Origin)
	if err != nil {
		return nil, err
	}
	return &Client{http: h, dav: d, principal: principal}, nil
}

func canonical(href string) (string, error) {
	u, err := url.Parse(href)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("invalid calendar href")
	}
	if u.IsAbs() {
		if u.Scheme != "https" || u.Host != "apidata.googleusercontent.com" {
			return "", errors.New("foreign calendar href")
		}
	} else if u.Host != "" {
		return "", errors.New("foreign calendar href")
	}
	p := u.Path
	if !strings.HasPrefix(p, "/caldav/v2/") || strings.ContainsAny(p, "\\?#") || strings.IndexFunc(p, func(ch rune) bool { return ch < 0x20 || ch == 0x7f }) >= 0 || strings.Contains(p, "//") || path.Clean(p) != strings.TrimSuffix(p, "/") {
		return "", errors.New("unsafe calendar href")
	}
	// Reject double escaping that another HTTP layer could interpret differently.
	if strings.Contains(p, "%") {
		return "", errors.New("double-escaped calendar href")
	}
	return p, nil
}

func Collection(href string) (string, error) {
	p, err := canonical(href)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 4 || parts[3] != "events" || parts[2] == "" {
		return "", errors.New("not a Google event collection")
	}
	return strings.TrimSuffix(p, "/") + "/", nil
}
func Resource(collection, href string) (string, error) {
	c, err := Collection(collection)
	if err != nil {
		return "", err
	}
	p, err := canonical(href)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(p, c) || strings.Contains(strings.TrimPrefix(p, c), "/") || p == c {
		return "", errors.New("resource outside selected calendar")
	}
	return p, nil
}
func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	// Google documents the account-email principal, not bare-root discovery.
	home, err := c.dav.FindCalendarHomeSet(ctx, c.principal)
	if err != nil {
		return nil, fmt.Errorf("calendar discovery denied; reconnect with Calendar permission: %w", err)
	}
	if _, err = canonical(home); err != nil {
		return nil, err
	}
	all, err := c.dav.FindCalendars(ctx, home)
	if err != nil {
		return nil, err
	}
	if len(all) > 500 {
		return nil, errors.New("calendar discovery exceeds safety limit")
	}
	result := []Calendar{}
	seen := map[string]bool{}
	for _, cal := range all {
		id, err := Collection(cal.Path)
		if err != nil || seen[id] || strings.Contains(id, ".v.calendar.google.com") {
			continue
		}
		supported := len(cal.SupportedComponentSet) == 0
		for _, comp := range cal.SupportedComponentSet {
			supported = supported || comp == "VEVENT"
		}
		if !supported {
			continue
		}
		seen[id] = true
		result = append(result, Calendar{ID: id, Name: cal.Name})
	}
	return result, nil
}
func (c *Client) Objects(ctx context.Context, calendar string) ([]caldav.CalendarObject, error) {
	collection, err := Collection(calendar)
	if err != nil {
		return nil, err
	}
	objects, err := c.dav.QueryCalendar(ctx, collection, &caldav.CalendarQuery{CompRequest: caldav.CalendarCompRequest{Name: "VCALENDAR", AllProps: true, AllComps: true}, CompFilter: caldav.CompFilter{Name: "VCALENDAR", Comps: []caldav.CompFilter{{Name: "VEVENT"}}}})
	if err != nil {
		return nil, fmt.Errorf("reading reminders denied/failed; check Calendar permission: %w", err)
	}
	if len(objects) > 2000 {
		return nil, errors.New("calendar exceeds 2000-resource safety limit")
	}
	for i := range objects {
		// go-webdav exposes the unquoted opaque tag, not the HTTP header value.
		if objects[i].ETag != "" {
			for _, ch := range objects[i].ETag {
				if ch < 0x21 || ch == '"' || ch == 0x7f {
					return nil, errors.New("invalid calendar ETag")
				}
			}
			objects[i].ETag = `"` + objects[i].ETag + `"`
		}
		objects[i].Path, err = Resource(collection, objects[i].Path)
		if err != nil {
			return nil, err
		}
		if objects[i].Data != nil {
			raw, e := json.Marshal(objects[i].Data)
			if e != nil || len(raw) > 1<<20 || len(objects[i].Data.Events()) > 1000 {
				return nil, errors.New("calendar resource exceeds safety limit")
			}
		}
	}
	return objects, nil
}
func (c *Client) Write(ctx context.Context, calendar, href, etag, body string) error {
	resource, err := Resource(calendar, href)
	if err != nil {
		return err
	}
	method := "PUT"
	if body == "" {
		method = "DELETE"
	}
	if etag == "" || strings.ContainsAny(etag, "\r\n") || strings.HasPrefix(etag, "W/") {
		return errors.New("strong conditional ETag required")
	}
	req, err := http.NewRequestWithContext(ctx, method, Origin+(&url.URL{Path: resource}).EscapedPath(), strings.NewReader(body))
	if err != nil {
		return err
	}
	if etag == "*" {
		if method != "PUT" {
			return errors.New("invalid delete condition")
		}
		req.Header.Set("If-None-Match", "*")
	} else {
		req.Header.Set("If-Match", etag)
	}
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("calendar write outcome unknown; refresh before retrying")
	}
	defer res.Body.Close()
	if res.StatusCode == 412 || res.StatusCode == 409 || res.StatusCode == 404 {
		return ErrConflict
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("calendar write denied/failed (HTTP %d); check write permission", res.StatusCode)
	}
	return nil
}
