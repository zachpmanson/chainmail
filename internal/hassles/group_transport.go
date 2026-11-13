package hassles

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// A snapshot is full VCALENDAR evidence, not proof that omitted hrefs are absent.
type GroupSnapshot struct{ Objects []caldav.CalendarObject }
type GroupProbe struct {
	Absent bool
	Object caldav.CalendarObject
}
type GroupReader interface {
	Snapshot(context.Context, string) (GroupSnapshot, error)
	Probe(context.Context, string, string) (GroupProbe, error)
}
type GroupBackend interface {
	GroupReader
	Write(context.Context, string, string, string, string) error
}

var _ GroupBackend = (*Client)(nil)

// Bound nesting before entering the recursive library decoder, and require EOF
// after exactly one calendar (the library otherwise ignores additional input).
func groupDecodeCalendar(raw string) (calendar *ical.Calendar, err error) {
	defer func() {
		if recover() != nil {
			calendar = nil
			err = errors.New("malformed calendar data")
		}
	}()
	if len(raw) > 1<<20 || !utf8.ValidString(raw) {
		return nil, errors.New("calendar data exceeds safety limit or is not UTF-8")
	}
	// Unfold exactly as the decoder does before checking component boundaries.
	unfolded := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\n ", ""), "\n\t", "")
	depth, nodes, roots := 0, 0, 0
	for _, line := range strings.Split(unfolded, "\n") {
		name := strings.ToUpper(strings.SplitN(strings.SplitN(line, ";", 2)[0], ":", 2)[0])
		if name == "BEGIN" {
			if depth == 0 {
				roots++
			}
			depth++
			nodes++
		} else if depth == 0 && line != "" {
			return nil, errors.New("data outside calendar")
		}
		if depth > dailyPartsComponentDepthLimit || nodes > dailyPartsComponentNodeLimit {
			return nil, errors.New("calendar tree exceeds safety limit")
		}
		if name == "END" {
			depth--
		}
		if depth < 0 {
			return nil, errors.New("unbalanced calendar")
		}
	}
	if roots != 1 || depth != 0 {
		return nil, errors.New("expected exactly one complete calendar")
	}
	d := ical.NewDecoder(strings.NewReader(raw))
	c, err := d.Decode()
	if err != nil {
		return nil, err
	}
	if _, err = d.Decode(); err != io.EOF {
		return nil, errors.New("additional or trailing calendar data")
	}
	if err = dailyPartsValidateTree(c.Component); err != nil {
		return nil, err
	}
	b, err := json.Marshal(c)
	if err != nil || len(b) > 1<<20 || len(c.Events()) > 1000 {
		return nil, errors.New("calendar resource exceeds safety limit")
	}
	return c, nil
}

func groupTransportTag(tag string) bool {
	return dailyPartsStrongETag(tag) && utf8.ValidString(tag)
}

