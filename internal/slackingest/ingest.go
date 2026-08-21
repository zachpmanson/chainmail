package slackingest

import (
	"fmt"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Result summarises one ingest run.
type Result struct {
	Channels int
	Seen     int
	Created  int
	Changed  int // existed with a different body: an edit slackdump has since seen
	Skipped  int // already present, byte for byte
	Resolved int64

	// Users counts the accounts the archive knows about, WithoutEmail those
	// whose profile carries no address. The second number is the ceiling on how
	// many people cannot be tied to a mail identity, and it is reported rather
	// than logged at debug because it is the number that decides whether the
	// Slack and mail halves of the corpus are one graph or two.
	Users             int
	UsersWithoutEmail int
	Bots              int

	// Authors and AuthorsWithoutEmail are the same counts restricted to accounts
	// that actually said something, which is the population that affects the
	// corpus at all.
	Authors             int
	AuthorsWithoutEmail int

	// Unauthored counts messages with no identifiable author of any kind. Their
	// bodies are still evidence, so they are stored anyway.
	Unauthored int

	// IdentityConflicts counts Slack accounts whose uid already belonged to a
	// different person than their email resolves to — the shape of a uid-only
	// person from an earlier run whose email is now known. Left for `corpus
	// candidates` and a human merge rather than merged here: an automatic merge
	// on this evidence would be unreviewable if it were wrong.
	IdentityConflicts int
}

// Ingest converts every message in a slackdump archive into the corpus.
//
// Idempotent by construction: nothing is fetched, everything is keyed on
// (channel, ts), and a message already present with the same body is skipped
// without a write. Re-running over a grown archive costs one query per message
// that is new.
func Ingest(store *corpus.Store, a *Archive) (Result, error) {
	var r Result

	workspace, err := a.Workspace()
	if err != nil {
		return r, err
	}
	users, err := a.Users()
	if err != nil {
		return r, err
	}
	channels, err := a.Channels()
	if err != nil {
		return r, err
	}
	r.Users = len(users)
	r.Channels = len(channels)
	for _, u := range users {
		if u.Email == "" {
			r.UsersWithoutEmail++
		}
		if u.IsBot {
			r.Bots++
		}
	}

	in := &ingester{
		store:     store,
		workspace: workspace,
		users:     users,
		channels:  channels,
		people:    map[string]int64{},
	}
	in.names = Names{
		User: func(id string) string {
			if u, ok := in.users[id]; ok {
				return u.DisplayName()
			}
			return ""
		},
		Channel: func(id string) string {
			return in.channels[id].Name
		},
	}

	if err := a.Messages(func(m Message) error {
		return in.put(m, &r)
	}); err != nil {
		return r, err
	}

	// Thread edges are resolved after the walk, not during it: a reply can be
	// archived in a chunk that precedes its parent's, so a link attempted inline
	// would miss and never be retried.
	n, err := store.ResolveSlackParents()
	if err != nil {
		return r, err
	}
	r.Resolved = n
	return r, nil
}

type ingester struct {
	store     *corpus.Store
	workspace string
	users     map[string]User
	channels  map[string]Channel
	names     Names

	// people caches uid -> person for the run. Resolution is otherwise two
	// queries per message, and a busy channel is one author over and over.
	people map[string]int64
}

func (in *ingester) put(m Message, r *Result) error {
	if m.TS == "" || m.ChannelID == "" {
		return fmt.Errorf("message in %q has no ts", m.ChannelID)
	}
	ts, err := m.Time()
	if err != nil {
		return err
	}
	r.Seen++

	person, author, err := in.author(m, r)
	if err != nil {
		return err
	}
	if person == 0 {
		r.Unauthored++
	}
	tz, offset := author.Zone(ts)
	ch := in.channels[m.ChannelID]

	e := corpus.Entry{
		Source:   corpus.SourceSlack,
		ExtID:    ExtID(m.ChannelID, m.TS),
		Kind:     "message",
		TS:       ts,
		TZ:       tz,
		TZOffset: offset,
		PersonID: person,
		// The channel is the container, as a mail thread is for mail: it is what
		// groups messages that are not linked by a reply edge.
		Container: m.ChannelID,
		ParentRef: m.ParentRef(),
		// No subject. Slack has none, and synthesising one from the channel name
		// would put the same words in the FTS subject column — weighted four times
		// the body — on every message in the channel.
		BodyText:  PlainText(m.Text, in.names),
		BodyHTML:  "",
		Permalink: Permalink(in.workspace, m.ChannelID, m.TS),
	}

	d := corpus.Slack{
		ChannelID:   m.ChannelID,
		ChannelName: ch.Name,
		TS:          m.TS,
		ThreadTS:    m.ThreadTS,
		ReplyCount:  m.ReplyCount,
		Subtype:     m.Subtype,
		IsBot:       author.IsBot || m.BotID != "",
		// The id prefix is authoritative even when the archive holds no CHANNEL
		// row for the conversation, which is the case for a messages-only dump.
		// is_im from the channel record confirms it; a group DM (mpim, "G"-prefixed)
		// is deliberately NOT a DM here — it is a small private channel, and folding
		// the two together would make "just between us" unanswerable.
		IsDM: strings.HasPrefix(m.ChannelID, "D") || ch.IsIM,
	}

	var atts []corpus.Attachment
	for _, f := range m.Files {
		name := firstNonEmpty(f.Name, f.Title, f.ID)
		if name == "" {
			continue
		}
		atts = append(atts, corpus.Attachment{
			Name: name, Mime: f.Mimetype, Size: f.Size,
			Permalink: f.Permalink, SourceRef: f.ID,
		})
	}

	res, err := in.store.PutSlack(e, d, atts)
	if err != nil {
		return fmt.Errorf("storing %s: %w", e.ExtID, err)
	}
	switch {
	case res.Created:
		r.Created++
	case res.Changed:
		r.Changed++
	default:
		r.Skipped++
		// Nothing else to record: participation and the sighting were written when
		// the entry was, and neither can change for a message that has not.
		return nil
	}

	if err := in.store.Sight(res.ID, 0, "direct", ""); err != nil {
		return err
	}
	// The author, and only the author. Channel membership is not participation:
	// a post in a 71-member channel is not addressed to 71 people, and recording
	// it as such would make "who was this sent to" meaningless for mail too,
	// where the answer is a real, deliberate list.
	if person != 0 {
		return corpus.Participate(in.store, res.ID, person, corpus.RoleFrom)
	}
	return nil
}

// ExtID is the corpus identity of a Slack message. The channel is part of it
// because a ts is only unique within a channel.
func ExtID(channelID, ts string) string {
	return "slack:" + channelID + ":" + ts
}

// author finds or creates the person who wrote a message, returning 0 when
// nothing identifies them.
func (in *ingester) author(m Message, r *Result) (int64, User, error) {
	switch {
	case m.User != "":
		u, known := in.users[m.User]
		if !known {
			// A message from an account the archive never recorded — someone
			// deactivated before the users dump, or a channel from another
			// workspace. The uid is still a durable identity.
			u = User{ID: m.User}
		}
		id, err := in.person(u, r)
		return id, u, err

	case m.BotID != "":
		// A bot post carries no user, only a bot id and the name it posted under.
		// The bot id is a real Slack id, so it keys the same way a uid does.
		u := User{ID: m.BotID, RealName: m.Username, IsBot: true}
		id, err := in.person(u, r)
		return id, u, err

	case m.Username != "":
		// Neither a uid nor a bot id: an integration that only ever gave a name.
		// A display-name identity is the honest key — it is exactly the placeholder
		// people.go describes, and a human can merge it later.
		id, err := corpus.ResolveWithRule(in.store, corpus.KindDisplayName,
			m.Username, m.Username, "slack:username")
		if err != nil {
			return 0, User{}, nil
		}
		return id, User{RealName: m.Username}, nil
	}
	return 0, User{}, nil
}

// person resolves one Slack account to a corpus person, preferring the profile
// email.
//
// The email is the whole point of this package: it is the only field that ties a
// Slack account to the same human's mail, and resolving through it is what makes
// one person out of two halves of a conversation. Where there is no email the
// account is keyed on its uid instead — never on a synthesised address, which
// would collide with a real one the moment anybody registered it.
func (in *ingester) person(u User, r *Result) (int64, error) {
	if id, ok := in.people[u.ID]; ok {
		return id, nil
	}
	name := u.DisplayName()

	if u.Email == "" {
		r.Authors++
		r.AuthorsWithoutEmail++
		id, err := corpus.ResolveWithRule(in.store, corpus.KindSlackUID, u.ID, name,
			"slack:uid-no-email")
		if err != nil {
			return 0, err
		}
		in.people[u.ID] = id
		return id, nil
	}

	r.Authors++
	id, err := corpus.ResolveAddress(in.store,
		corpus.Address{Addr: u.Email, Name: name}, "slack:profile-email")
	if err != nil {
		return 0, err
	}
	// Attach the uid to whoever the email resolved to, so the next appearance of
	// this account is one lookup and so a search by uid finds their mail too. A
	// uid already held by someone else is a conflict a human should settle.
	if err := corpus.AddAlias(in.store, id, corpus.KindSlackUID, u.ID,
		"slack:profile-email"); err != nil {
		r.IdentityConflicts++
	}
	in.people[u.ID] = id
	return id, nil
}
