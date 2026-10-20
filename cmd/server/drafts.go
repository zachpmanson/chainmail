package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zachpmanson/chainmail/internal/gmailclient"
)

type draftMailbox interface {
	mailbox
	ListDrafts() ([]gmailclient.Draft, error)
	GetDraft(id string) (gmailclient.Draft, error)
	SaveDraft(gmailclient.Draft, string) (gmailclient.Draft, error)
	DeleteDraft(id string) error
	SendDraft(id string) (messageID, threadID string, err error)
}

type draftRequest struct {
	AccountID         string   `json:"accountId,omitempty"`
	ID                string   `json:"id,omitempty"`
	ExpectedMessageID string   `json:"expectedMessageId,omitempty"`
	To                []string `json:"to"`
	Cc                []string `json:"cc,omitempty"`
	Subject           string   `json:"subject"`
	Body              string   `json:"body"`
	ReplyToEntry      string   `json:"replyToEntry,omitempty"`
	ReplyAll          bool     `json:"replyAll,omitempty"`
}

type draftResponse struct {
	AccountID string `json:"accountId"`
	gmailclient.Draft
}

// drafts serves the mailbox's current draft list or creates a Gmail draft.
func (s *server) drafts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		accountID, err := s.connectedAccount(r.URL.Query().Get("accountId"))
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		mb, err := s.draftMailbox(accountID)
		if err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
			return
		}
		drafts, err := mb.ListDrafts()
		if err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("listing drafts: %w", err))
			return
		}
		out := make([]draftResponse, 0, len(drafts))
		for _, d := range drafts {
			out = append(out, draftResponse{AccountID: accountID, Draft: d})
		}
		send(w, http.StatusOK, out)
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, errors.New("method must be GET or POST"))
		return
	}
	if !s.draftMailEnabled {
		fail(w, http.StatusForbidden, errors.New("saving Gmail drafts is disabled: restart with -draft-mail to enable draft writes"))
		return
	}
	var req draftRequest
	if err := decodeDraftRequest(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.saveDraft(w, req)
}

// draft handles one Gmail draft and its /send action.
func (s *server) draft(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/drafts/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" || strings.Contains(parts[0], "..") {
		fail(w, http.StatusBadRequest, errors.New("draft id is required"))
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "send" && r.Method == http.MethodPost {
		if !s.sendMailEnabled {
			fail(w, http.StatusForbidden, errors.New("sending mail is disabled: restart with -send-mail to send a Gmail draft"))
			return
		}
		var req struct {
			AccountID string `json:"accountId,omitempty"`
		}
		if r.ContentLength != 0 {
			if err := decodeDraftRequest(w, r, &req); err != nil {
				fail(w, http.StatusBadRequest, err)
				return
			}
		}
		accountID, err := s.connectedAccount(req.AccountID)
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		mb, err := s.draftMailbox(accountID)
		if err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
			return
		}
		messageID, threadID, err := mb.SendDraft(id)
		if err != nil {
			fail(w, gmailDraftErrorStatus(err), fmt.Errorf("Gmail did not confirm sending this draft; check Gmail before retrying: %w", err))
			return
		}
		filed := s.fileSent(accountID, mb, messageID)
		send(w, http.StatusOK, map[string]any{"accountId": accountID, "id": id, "gmailId": messageID, "threadId": threadID, "sent": true, "filed": filed})
		return
	}
	if len(parts) != 1 {
		fail(w, http.StatusNotFound, errors.New("unknown draft action"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		accountID, err := s.connectedAccount(r.URL.Query().Get("accountId"))
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		mb, err := s.draftMailbox(accountID)
		if err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
			return
		}
		d, err := mb.GetDraft(id)
		if err != nil {
			fail(w, gmailDraftErrorStatus(err), fmt.Errorf("reading draft: %w", err))
			return
		}
		send(w, http.StatusOK, draftResponse{AccountID: accountID, Draft: d})
	case http.MethodPut:
		if !s.draftMailEnabled {
			fail(w, http.StatusForbidden, errors.New("saving Gmail drafts is disabled: restart with -draft-mail to enable draft writes"))
			return
		}
		var req draftRequest
		if err := decodeDraftRequest(w, r, &req); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		req.ID = id
		s.saveDraft(w, req)
	case http.MethodDelete:
		if !s.draftMailEnabled {
			fail(w, http.StatusForbidden, errors.New("deleting Gmail drafts is disabled: restart with -draft-mail to enable draft writes"))
			return
		}
		accountID, err := s.connectedAccount(r.URL.Query().Get("accountId"))
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		mb, err := s.draftMailbox(accountID)
		if err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
			return
		}
		if err := mb.DeleteDraft(id); err != nil {
			fail(w, gmailDraftErrorStatus(err), fmt.Errorf("deleting draft: %w", err))
			return
		}
		send(w, http.StatusOK, map[string]any{"accountId": accountID, "id": id, "deleted": true})
	default:
		fail(w, http.StatusMethodNotAllowed, errors.New("method must be GET, PUT, or DELETE"))
	}
}

