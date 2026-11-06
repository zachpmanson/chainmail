package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"google.golang.org/api/googleapi"
)

const (
	maxDraftMIMEBytes  = 2 << 20
	maxDraftMIMEDepth  = 12
	maxDraftMIMEParts  = 100
	maxDraftHTMLBytes  = 256 << 10
	maxDraftHTMLTokens = 20000
)

var (
	ErrDraftMIMEMalformed = errors.New("draft MIME check failed [reason=malformed_message]")
	ErrDraftMIMELimit     = errors.New("draft MIME check failed [reason=limit_exceeded]")
	ErrDraftMIMEEncoding  = errors.New("draft MIME check failed [reason=unsupported_html_encoding]")
)

// DraftMIMEPart contains only fixed categories, never provider strings or content.
// It is diagnostic evidence, not a save/send compatibility decision.
type DraftMIMEPart struct {
	MIMEClass        string             `json:"mimeClass"`
	Charset          string             `json:"charset"`
	TransferEncoding string             `json:"transferEncoding"`
	Disposition      string             `json:"disposition"`
	HTMLFeatures     *DraftHTMLFeatures `json:"htmlFeatures,omitempty"`
	Parts            []DraftMIMEPart    `json:"parts"`
}

type DraftHTMLFeatures struct {
	HasLinks           bool `json:"hasLinks"`
	HasImages          bool `json:"hasImages"`
	HasFormatting      bool `json:"hasFormatting"`
	HasQuotes          bool `json:"hasQuotes"`
	HasSignature       bool `json:"hasSignature"`
	HasUnknownElements bool `json:"hasUnknownElements"`
}

// CheckDraftMIME reads only the selected raw draft. It never reads identities,
// parent messages or settings, and never constructs a replacement draft.
func (c Client) CheckDraftMIME(id string) (*DraftMIMEPart, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrDraftMIMEMalformed
	}
	got, err := c.svc.Users.Drafts.Get("me", id).Format("raw").Fields("message/raw").Context(c.ctx).Do()
	if err != nil {
		if IsNotFound(err) {
			return nil, &googleapi.Error{Code: 404, Message: "Gmail draft no longer exists"}
		}
		return nil, errors.New("Gmail could not read the draft for MIME checking")
	}
	if got == nil || got.Message == nil || got.Message.Raw == "" {
		return nil, ErrDraftMIMEMalformed
	}
	// Check before allocating the decoded raw message (padded or unpadded URL base64).
	if len(got.Message.Raw) > base64.URLEncoding.EncodedLen(maxDraftMIMEBytes) {
		return nil, ErrDraftMIMELimit
	}
	raw, err := decodeDraftRaw(got.Message.Raw)
	if err != nil {
		return nil, ErrDraftMIMEMalformed
	}
	return inspectDraftMIME(raw)
}

func inspectDraftMIME(raw []byte) (*DraftMIMEPart, error) {
	if len(raw) > maxDraftMIMEBytes {
		return nil, ErrDraftMIMELimit
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrDraftMIMEMalformed
	}
	count := 0
	part, err := inspectDraftMIMEPart(textproto.MIMEHeader(msg.Header), msg.Body, 1, &count)
	if err != nil {
		return nil, err // Never return a partial tree.
	}
	return &part, nil
}

