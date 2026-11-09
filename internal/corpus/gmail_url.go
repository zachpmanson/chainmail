package corpus

import "net/url"

// GmailMessageURL selects the known mailbox without assuming a browser index.
// Missing account metadata retains the legacy generic link, never a guessed user.
func GmailMessageURL(gmailID, email string) string {
	base := "https://mail.google.com/mail/"
	if email != "" {
		base += "?authuser=" + url.QueryEscape(email)
	}
	return base + "#all/" + url.PathEscape(gmailID)
}
