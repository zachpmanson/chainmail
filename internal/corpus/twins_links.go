package corpus

import (
	"regexp"

	"github.com/zachpmanson/chainmail/internal/textsim"
)

// quoteLinks keeps destinations verbatim and their positions among prose tokens.
// It is comparison evidence only; the stored/rendered bodies are never rewritten.
type quoteLinks struct {
	shape []string
	urls  []string
	words int
}

var reQuoteLink = regexp.MustCompile(`(?i)https?://[^\s<>]+`)

func copyLinks(body, bodyHTML string, quoted bool) quoteLinks {
	if quoted {
		body = stripQuotedClientPreamble(body)
		body = reTwinImagePlaceholder.ReplaceAllStringFunc(body, func(s string) string {
			if reQuoteLink.MatchString(s) {
				return s // a visible URL, not an attachment placeholder
			}
			return " "
		})
	} else {
		body = ownWords(twinHTMLTextOrBody(body, bodyHTML))
	}
	locations := reQuoteLink.FindAllStringIndex(body, -1)
	if len(locations) == 0 {
		return quoteLinks{}
	}
	var out quoteLinks
	appendProse := func(s string) {
		words := textsim.Tokens(s)
		out.shape = append(out.shape, words...)
		out.words += len(words)
	}
	end := 0
	for _, loc := range locations {
		appendProse(body[end:loc[0]])
		// Angle brackets delimit a URL; do not fold case or strip punctuation
		// from paths/queries, where those bytes can change the destination.
		out.urls = append(out.urls, body[loc[0]:loc[1]])
		out.shape = append(out.shape, "\x00") // not a textsim token
		end = loc[1]
	}
	appendProse(body[end:])
	return out
}

// replacedQuoteLinks is deliberately stricter than approximate twin matching:
// every non-URL token and every URL position must agree. Added/removed links,
// footers and URL-only bodies are not evidence of a replacement.
func replacedQuoteLinks(a, b twinCopy) bool {
	x, y := a.links, b.links
	if x.words < twinHeadRun || len(x.urls) == 0 || len(x.urls) != len(y.urls) ||
		!wordSlicesEqual(x.shape, y.shape) {
		return false
	}
	return !wordSlicesEqual(x.urls, y.urls)
}

func vouchedQuoteLinks(a, b twinCopy) bool {
	if !replacedQuoteLinks(a, b) {
		return false
	}
	if a.links.words >= minTwinTokens {
		return true
	}
	// Short prose still needs an authoritative mailbox/thread witness. Raw URL
	// length must not manufacture enough words to bypass the short-message gate.
	return sameSubjectWithMailbox(a, b) ||
		(a.quoted != b.quoted && a.container != "" && a.container == b.container)
}