func inspectDraftMIMEPart(h textproto.MIMEHeader, body io.Reader, depth int, count *int) (DraftMIMEPart, error) {
	out := DraftMIMEPart{MIMEClass: "other", Charset: "none", TransferEncoding: "none", Disposition: "none", Parts: []DraftMIMEPart{}}
	*count++
	if depth > maxDraftMIMEDepth || *count > maxDraftMIMEParts {
		return out, ErrDraftMIMELimit
	}
	for _, key := range []string{"Content-Type", "Content-Transfer-Encoding", "Content-Disposition"} {
		if len(h.Values(key)) > 1 {
			return out, ErrDraftMIMEMalformed
		}
	}
	mediaType, params := "text/plain", map[string]string{}
	if len(h.Values("Content-Type")) != 0 {
		var err error
		mediaType, params, err = mime.ParseMediaType(h.Get("Content-Type"))
		if err != nil || !strings.Contains(mediaType, "/") {
			return out, ErrDraftMIMEMalformed
		}
	}
	switch mediaType {
	case "text/plain", "text/html", "multipart/alternative", "multipart/mixed", "multipart/related", "multipart/signed", "multipart/encrypted":
		out.MIMEClass = mediaType
	}
	if charset, ok := params["charset"]; ok {
		out.Charset = "other"
		switch strings.ToLower(charset) {
		case "utf-8", "us-ascii", "iso-8859-1", "windows-1252":
			out.Charset = strings.ToLower(charset)
		}
	}
	if len(h.Values("Content-Transfer-Encoding")) != 0 {
		out.TransferEncoding = "other"
		switch encoding := strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))); encoding {
		case "7bit", "8bit", "binary", "base64", "quoted-printable":
			out.TransferEncoding = encoding
		}
	}
	if len(h.Values("Content-Disposition")) != 0 {
		disposition, _, err := mime.ParseMediaType(h.Get("Content-Disposition"))
		if err != nil {
			return out, ErrDraftMIMEMalformed
		}
		out.Disposition = "other"
		switch disposition {
		case "inline", "attachment":
			out.Disposition = disposition
		}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		if params["boundary"] == "" || out.TransferEncoding == "base64" || out.TransferEncoding == "quoted-printable" || out.TransferEncoding == "other" {
			return out, ErrDraftMIMEMalformed
		}
		reader := multipart.NewReader(body, params["boundary"])
		for {
			// NextPart silently decodes quoted-printable and removes its header.
			part, err := reader.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return out, ErrDraftMIMEMalformed
			}
			child, err := inspectDraftMIMEPart(part.Header, part, depth+1, count)
			if err != nil {
				return out, err
			}
			out.Parts = append(out.Parts, child)
		}
		if len(out.Parts) == 0 {
			return out, ErrDraftMIMEMalformed
		}
	} else if mediaType == "text/html" {
		features, err := inspectDraftHTML(body, out.Charset, out.TransferEncoding)
		if err != nil {
			return out, err
		}
		out.HTMLFeatures = features
	} else {
		// Consume leaves so malformed multipart framing is not hidden by a short read.
		if _, err := io.Copy(io.Discard, body); err != nil {
			return out, ErrDraftMIMEMalformed
		}
	}
	return out, nil
}

func mimeHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func inspectDraftHTML(body io.Reader, charset, encoding string) (*DraftHTMLFeatures, error) {
	if charset == "other" || encoding == "other" {
		return nil, ErrDraftMIMEEncoding
	}
	encoded, err := io.ReadAll(io.LimitReader(body, maxDraftMIMEBytes+1))
	if err != nil {
		return nil, ErrDraftMIMEMalformed
	}
	if len(encoded) > maxDraftMIMEBytes {
		return nil, ErrDraftMIMELimit
	}
	body = bytes.NewReader(encoded)
	switch encoding {
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		// The standard decoder tolerates malformed escapes as literal content.
		for i := 0; i < len(encoded); i++ {
			if encoded[i] != '=' {
				continue
			}
			if i+2 >= len(encoded) {
				return nil, ErrDraftMIMEMalformed
			}
			if !(encoded[i+1] == '\r' && encoded[i+2] == '\n') && !(mimeHex(encoded[i+1]) && mimeHex(encoded[i+2])) {
				return nil, ErrDraftMIMEMalformed
			}
			i += 2
		}
		body = quotedprintable.NewReader(body)
	}
	decoded, err := io.ReadAll(io.LimitReader(body, maxDraftHTMLBytes+1))
	if err != nil {
		return nil, ErrDraftMIMEMalformed
	}
	if len(decoded) > maxDraftHTMLBytes {
		return nil, ErrDraftMIMELimit
	}
	if (charset == "none" || charset == "utf-8") && !utf8.Valid(decoded) {
		return nil, ErrDraftMIMEMalformed
	}
	for _, b := range decoded {
		if b == 0 || (charset == "us-ascii" && b > 127) {
			return nil, ErrDraftMIMEMalformed
		}
	}
	features := &DraftHTMLFeatures{}
	tokens := html.NewTokenizer(bytes.NewReader(decoded))
	for n := 0; ; n++ {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			if tokens.Err() != io.EOF {
				return nil, ErrDraftMIMEMalformed
			}
			return features, nil
		}
		if n >= maxDraftHTMLTokens {
			return nil, ErrDraftMIMELimit
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokens.Token()
		switch token.Data {
		case "a":
			features.HasLinks = true
		case "img":
			features.HasImages = true
		case "blockquote":
			features.HasQuotes = true
		case "b", "strong", "i", "em", "u", "s", "strike", "font", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "pre", "code", "hr", "sub", "sup", "del", "ins":
			features.HasFormatting = true
		case "html", "head", "body", "meta", "title", "div", "span", "p", "br", "wbr":
		default:
			features.HasUnknownElements = true
		}
		for _, attr := range token.Attr {
			switch attr.Key {
			case "style", "align", "color", "face", "size", "bgcolor":
				features.HasFormatting = true
			case "class":
				for _, class := range strings.Fields(attr.Val) {
					switch class {
					case "gmail_quote", "gmail_attr":
						features.HasQuotes = true
					case "gmail_signature":
						features.HasSignature = true
					}
				}
			}
		}
	}
}
