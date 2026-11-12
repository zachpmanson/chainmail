package corpus

import (
	"io"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// This is positive evidence for one narrow representation difference, not an
// alternate HTML renderer. Unknown own content leaves the normal matcher alone.
const twinPlainMaxBytes = 2 << 20

func plainTwinEvidence(body, rawHTML string, quoted bool) string {
	if len(body) > twinPlainMaxBytes || len(rawHTML) > twinPlainMaxBytes || !utf8.ValidString(body) || !utf8.ValidString(rawHTML) {
		return ""
	}
	own := body
	if !quoted {
		own = ownWords(body)
	} else if rawHTML != "" {
		return ""
	}
	own = plainTwinText(own)
	// Address expansions must not manufacture the meaningful-message floor.
	if len(proseWords(own, "", true)) < minTwinTokens {
		return ""
	}
	if quoted {
		return own
	}
	if rawHTML == "" {
		return ""
	}
	projected, ok := mentionPlainHTML(rawHTML)
	if !ok || plainTwinText(projected) != own {
		return ""
	}
	return own
}

func plainTwinText(text string) string { return strings.Join(strings.Fields(text), " ") }

func plainTwinnable(a, b twinCopy) bool {
	q, m := a, b
	if !a.quoted && b.quoted {
		q, m = b, a
	}
	return q.quoted && !m.quoted && q.bodyHTML == "" && m.bodyHTML != "" &&
		q.container != "" && q.container == m.container && q.plain != "" && q.plain == m.plain
}

// Raw syntax and the parsed tree must agree about own content: tokenizer div
// depth alone cannot establish a history boundary in select/table/foreign HTML.
func mentionPlainHTML(raw string) (string, bool) {
	projected, ok := mentionPlainTokens(raw)
	if !ok {
		return "", false
	}
	parsed, ok := mentionPlainTree(raw)
	if !ok || plainTwinText(parsed) != plainTwinText(projected) {
		return "", false
	}
	return projected, true
}

// Tokenization avoids repairing malformed own markup into acceptance. Only an
// explicit Gmail quote container may be omitted, never an arbitrary blockquote.
func mentionPlainTokens(raw string) (string, bool) {
	z := html.NewTokenizer(strings.NewReader(raw))
	var out, label strings.Builder
	var stack []string
	mentions, history := 0, 0
	address := ""
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return out.String(), z.Err() == io.EOF && len(stack) == 0 && history == 0 && mentions > 0
		}
		rawToken := string(z.Raw())
		token := z.Token()
		if history > 0 {
			if token.Data == "div" {
				if kind == html.StartTagToken {
					history++
				} else if kind == html.EndTagToken {
					history--
				}
			}
			continue
		}
		if (kind == html.StartTagToken || kind == html.SelfClosingTagToken || kind == html.EndTagToken) && !plainTagSyntax(rawToken, token) {
			return "", false
		}
		inAnchor := len(stack) > 0 && stack[len(stack)-1] == "a"
		switch kind {
		case html.TextToken:
			if inAnchor {
				label.WriteString(token.Data)
			} else {
				out.WriteString(token.Data)
				out.WriteByte(' ')
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			if inAnchor {
				return "", false
			}
			if token.Data == "div" && gmailQuoteClass(token.Attr) && kind == html.StartTagToken {
				history = 1
				continue
			}
			switch token.Data {
			case "div", "br":
				if !plainPresentationAttrs(token.Attr, false) {
					return "", false
				}
			case "a":
				if kind != html.StartTagToken || !plainPresentationAttrs(token.Attr, true) {
					return "", false
				}
				var href string
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						href = attr.Val
					}
				}
				address = mentionAddress(href)
				if address == "" {
					return "", false
				}
				label.Reset()
			default:
				return "", false
			}
			out.WriteByte(' ')
			if token.Data != "br" {
				if kind == html.SelfClosingTagToken {
					return "", false
				}
				stack = append(stack, token.Data)
			}
		case html.EndTagToken:
			if len(stack) == 0 || stack[len(stack)-1] != token.Data {
				return "", false
			}
			stack = stack[:len(stack)-1]
			if token.Data == "a" {
				name := plainTwinText(label.String())
				if !strings.HasPrefix(name, "@") || len(name) == 1 || strings.ContainsAny(name, "<>") {
					return "", false
				}
				out.WriteString(name)
				out.WriteString(" <")
				out.WriteString(address)
				out.WriteByte('>')
				mentions++
			}
			out.WriteByte(' ')
		default:
			return "", false
		}
	}
}

