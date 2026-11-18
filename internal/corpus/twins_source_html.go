package corpus

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Exact representations of the surviving mailbox's own content, never of a
// requote. MIME wins when both match, retaining the original annotation veto.
type sourceTwinBody struct {
	mime, html, block string
}

func (b sourceTwinBody) matches(text string) bool {
	text = plainTwinText(text)
	return text != "" && (text == b.mime || text == b.html || text == b.block)
}

func (b sourceTwinBody) rendered(text string) bool {
	text = plainTwinText(text)
	return text != b.mime && b.matches(text)
}

func sourceTwinRepresentations(body, rawHTML string) sourceTwinBody {
	b := sourceTwinBody{mime: plainTwinText(ownWords(body))}
	if rawHTML == "" || !utf8.ValidString(rawHTML) {
		return b
	}
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return b
	}
	b.html = plainTwinText(ownWords(twinHTMLNodeText(doc)))
	if block, ok := sourceTwinBlockText(doc); ok {
		b.block = plainTwinText(ownWords(block))
	}
	return b
}

// Some requoters preserve block boundaries and original text whitespace, but
// insert nothing between adjacent table cells. Only this known table shape is
// an alternative; no text, URL, image placeholder or address is stripped.
func sourceTwinBlockText(doc *html.Node) (string, bool) {
	var out strings.Builder
	cells := false
	var visit func(*html.Node) bool
	visit = func(n *html.Node) bool {
		boundary := false
		switch n.Type {
		case html.DocumentNode:
		case html.TextNode:
			out.WriteString(n.Data)
			return true
		case html.CommentNode:
			return true
		case html.ElementNode:
			if n.Namespace != "" {
				return false
			}
			switch n.Data {
			case "script", "style":
				return true
			case "html", "head", "body", "div", "p", "br", "table", "thead", "tbody", "tfoot", "tr", "blockquote":
				boundary = true
			case "colgroup":
				// Column sizing is textless; repaired content must still be visited.
				if n.Parent == nil || n.Parent.Data != "table" {
					return false
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == html.TextNode && strings.TrimSpace(child.Data) == "" {
						continue
					}
					if child.Type != html.ElementNode || child.Namespace != "" || child.Data != "col" {
						return false
					}
				}
			case "col":
				if n.Parent == nil || n.Parent.Data != "colgroup" || n.FirstChild != nil {
					return false
				}
			case "td", "th":
				cells = true
			case "a", "span", "b", "strong", "i", "em", "u", "s":
			default:
				return false
			}
		default:
			return false
		}
		if boundary {
			out.WriteByte('\n')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if !visit(child) {
				return false
			}
		}
		if boundary {
			out.WriteByte('\n')
		}
		return true
	}
	ok := visit(doc)
	return out.String(), ok && cells
}
