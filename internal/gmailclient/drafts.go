package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

var ErrDraftConflict = errors.New("Gmail draft changed outside Chainmail; reload before editing")

var ErrUnsupportedDraft = errors.New("this draft contains headers or MIME content Chainmail cannot preserve; edit it in Gmail")

// IsConflict reports an optimistic concurrency conflict caused by an external edit.
func IsConflict(err error) bool { return errors.Is(err, ErrDraftConflict) }

// IsNotFound reports a Gmail 404, used to distinguish an externally deleted
// draft from a transient mailbox failure.
func IsNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 404
}

// Draft is a Gmail-owned draft. IDs are mailbox-local; AccountID is supplied by
// the server that selected the credential used for this call.
type Draft struct {
	ID        string `json:"id"`
	MessageID string `json:"messageId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	To        string `json:"to,omitempty"`
	Cc        string `json:"cc,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Body      string `json:"body,omitempty"`
}

// ListDrafts lists drafts in Gmail and fetches each full message so callers can
// resume editing without relying on Gmail's usually-empty list snippets.
func (c Client) ListDrafts() ([]Draft, error) {
	var out []Draft
	page := ""
	for i := 0; i < 100; i++ {
		res, err := c.svc.Users.Drafts.List("me").MaxResults(100).PageToken(page).Do()
		if err != nil {
			return nil, fmt.Errorf("listing Gmail drafts: %w", err)
		}
		for _, item := range res.Drafts {
			if item == nil || item.Id == "" {
				continue
			}
			draft, err := c.GetDraft(item.Id)
			if IsNotFound(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("reading Gmail draft %q: %w", item.Id, err)
			}
			out = append(out, draft)
		}
		if res.NextPageToken == "" {
			return out, nil
		}
		if res.NextPageToken == page {
			return nil, fmt.Errorf("Gmail returned the same draft page token twice")
		}
		page = res.NextPageToken
	}
	return nil, fmt.Errorf("Gmail draft listing exceeded 100 pages")
}

// GetDraft reads the latest saved content from Gmail. A deleted or stale draft
// is reported as an error instead of being reconstructed from client state.
func (c Client) GetDraft(id string) (Draft, error) {
	if strings.TrimSpace(id) == "" {
		return Draft{}, fmt.Errorf("draft id is required")
	}
	got, err := c.svc.Users.Drafts.Get("me", id).Format("raw").Do()
	if err != nil {
		return Draft{}, fmt.Errorf("getting Gmail draft %q: %w", id, err)
	}
	if got.Message == nil {
		return Draft{}, fmt.Errorf("Gmail draft %q has no message", id)
	}
	raw, err := base64.RawURLEncoding.DecodeString(got.Message.Raw)
	if err != nil {
		return Draft{}, fmt.Errorf("decoding Gmail draft %q: %w", id, err)
	}
	parsed, err := parseDraft(raw)
	if err != nil {
		return Draft{}, fmt.Errorf("parsing Gmail draft %q: %w", id, err)
	}
	parsed.ID = got.Id
	parsed.MessageID = got.Message.Id
	parsed.ThreadID = got.Message.ThreadId
	return parsed, nil
}

// SaveDraft creates or updates one Gmail draft. A non-empty ID updates that
// mailbox draft; an empty ID creates one. Callers should read before editing so
// external deletion is surfaced rather than silently creating a replacement.
func (c Client) SaveDraft(d Draft, replyToMessageID string) (Draft, error) {
	raw, err := draftMIME(d)
	if err != nil {
		return Draft{}, err
	}
	message := &gmail.Message{Raw: base64.RawURLEncoding.EncodeToString(raw)}
	if replyToMessageID != "" {
		parent, err := c.svc.Users.Messages.Get("me", replyToMessageID).Format("metadata").MetadataHeaders("Message-ID", "References").Do()
		if err != nil {
			return Draft{}, fmt.Errorf("reading reply parent %q: %w", replyToMessageID, err)
		}
		message.ThreadId = parent.ThreadId
		headers := map[string]string{}
		for _, h := range parent.Payload.Headers {
			headers[strings.ToLower(h.Name)] = h.Value
		}
		if id := headers["message-id"]; id != "" {
			raw = addHeader(raw, "In-Reply-To", id)
			refs := strings.TrimSpace(headers["references"] + " " + id)
			raw = addHeader(raw, "References", refs)
			message.Raw = base64.RawURLEncoding.EncodeToString(raw)
		}
	}
	container := &gmail.Draft{Message: message}
	var saved *gmail.Draft
	if d.ID == "" {
		saved, err = c.svc.Users.Drafts.Create("me", container).Do()
	} else {
		if d.MessageID == "" {
			return Draft{}, ErrDraftConflict
		}
		current, getErr := c.svc.Users.Drafts.Get("me", d.ID).Format("raw").Do()
		if getErr != nil {
			return Draft{}, fmt.Errorf("checking Gmail draft before update: %w", getErr)
		}
		if current.Message == nil || current.Message.Id != d.MessageID {
			return Draft{}, ErrDraftConflict
		}
		currentRaw, decodeErr := base64.RawURLEncoding.DecodeString(current.Message.Raw)
		if decodeErr != nil {
			return Draft{}, fmt.Errorf("checking Gmail draft MIME: %w", decodeErr)
		}
		if err := checkDraftReplacement(currentRaw, raw); err != nil {
			return Draft{}, err
		}
		container.Id = d.ID
		saved, err = c.svc.Users.Drafts.Update("me", d.ID, container).Do()
	}
	if err != nil {
		return Draft{}, fmt.Errorf("saving Gmail draft: %w", err)
	}
	if saved == nil || saved.Message == nil {
		return Draft{}, fmt.Errorf("Gmail saved the draft without returning its message")
	}
	d.ID, d.MessageID, d.ThreadID = saved.Id, saved.Message.Id, saved.Message.ThreadId
	return d, nil
}