func (c *Client) Probe(ctx context.Context, calendar, href string) (GroupProbe, error) {
	path, err := Resource(calendar, href)
	if err != nil {
		return GroupProbe{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", Origin+(&url.URL{Path: path}).EscapedPath(), nil)
	if err != nil {
		return GroupProbe{}, err
	}
	req.Header.Set("Accept", "text/calendar")
	res, err := c.http.Do(req)
	if err != nil {
		return GroupProbe{}, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return GroupProbe{Absent: true}, nil
	}
	if res.StatusCode != http.StatusOK {
		return GroupProbe{}, fmt.Errorf("calendar probe failed (HTTP %d)", res.StatusCode)
	}
	if len(res.Header.Values("ETag")) != 1 || !groupTransportTag(res.Header.Get("ETag")) {
		return GroupProbe{}, errors.New("strong calendar ETag required")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return GroupProbe{}, err
	}
	data, err := groupDecodeCalendar(string(raw))
	if err != nil {
		return GroupProbe{}, err
	}
	return GroupProbe{Object: caldav.CalendarObject{Path: path, ETag: res.Header.Get("ETag"), Data: data}}, nil
}

const groupReport = `<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data><c:comp name="VCALENDAR"><c:allprop/><c:allcomp/></c:comp></c:calendar-data></d:prop><c:filter><c:comp-filter name="VCALENDAR"/></c:filter></c:calendar-query>`

// Decode our bounded REPORT directly: QueryCalendar decodes only the first
// VCALENDAR and cannot attest that calendar-data had no ignored trailing bytes.
func (c *Client) Snapshot(ctx context.Context, calendar string) (GroupSnapshot, error) {
	collection, err := Collection(calendar)
	if err != nil {
		return GroupSnapshot{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "REPORT", Origin+(&url.URL{Path: collection}).EscapedPath(), strings.NewReader(groupReport))
	if err != nil {
		return GroupSnapshot{}, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	res, err := c.http.Do(req)
	if err != nil {
		return GroupSnapshot{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		return GroupSnapshot{}, fmt.Errorf("calendar snapshot failed (HTTP %d)", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return GroupSnapshot{}, errors.New("calendar response exceeds safety limit")
	}
	if err = validateResponseHrefs(raw, req); err != nil {
		return GroupSnapshot{}, err
	}
	return groupDecodeReport(raw, collection)
}

func groupDecodeReport(raw []byte, collection string) (GroupSnapshot, error) {
	type prop struct {
		Tags []string            `xml:"DAV: getetag"`
		Data []groupCalendarData `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	}
	type stat struct {
		Status string `xml:"DAV: status"`
		Prop   prop   `xml:"DAV: prop"`
	}
	type row struct {
		Hrefs  []string `xml:"DAV: href"`
		Stats  []stat   `xml:"DAV: propstat"`
		Status string   `xml:"DAV: status"`
	}
	var report struct {
		XMLName xml.Name `xml:"DAV: multistatus"`
		Rows    []row    `xml:"DAV: response"`
	}
	if err := xml.Unmarshal(raw, &report); err != nil {
		return GroupSnapshot{}, err
	}
	// encoding/xml also accepts trailing XML: reject it explicitly.
	d := xml.NewDecoder(strings.NewReader(string(raw)))
	roots := 0
	depth := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return GroupSnapshot{}, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return GroupSnapshot{}, errors.New("trailing XML data")
			}
		}
	}
	if roots != 1 || len(report.Rows) > 2000 {
		return GroupSnapshot{}, errors.New("invalid or oversized calendar snapshot")
	}
	out := GroupSnapshot{}
	seen := map[string]bool{}
	for _, row := range report.Rows {
		if len(row.Hrefs) != 1 || row.Status != "" {
			return GroupSnapshot{}, errors.New("incomplete calendar REPORT row")
		}
		path, err := Resource(collection, row.Hrefs[0])
		if err != nil || seen[path] {
			return GroupSnapshot{}, errors.New("duplicate or unsafe calendar REPORT href")
		}
		seen[path] = true
		var tags, data []string
		for _, stat := range row.Stats {
			if stat.Status != "HTTP/1.1 200 OK" && stat.Status != "HTTP/2 200 OK" {
				return GroupSnapshot{}, errors.New("incomplete calendar REPORT properties")
			}
			tags = append(tags, stat.Prop.Tags...)
			for _, value := range stat.Prop.Data {
				data = append(data, string(value))
			}
		}
		if len(tags) != 1 || !groupTransportTag(tags[0]) || len(data) != 1 {
			return GroupSnapshot{}, errors.New("missing or duplicate calendar data/ETag")
		}
		cal, err := groupDecodeCalendar(data[0])
		if err != nil {
			return GroupSnapshot{}, err
		}
		out.Objects = append(out.Objects, caldav.CalendarObject{Path: path, ETag: tags[0], Data: cal})
	}
	return out, nil
}

// Calendar data is text, not an XML subtree whose children may be ignored by
// encoding/xml's string decoder.
type groupCalendarData string

func (data *groupCalendarData) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var b strings.Builder
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch t := token.(type) {
		case xml.CharData:
			if b.Len() > 1<<20-len(t) {
				return errors.New("oversized calendar data")
			}
			b.Write(t)
		case xml.EndElement:
			if t.Name != start.Name {
				return errors.New("invalid calendar-data element")
			}
			*data = groupCalendarData(b.String())
			return nil
		default:
			return errors.New("nontext calendar-data element")
		}
	}
}
