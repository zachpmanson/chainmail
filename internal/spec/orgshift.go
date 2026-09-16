package spec

import (
	"fmt"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// OrgShift is what a proposed set of organisation rules would redraw, counted
// over the whole corpus.
//
// It exists because a rule about a domain is not a rule about that domain's mail
// alone. An organisation is a name a domain's mail is drawn under, and mail that
// has no organisation of its own — a quote-recovered entry, a message from a
// personal address by someone who also mails from work — takes the name its
// sender's own mail established. Rule a domain and those follow it. A screen
// that reported only the domain's own message count would be describing the
// smaller half of the change it was about to make.
type OrgShift struct {
	// Messages is how many entries would be drawn under a different organisation
	// than they are drawn under today. The count the Ops preview puts in front of
	// the write.
	Messages int
	// People is how many distinct senders those entries belong to — the same
	// change, measured in the thing the colour is actually about.
	People int
	// Ambiguous counts the entries left out of both numbers. Their sender's own
	// mail names two organisations and they have no address of their own, so which
	// name colours them depends on the order a particular trail happened to be
	// read in (see orgResolver). Counting them would be inventing an order and
	// reporting its answer as the corpus's; leaving them silently out would make
	// the number above a smaller claim than it reads as.
	Ambiguous int
}

// OrgShiftFor compares the corpus as it is drawn today against the same corpus
// under proposed rules, which is the stored set with one reader's edit applied.
//
// It resolves through OrgForDomain — the same function that resolves a bubble's
// colour — so the number a reader is shown before saving is the number the
// resolver will produce, not a second estimate of it. What it cannot reproduce is
// a single trail's order: a build and a pane resolve a person at whichever of
// their organisations the trail reached first, and "first" is a question about a
// selection. So the person-side answer here is taken only where it is one answer
// — every address the corpus holds for them naming the same organisation — and
// everything else is reported as Ambiguous rather than guessed at.
func OrgShiftFor(store *corpus.Store, proposed map[string]string) (OrgShift, error) {
	stored, err := store.OrgRules()
	if err != nil {
		return OrgShift{}, err
	}
	db := store.DB()

	// Every entry, with the address it was sent from where it has one. A
	// recovered entry has no mail_detail row and a Slack message never will, so
	// this is a left join: their colour comes from their sender, and dropping the
	// rows here would drop exactly the entries the person resolution is for.
	rows, err := db.Query(`
		select coalesce(e.person_id, 0), coalesce(d.from_addr, '')
		from entries e left join mail_detail d on d.entry_id = e.id`)
	if err != nil {
		return OrgShift{}, fmt.Errorf("reading the senders: %w", err)
	}
	defer rows.Close()
	type sent struct {
		person int64
		from   string
	}
	sentBy := make([]sent, 0, 1024)
	for rows.Next() {
		var s sent
		if err := rows.Scan(&s.person, &s.from); err != nil {
			return OrgShift{}, err
		}
		sentBy = append(sentBy, s)
	}
	if err := rows.Err(); err != nil {
		return OrgShift{}, err
	}

	// Every address the corpus holds for a person, which is what orgResolver
	// falls back on: not the addresses in some selection, but the ones the corpus
	// merged into them. A person whose only address is a personal one is still at
	// work once the corpus knows a work address of theirs, and this has to agree.
	addrs, err := loadAddresses(db, nil)
	if err != nil {
		return OrgShift{}, err
	}

	before := orgNamesPerPerson(addrs, stored)
	after := orgNamesPerPerson(addrs, proposed)

	var shift OrgShift
	people := map[int64]bool{}
	for _, s := range sentBy {
		was, wasAmbiguous := orgForEntry(s.person, s.from, stored, before)
		now, nowAmbiguous := orgForEntry(s.person, s.from, proposed, after)
		// An entry the corpus cannot answer for is out of the count either way: it
		// has no answer now, or it would not have one after, and in both cases the
		// entry is one whose colour no corpus-wide pass can predict.
		if wasAmbiguous || nowAmbiguous {
			shift.Ambiguous++
			continue
		}
		if was == now {
			continue
		}
		shift.Messages++
		if s.person != 0 {
			people[s.person] = true
		}
	}
	shift.People = len(people)
	return shift, nil
}

// orgForEntry answers which organisation one entry is drawn under, and whether
// the corpus can answer at all.
//
// The order is the resolver's: this appearance's own address first, because a
// stored rule about the address the mail actually came from is a decision about
// this message; then the sender, for an appearance whose own address decided
// nothing.
func orgForEntry(person int64, from string, rules map[string]string, people map[int64]personOrg) (string, bool) {
	if org, decided := OrgForDomain(corpus.MailDomain(from), rules); decided {
		return org, false
	}
	if person == 0 {
		return "", false
	}
	p, ok := people[person]
	if !ok {
		return "", false
	}
	return p.org, p.ambiguous
}

// personOrg is one person's organisation as a corpus-wide pass can honestly
// state it: a name, or the fact that there is no one name.
type personOrg struct {
	org       string
	ambiguous bool
}

// orgNamesPerPerson resolves every address the corpus holds into the name it is
// drawn under, per person.
//
// A person whose addresses name one organisation has an answer and the resolver
// will reach it whichever trail it is reading. A person whose addresses name two
// has no corpus-wide answer at all — a trail that opens with their Threadlet mail
// draws them Threadlet, and one that opens with their Termina mail draws them
// Termina — so that is reported as ambiguous rather than resolved by an ordering
// this pass invented. A rule that names no organisation is not a name, so it
// contributes nothing here, exactly as it does not stop the resolver's search.
func orgNamesPerPerson(addrs map[int64][]string, rules map[string]string) map[int64]personOrg {
	out := make(map[int64]personOrg, len(addrs))
	for person, values := range addrs {
		var org string
		ambiguous := false
		for _, a := range values {
			name, decided := OrgForDomain(corpus.MailDomain(a), rules)
			if !decided || name == "" {
				continue
			}
			if org == "" {
				org = name
				continue
			}
			if org != name {
				ambiguous = true
				break
			}
		}
		out[person] = personOrg{org: org, ambiguous: ambiguous}
	}
	return out
}
