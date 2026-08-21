// Package mailingest fills the corpus from Gmail, by way of the docket CLI.
//
// Shelling out to docket rather than talking to Gmail directly keeps OAuth in
// one place — docket already owns the tokens, the refresh loop and the error
// messages. The cost is a subprocess per read, which is irrelevant next to the
// API round-trip it wraps.
package mailingest

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxBytes is passed to every read. docket truncates from the END of a body,
// and the end of a forward is where the OLDEST quoted material sits — so a
// capped read silently discards exactly the history that quoted-block
// extraction exists to recover. There is no unlimited sentinel: 0 and -1 both
// fall back to docket's 20 000-byte default.
const maxBytes = "99999999"

// Envelope is docket's search/list shape. Field names are docket's snake_case.
type Envelope struct {
	ID         string   `json:"id"`
	ThreadID   string   `json:"thread_id"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	Cc         string   `json:"cc"`
	Subject    string   `json:"subject"`
	Date       string   `json:"date"`
	MessageID  string   `json:"message_id"`
	InReplyTo  string   `json:"in_reply_to"`
	References []string `json:"references"`
	Labels     []string `json:"labels"`
	Snippet    string   `json:"snippet"`
}

// Message is docket's read shape: an envelope plus the body.
type Message struct {
	Envelope
	Body      string `json:"body"`
	Truncated bool   `json:"truncated"`
	// BodyHTML is the raw text/html part, exactly as the sender's client wrote
	// it. Nothing sanitises it — see issue #14. It is stored but NOT yet
	// preferred at render time, because the renderer injects with
	// dangerouslySetInnerHTML and the plain-text path is safe only because it
	// escapes everything.
	BodyHTML string `json:"body_html"`
	// HTMLStatus is "present" or "none", and empty from a docket too old to
	// know the flag — which is why it is a string rather than a bool.
	HTMLStatus    string `json:"html_status"`
	HTMLTruncated bool   `json:"html_truncated"`
	Attachments   []struct {
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		Size     int64  `json:"size"`
		PartID   string `json:"part_id"`
	} `json:"attachments"`
}

type envelope[T any] struct {
	OK    bool  `json:"ok"`
	Data  T     `json:"data"`
	Page  *Page `json:"page"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Page is docket's paging block, a sibling of `data` on a search or list.
//
// HasMore and NextPageToken are read together and never separately: a token is
// the server's own position in the result set, so a caller that stops on an
// empty token would also stop on a page that simply arrived without one. Both
// are absent from a docket old enough to predate paging, which is why a missing
// block is treated as a single complete page — see Search.
type Page struct {
	Returned      int    `json:"returned"`
	Limit         int    `json:"limit"`
	HasMore       bool   `json:"has_more"`
	NextPageToken string `json:"next_page_token"`
}

// Client runs docket. Bin defaults to "docket" on PATH.
type Client struct{ Bin string }

func (c Client) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "docket"
}

func run[T any](c Client, args ...string) (T, error) {
	data, _, err := runPaged[T](c, args...)
	return data, err
}

// runPaged also returns the paging block. Nil when docket did not send one.
func runPaged[T any](c Client, args ...string) (T, *Page, error) {
	var zero T
	cmd := exec.Command(c.bin(), args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return zero, nil, fmt.Errorf("%s %s: %w%s", c.bin(), strings.Join(args, " "), err,
			ifNotEmpty(": ", stderr))
	}
	var env envelope[T]
	if err := json.Unmarshal(out, &env); err != nil {
		return zero, nil, fmt.Errorf("parsing %s output: %w", c.bin(), err)
	}
	if !env.OK {
		msg := "unknown error"
		if env.Error != nil {
			msg = env.Error.Message
		}
		return zero, nil, fmt.Errorf("%s %s: %s", c.bin(), strings.Join(args, " "), msg)
	}
	return env.Data, env.Page, nil
}

// Search runs a Gmail query and returns one page of envelopes plus the paging
// block. Pass the previous page's NextPageToken to continue; pass "" to start.
//
// A docket too old to send a paging block yields a synthesised one with HasMore
// false. That is the truthful reading of what such a binary can tell us — it has
// no token to offer, so there is no continuation to take — and it keeps the walk
// in Ingest from spinning on a token that will never arrive. The result then
// reports the same single page it always did, and the limit is the only bound.
func (c Client) Search(query string, limit int, pageToken string) ([]Envelope, Page, error) {
	args := []string{"mail", "search", "--query", query, "--limit", fmt.Sprint(limit)}
	if pageToken != "" {
		args = append(args, "--page-token", pageToken)
	}
	envs, page, err := runPaged[[]Envelope](c, args...)
	if err != nil {
		return nil, Page{}, err
	}
	if page == nil {
		return envs, Page{Returned: len(envs), Limit: limit}, nil
	}
	return envs, *page, nil
}

// Read fetches one message in full, with its HTML part. Always at full size —
// see maxBytes.
//
// --html is unconditional. The HTML is the only place a link target survives:
// docket's plain-text rendering drops href attributes, so a message whose text
// reads "click here" loses the URL entirely and no later pass can recover it.
func (c Client) Read(id string) (Message, error) {
	return run[Message](c, "mail", "read", "--id", id, "--html", "--max-bytes", maxBytes)
}

// Thread returns every envelope in a thread.
func (c Client) Thread(id string) (struct {
	ThreadID string     `json:"thread_id"`
	Messages []Envelope `json:"messages"`
}, error) {
	return run[struct {
		ThreadID string     `json:"thread_id"`
		Messages []Envelope `json:"messages"`
	}](c, "mail", "thread", "--id", id)
}

// probed caches SupportsThreadingHeaders for the lifetime of the process.
//
// Keyed on the binary, not global: a Client pointed at a fake in a test and one
// pointed at the real docket are answering about different programs, and one
// cached answer for both would make a test's verdict depend on what ran before
// it. Nothing invalidates the cache — a docket replaced on disk mid-run is not a
// case worth carrying state for, and the next invocation re-probes anyway.
var probed struct {
	sync.Mutex
	answers map[string]bool
}

// SupportsThreadingHeaders reports whether the docket on PATH exposes the fields
// this package needs. An older build returns an envelope without them and would
// silently produce a corpus with no reply graph at all, so this fails closed.
//
// It retries once: the probe is a live API call, and a token refresh or a rate
// blip should not abort a backfill that may otherwise run for hours.
// The probe is one live API call, so it is cached for the process: a cron
// top-up walking a dozen containers paid for a dozen identical answers.
func (c Client) SupportsThreadingHeaders() (bool, error) {
	probed.Lock()
	defer probed.Unlock()
	if ok, seen := probed.answers[c.bin()]; seen {
		return ok, nil
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		out, err := run[[]Envelope](c, "mail", "search", "--query", "in:anywhere", "--limit", "1")
		if err != nil {
			lastErr = err
			continue
		}
		if len(out) == 0 {
			return false, fmt.Errorf(
				"%s returned no messages, so it is impossible to tell whether it exposes "+
					"threading headers", c.bin())
		}
		ok := out[0].MessageID != ""
		if probed.answers == nil {
			probed.answers = map[string]bool{}
		}
		probed.answers[c.bin()] = ok
		return ok, nil
	}
	return false, fmt.Errorf("probing %s for threading headers: %w", c.bin(), lastErr)
}

func ifNotEmpty(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}
