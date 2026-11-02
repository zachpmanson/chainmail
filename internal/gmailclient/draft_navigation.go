package gmailclient

import (
	"net/mail"
	"strings"
)

// ReplyHeaders retains unfolded values and occurrence counts, including empty
// fields. It is evidence only, not a claim of MIME editability or sender safety.
type ReplyHeaders struct {
	InReplyTo  []string
	References []string
}

func replyHeaders(h mail.Header) ReplyHeaders {
	return ReplyHeaders{InReplyTo: append([]string(nil), h["In-Reply-To"]...), References: append([]string(nil), h["References"]...)}
}

// Parent accepts only bracketed, unquoted dot-atom IDs, without comments or
// obsolete syntax. Optional References must be one nonempty whitespace-separated
// list of the same syntax; it never supplies or overrides the explicit parent.
// All input is consumed, and identifier case/spelling/brackets are preserved.
func (h ReplyHeaders) Parent() (parent, resolution string) {
	if len(h.InReplyTo) == 0 {
		if len(h.References) == 0 {
			return "", "compose"
		}
		return "", "missing-explicit-parent"
	}
	if len(h.InReplyTo) != 1 {
		return "", "malformed-parent"
	}
	parent = strings.Trim(h.InReplyTo[0], " \t")
	if !plainDraftMessageID(parent) {
		return "", "malformed-parent"
	}
	if len(h.References) > 1 {
		return "", "malformed-parent"
	}
	if len(h.References) == 1 {
		refs := strings.FieldsFunc(h.References[0], func(r rune) bool { return r == ' ' || r == '\t' })
		if len(refs) == 0 {
			return "", "malformed-parent"
		}
		for _, ref := range refs {
			if !plainDraftMessageID(ref) {
				return "", "malformed-parent"
			}
		}
	}
	return parent, "candidate"
}