func (s *server) saveDraft(w http.ResponseWriter, req draftRequest) {
	if err := checkAddresses(req.To, req.Cc); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	accountID, err := s.connectedAccount(req.AccountID)
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	mb, err := s.draftMailbox(accountID)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
		return
	}
	replyToMessageID := ""
	if req.ReplyToEntry != "" {
		entries, err := s.store.ChainEntries(req.ReplyToEntry)
		if err != nil {
			fail(w, http.StatusInternalServerError, fmt.Errorf("resolving reply thread: %w", err))
			return
		}
		for _, entry := range entries {
			if entry.ExtID != req.ReplyToEntry {
				continue
			}
			if copy, ok := copyForAccount(entry, accountID); ok {
				replyToMessageID = copy.GmailID
			}
			break
		}
		if replyToMessageID == "" {
			fail(w, http.StatusBadRequest, errors.New("replyToEntry has no Gmail message in the selected account"))
			return
		}
		// Nil lists belong to the mailbox, not to corpus header estimates. Resolve
		// Reply-To and reply-all defaults before the draft makes them explicit.
		plan, err := mb.Reply(replyToMessageID, gmailclient.ReplyBody{Text: req.Body}, gmailclient.ReplyOptions{All: req.ReplyAll, To: req.To, Cc: req.Cc})
		if err != nil {
			fail(w, http.StatusBadGateway, fmt.Errorf("preparing reply draft: %w", err))
			return
		}
		req.To, req.Cc = []string{plan.To}, nil
		if plan.Cc != "" {
			req.Cc = []string{plan.Cc}
		}
		req.Subject = plan.Subject
	}
	d, err := mb.SaveDraft(gmailclient.Draft{ID: req.ID, MessageID: req.ExpectedMessageID, To: strings.Join(req.To, ", "), Cc: strings.Join(req.Cc, ", "), Subject: req.Subject, Body: req.Body}, replyToMessageID)
	if err != nil {
		status := http.StatusBadGateway
		if gmailclient.IsConflict(err) || errors.Is(err, gmailclient.ErrUnsupportedDraft) {
			status = http.StatusConflict
		}
		if req.ID != "" && gmailclient.IsNotFound(err) {
			status = http.StatusNotFound
		}
		fail(w, status, fmt.Errorf("saving Gmail draft: %w", err))
		return
	}
	send(w, http.StatusOK, draftResponse{AccountID: accountID, Draft: d})
}

func gmailDraftErrorStatus(err error) int {
	if gmailclient.IsNotFound(err) {
		return http.StatusNotFound
	}
	return http.StatusBadGateway
}

func (s *server) draftMailbox(accountID string) (draftMailbox, error) {
	mb, err := s.mailboxForAccount(accountID)
	if err != nil {
		return nil, err
	}
	drafts, ok := mb.(draftMailbox)
	if !ok {
		return nil, errors.New("mailbox does not support Gmail drafts")
	}
	return drafts, nil
}

func decodeDraftRequest(w http.ResponseWriter, r *http.Request, target any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("reading the request body: %w", err)
	}
	return nil
}
