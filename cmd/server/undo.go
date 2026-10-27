package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// Receipts live only in this process. They bind a reversal to the exact mailbox
// copies changed, not a chain that may acquire new messages before undo.
type mailUndo struct {
	AccountID string
	Read      bool
	Messages  []undoMessage
}
type undoMessage struct {
	ID                     string
	Before, After, Touched []string
	Version                uint64
	Previous               map[string]uint64
}

type mailLabelKey struct{ Account, ID, Label string }

func (s *server) bumpMailVersion(account, id string, touched []string) (uint64, map[string]uint64) {
	if s.mailVersions == nil {
		s.mailVersions = map[mailLabelKey]uint64{}
	}
	s.mailRevision++
	previous := map[string]uint64{}
	for _, label := range touched {
		if _, seen := previous[label]; seen {
			continue
		}
		key := mailLabelKey{account, id, label}
		previous[label] = s.mailVersions[key]
		s.mailVersions[key] = s.mailRevision
	}
	return s.mailRevision, previous
}

func (s *server) pruneMailVersions() {
	keep := map[mailLabelKey]bool{}
	for _, receipt := range s.mailUndos {
		for _, message := range receipt.Messages {
			for _, label := range message.Touched {
				keep[mailLabelKey{receipt.AccountID, message.ID, label}] = true
			}
		}
	}
	for key := range s.mailVersions {
		if !keep[key] {
			delete(s.mailVersions, key)
		}
	}
}

func (s *server) saveUndo(receipt *mailUndo) string {
	defer s.pruneMailVersions()
	if len(receipt.Messages) == 0 {
		return ""
	}
	if s.mailUndos == nil {
		s.mailUndos = map[string]*mailUndo{}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(random[:])
	s.mailUndos[token] = receipt
	s.mailUndoOrder = append(s.mailUndoOrder, token)
	if len(s.mailUndoOrder) > 100 {
		delete(s.mailUndos, s.mailUndoOrder[0])
		s.mailUndoOrder = s.mailUndoOrder[1:]
	}
	return token
}

// Only capture the bits this action can change. Other clients' unrelated labels
// must survive undo, and the corpus mirror may be older than the mailbox.
func captureUndo(mb mailbox, trails [][]corpus.ChainEntry, account string, touched []string) (map[string][]string, error) {
	before := map[string][]string{}
	for _, entries := range trails {
		for _, e := range entries {
			copy, ok := copyForAccount(e, account)
			if !ok {
				continue
			}
			if _, seen := before[copy.GmailID]; seen {
				continue
			}
			message, err := mb.Read(copy.GmailID)
			if err != nil {
				return nil, fmt.Errorf("reading labels for undo before changing anything: %w", err)
			}
			before[copy.GmailID] = labelSubset(message.Labels, touched)
		}
	}
	return before, nil
}

func labelSubset(labels, touched []string) []string {
	var out []string
	for _, label := range touched {
		if slices.Contains(labels, label) && !slices.Contains(out, label) {
			out = append(out, label)
		}
	}
	slices.Sort(out)
	return out
}

func (s *server) recordUndo(u *mailUndo, id string, before, after []string, touched []string) {
	after = labelSubset(after, touched)
	if slices.Equal(before, after) {
		s.bumpMailVersion(u.AccountID, id, touched)
		return
	}
	for _, m := range u.Messages {
		if m.ID == id {
			return
		}
	}
	version, previous := s.bumpMailVersion(u.AccountID, id, touched)
	u.Messages = append(u.Messages, undoMessage{ID: id, Before: before, After: after, Touched: slices.Clone(touched), Version: version, Previous: previous})
}

func (s *server) undoFailure(w http.ResponseWriter, status int, err error, receipt *mailUndo) {
	send(w, status, struct {
		Error     string `json:"error"`
		UndoToken string `json:"undoToken,omitempty"`
	}{err.Error(), s.saveUndo(receipt)})
}

func (s *server) undoMail(w http.ResponseWriter, r *http.Request) {
	s.mailMu.Lock()
	defer s.mailMu.Unlock()
	var req struct {
		Token string `json:"token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	receipt := s.mailUndos[req.Token]
	if receipt == nil {
		fail(w, 409, errors.New("undo expired; server restarts do not preserve undo"))
		return
	}
	if len(receipt.Messages) == 0 {
		send(w, 200, struct {
			Undone bool `json:"undone"`
		}{true})
		return
	}
	if (receipt.Read && !s.markReadEnabled) || (!receipt.Read && !s.mailWriteEnabled) {
		fail(w, 403, errors.New("this host no longer permits the mailbox write needed by undo"))
		return
	}
	mb, err := s.mailboxForAccount(receipt.AccountID)
	if err != nil {
		fail(w, 502, err)
		return
	}
	count := len(receipt.Messages)
	defer func() {
		log.Printf("undo: account=%s restored=%d remaining=%d", receipt.AccountID, count-len(receipt.Messages), len(receipt.Messages))
	}()
	// Preflight every message before any reversal. A touched bit changed elsewhere
	// is a conflict; never overwrite it silently. The Before state is accepted for
	// an idempotent retry after an ambiguous network/store failure.
	for _, m := range receipt.Messages {
		message, err := mb.Read(m.ID)
		if err != nil {
			fail(w, 502, err)
			return
		}
		for _, label := range m.Touched {
			if s.mailVersions[mailLabelKey{receipt.AccountID, m.ID, label}] != m.Version {
				fail(w, 409, errors.New("a newer mailbox action overlaps this undo; undo newer actions first (unrecorded writes cannot be reversed)"))
				return
			}
		}
		now := labelSubset(message.Labels, m.Touched)
		if !slices.Equal(now, m.After) && !slices.Equal(now, m.Before) {
			fail(w, 409, errors.New("mail changed elsewhere; undo refused without changing anything"))
			return
		}
	}
	for len(receipt.Messages) > 0 {
		m := receipt.Messages[len(receipt.Messages)-1]
		remove := []string{}
		for _, label := range m.After {
			if !slices.Contains(m.Before, label) {
				remove = append(remove, label)
			}
		}
		labels, err := mb.SetLabels(m.ID, m.Before, remove)
		if err != nil {
			fail(w, 502, fmt.Errorf("undo incomplete; remaining messages kept for retry: %w", err))
			return
		}
		if err := s.store.SetGmailCopyLabels(receipt.AccountID, m.ID, labels); err != nil {
			fail(w, 500, fmt.Errorf("mailbox restored but mirror update failed; retry undo: %w", err))
			return
		}
		for label, previous := range m.Previous {
			s.mailVersions[mailLabelKey{receipt.AccountID, m.ID, label}] = previous
		}
		receipt.Messages = receipt.Messages[:len(receipt.Messages)-1]
	}
	s.pruneMailVersions()
	send(w, 200, struct {
		Undone bool `json:"undone"`
	}{true})
}
