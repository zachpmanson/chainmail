package corpus

import (
	"strings"

	"golang.org/x/net/html"
)

// twinHTMLText preserves the boundaries between elements in a sender's HTML
// part. The parallel plain-text rendition can concatenate adjacent paragraphs,
// which merges distinct words and prevents an otherwise identical quote from
// matching its mailbox message.
func twinHTMLText(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	var out strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
			out.WriteByte(' ')
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if n.Type == html.ElementNode {
			out.WriteByte(' ')
		}
	}
	visit(doc)
	return out.String()
}