// This second projection uses actual HTML subtree boundaries but cannot widen
// the own-fragment grammar or override a raw-syntax refusal.
func mentionPlainTree(raw string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", false
	}
	var out strings.Builder
	mentions := 0
	var visit func(*html.Node) bool
	visit = func(n *html.Node) bool {
		switch n.Type {
		case html.DocumentNode:
		case html.TextNode:
			out.WriteString(n.Data)
			out.WriteByte(' ')
			return true
		case html.ElementNode:
			if n.Namespace != "" {
				return false
			}
			if n.Data == "div" && gmailQuoteClass(n.Attr) {
				return true
			}
			switch n.Data {
			case "html", "body":
				// html.Parse supplies these; explicit envelopes fail the raw gate.
				if len(n.Attr) != 0 {
					return false
				}
			case "head":
				return len(n.Attr) == 0 && n.FirstChild == nil
			case "div", "br":
				if !plainPresentationAttrs(n.Attr, false) {
					return false
				}
			case "a":
				if !plainPresentationAttrs(n.Attr, true) {
					return false
				}
				var address string
				var label strings.Builder
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						address = mentionAddress(attr.Val)
					}
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					if child.Type != html.TextNode {
						return false
					}
					label.WriteString(child.Data)
				}
				name := plainTwinText(label.String())
				if address == "" || !strings.HasPrefix(name, "@") || len(name) == 1 || strings.ContainsAny(name, "<>") {
					return false
				}
				out.WriteByte(' ')
				out.WriteString(name)
				out.WriteString(" <")
				out.WriteString(address)
				out.WriteString("> ")
				mentions++
				return true
			default:
				return false
			}
		default:
			return false
		}
		out.WriteByte(' ')
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if !visit(child) {
				return false
			}
		}
		out.WriteByte(' ')
		return true
	}
	if !visit(doc) || mentions == 0 {
		return "", false
	}
	return out.String(), true
}

func gmailQuoteClass(attrs []html.Attribute) bool {
	seen, quote := false, false
	for _, attr := range attrs {
		if attr.Key != "class" {
			continue
		}
		if attr.Namespace != "" || seen {
			return false
		}
		seen = true
		for _, class := range strings.FieldsFunc(attr.Val, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
		}) {
			if class == "gmail_quote" {
				quote = true
			}
		}
	}
	return quote
}

func plainPresentationAttrs(attrs []html.Attribute, anchor bool) bool {
	seen := map[string]bool{}
	for _, attr := range attrs {
		if attr.Namespace != "" || seen[attr.Key] {
			return false
		}
		seen[attr.Key] = true
		switch attr.Key {
		case "class", "id":
		case "dir":
			if attr.Val != "ltr" && attr.Val != "rtl" && attr.Val != "auto" {
				return false
			}
		case "href":
			if !anchor {
				return false
			}
		case "tabindex":
			if !anchor || (attr.Val != "-1" && attr.Val != "0") {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func mentionAddress(href string) string {
	u, err := url.Parse(href)
	if err != nil || u.Scheme != "mailto" || u.Opaque == "" || u.Host != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(href, "%#\r\n") {
		return ""
	}
	a, err := mail.ParseAddress(u.Opaque)
	if err != nil || a.Name != "" || a.Address != u.Opaque || strings.ContainsAny(u.Opaque, " ,;<>?") {
		return ""
	}
	return a.Address
}

// The HTML tokenizer drops duplicate attributes. Validate the bounded raw tag
// too, accepting only explicit quoted values rather than silently repairing it.
func plainTagSyntax(raw string, token html.Token) bool {
	if len(raw) < 3 || raw[0] != '<' || raw[len(raw)-1] != '>' {
		return false
	}
	inside := strings.TrimSpace(raw[1 : len(raw)-1])
	if token.Type == html.EndTagToken {
		return strings.EqualFold(inside, "/"+token.Data)
	}
	if token.Type == html.SelfClosingTagToken {
		inside = strings.TrimSpace(strings.TrimSuffix(inside, "/"))
	}
	if len(inside) < len(token.Data) || !strings.EqualFold(inside[:len(token.Data)], token.Data) {
		return false
	}
	rest := inside[len(token.Data):]
	seen := map[string]bool{}
	for rest != "" {
		if rest[0] != ' ' && rest[0] != '\t' && rest[0] != '\n' && rest[0] != '\r' && rest[0] != '\f' {
			return false
		}
		rest = strings.TrimLeft(rest, " \t\r\n\f")
		if rest == "" {
			break
		}
		n := 0
		for n < len(rest) && ((rest[n] >= 'a' && rest[n] <= 'z') || (rest[n] >= 'A' && rest[n] <= 'Z') || (rest[n] >= '0' && rest[n] <= '9') || rest[n] == '-' || rest[n] == ':') {
			n++
		}
		if n == 0 {
			return false
		}
		key := strings.ToLower(rest[:n])
		if seen[key] {
			return false
		}
		seen[key] = true
		rest = strings.TrimLeft(rest[n:], " \t\r\n\f")
		if len(rest) < 2 || rest[0] != '=' {
			return false
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\n\f")
		if len(rest) < 2 || (rest[0] != '\'' && rest[0] != '"') {
			return false
		}
		end := strings.IndexByte(rest[1:], rest[0])
		if end < 0 {
			return false
		}
		rest = rest[end+2:]
	}
	return len(seen) == len(token.Attr)
}
