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
	Body        string `json:"body"`
	Truncated   bool   `json:"truncated"`
	Attachments []struct {
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		Size     int64  `json:"size"`
		PartID   string `json:"part_id"`
	} `json:"attachments"`
}

type envelope[T any] struct {
	OK    bool `json:"ok"`
	Data  T    `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
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
	var zero T
	cmd := exec.Command(c.bin(), args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return zero, fmt.Errorf("%s %s: %w%s", c.bin(), strings.Join(args, " "), err,
			ifNotEmpty(": ", stderr))
	}
	var env envelope[T]
	if err := json.Unmarshal(out, &env); err != nil {
		return zero, fmt.Errorf("parsing %s output: %w", c.bin(), err)
	}
	if !env.OK {
		msg := "unknown error"
		if env.Error != nil {
			msg = env.Error.Message
		}
		return zero, fmt.Errorf("%s %s: %s", c.bin(), strings.Join(args, " "), msg)
	}
	return env.Data, nil
}

// Search runs a Gmail query and returns envelopes.
func (c Client) Search(query string, limit int) ([]Envelope, error) {
	return run[[]Envelope](c, "mail", "search", "--query", query, "--limit", fmt.Sprint(limit))
}

// Read fetches one message in full. Always at full size — see maxBytes.
func (c Client) Read(id string) (Message, error) {
	return run[Message](c, "mail", "read", "--id", id, "--max-bytes", maxBytes)
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

// SupportsThreadingHeaders reports whether the docket on PATH exposes the
// fields this package needs. An older build returns an envelope without them,
// and would silently produce a corpus with no reply graph at all.
func (c Client) SupportsThreadingHeaders() (bool, error) {
	out, err := run[[]Envelope](c, "mail", "search", "--query", "has:nouserlabels", "--limit", "1")
	if err != nil {
		return false, err
	}
	if len(out) == 0 {
		return false, fmt.Errorf("no messages returned, cannot tell whether %s exposes threading headers", c.bin())
	}
	return out[0].MessageID != "", nil
}

func ifNotEmpty(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}