// DeleteDraft permanently removes a draft from Gmail. Gmail draft deletion is
// distinct from trashing a sent message.
func (c Client) DeleteDraft(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("draft id is required")
	}
	if err := c.svc.Users.Drafts.Delete("me", id).Do(); err != nil {
		return fmt.Errorf("deleting Gmail draft %q: %w", id, err)
	}
	return nil
}

// SendDraft submits the exact draft Gmail currently holds. On transport errors
// the outcome can be unknown, just as with an ordinary send.
func (c Client) SendDraft(id string) (string, string, error) {
	if strings.TrimSpace(id) == "" {
		return "", "", fmt.Errorf("draft id is required")
	}
	sent, err := c.svc.Users.Drafts.Send("me", &gmail.Draft{Id: id}).Do()
	if err != nil {
		return "", "", fmt.Errorf("sending Gmail draft %q: %w", id, err)
	}
	if sent == nil {
		return "", "", fmt.Errorf("Gmail accepted draft %q without returning a message", id)
	}
	return sent.Id, sent.ThreadId, nil
}

// Replacement is intentionally conservative: even a revision-matching draft may
// contain attachments, Bcc, rich text, an alias, or other fields the editor omits.
func checkDraftReplacement(current, replacement []byte) error {
	old, err := mail.ReadMessage(bytes.NewReader(current))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupportedDraft, err)
	}
	next, err := mail.ReadMessage(bytes.NewReader(replacement))
	if err != nil {
		return err
	}
	for name, values := range old.Header {
		switch strings.ToLower(name) {
		case "to", "cc", "subject", "mime-version", "content-type", "content-transfer-encoding":
			if len(values) != 1 {
				return ErrUnsupportedDraft
			}
		default:
			if strings.Join(values, "\n") != strings.Join(next.Header[name], "\n") {
				return ErrUnsupportedDraft
			}
		}
	}
	contentType := old.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/plain" || len(params) > 1 {
		return ErrUnsupportedDraft
	}
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return ErrUnsupportedDraft
	}
	for name := range params {
		if name != "charset" {
			return ErrUnsupportedDraft
		}
	}
	switch strings.ToLower(old.Header.Get("Content-Transfer-Encoding")) {
	case "", "7bit", "8bit", "quoted-printable":
		return nil
	default:
		return ErrUnsupportedDraft
	}
}

func draftMIME(d Draft) ([]byte, error) {
	var b strings.Builder
	for _, field := range []struct{ name, value string }{{"To", d.To}, {"Cc", d.Cc}} {
		if strings.TrimSpace(field.value) != "" {
			addresses, err := mail.ParseAddressList(field.value)
			if err != nil {
				return nil, fmt.Errorf("invalid %s recipients: %w", field.name, err)
			}
			formatted := make([]string, 0, len(addresses))
			for _, address := range addresses {
				formatted = append(formatted, address.String())
			}
			fmt.Fprintf(&b, "%s: %s\r\n", field.name, strings.Join(formatted, ", "))
		}
	}
	fmt.Fprintf(&b, "Subject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", mime.QEncoding.Encode("UTF-8", d.Subject))
	w := quotedprintable.NewWriter(&b)
	if _, err := io.WriteString(w, strings.ReplaceAll(d.Body, "\n", "\r\n")); err != nil {
		return nil, fmt.Errorf("encoding draft body: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finishing draft body: %w", err)
	}
	return []byte(b.String()), nil
}

func parseDraft(raw []byte) (Draft, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return Draft{}, err
	}
	var d Draft
	d.To, d.Cc = msg.Header.Get("To"), msg.Header.Get("Cc")
	d.Subject, err = new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		return Draft{}, fmt.Errorf("decoding subject: %w", err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		return Draft{}, err
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return Draft{}, fmt.Errorf("multipart draft has no boundary")
		}
		mr := multipart.NewReader(msg.Body, boundary)
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return Draft{}, err
			}
			pt, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if pt == "text/plain" {
				var bodyReader io.Reader = part
				if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
					bodyReader = quotedprintable.NewReader(part)
				}
				body, err := io.ReadAll(bodyReader)
				if err != nil {
					return Draft{}, err
				}
				d.Body = string(body)
				break
			}
		}
	} else {
		body, err := io.ReadAll(msg.Body)
		if err != nil {
			return Draft{}, err
		}
		if strings.EqualFold(msg.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
			decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
			if err != nil {
				return Draft{}, err
			}
			body = decoded
		}
		d.Body = string(body)
	}
	d.Body = strings.ReplaceAll(d.Body, "\r\n", "\n")
	return d, nil
}

func addHeader(raw []byte, name, value string) []byte {
	value = strings.Join(strings.Fields(value), " ")
	separator := bytes.Index(raw, []byte("\r\n\r\n"))
	if separator < 0 {
		return raw
	}
	out := make([]byte, 0, len(raw)+len(name)+len(value)+4)
	out = append(out, raw[:separator]...)
	out = append(out, []byte("\r\n"+name+": "+value)...)
	out = append(out, raw[separator:]...)
	return out
}
