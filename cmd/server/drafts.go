package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/spec"
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
	AccountID    string `json:"accountId"`
	Kind         string `json:"kind"`
	Resolution   string `json:"resolution"`
	ReplyToEntry string `json:"replyToEntry,omitempty"`
	RootID       string `json:"rootId,omitempty"`
	gmailclient.Draft
}

// Navigation enrichment must not turn successful Gmail writes into failures.
func (s *server) draftResponse(accountID string, d gmailclient.Draft) draftResponse {
	out := draftResponse{AccountID: accountID, Draft: d, Kind: "unresolved"}
	parent, resolution := d.ReplyHeaders.Parent()
	out.Resolution = resolution
	if resolution == "compose" {
		out.Kind = "compose"
		return out
	}
	if resolution != "candidate" {
		return out
	}
	nav, err := s.store.ResolveDraftParent(accountID, parent)
	if err != nil {
		// Static output only: no SQL, header values or provider error data.
		out.Resolution = "lookup-unavailable"
		return out
	}
	out.Resolution = nav.Resolution
	if nav.Resolution == "resolved" {
		out.Kind, out.ReplyToEntry, out.RootID = "reply", nav.ReplyToEntry, nav.RootID
	}
	return out
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
			out = append(out, s.draftResponse(accountID, d))
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
	if len(parts) == 2 && parts[1] == "save-check" {
		if r.Method != http.MethodGet {
			fail(w, http.StatusMethodNotAllowed, errors.New("method must be GET"))
			return
		}
		s.checkDraftSave(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "mime-check" {
		if r.Method != http.MethodGet {
			fail(w, http.StatusMethodNotAllowed, errors.New("method must be GET"))
			return
		}
		s.checkDraftMIME(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "sender-check" {
		if r.Method != http.MethodGet {
			fail(w, http.StatusMethodNotAllowed, errors.New("method must be GET"))
			return
		}
		s.checkDraftSender(w, r, id)
		return
	}
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
		send(w, http.StatusOK, s.draftResponse(accountID, d))
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
	req, replyToMessageID, status, err := s.planDraftRequest(accountID, mb, req)
	if err != nil {
		fail(w, status, err)
		return
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
		operation := "create"
		if req.ID != "" {
			operation = "update"
		}
		fail(w, status, fmt.Errorf("saving Gmail draft: %w [operation=%s; reply_parent=%t]", err, operation, req.ReplyToEntry != ""))
		return
	}
	send(w, http.StatusOK, s.draftResponse(accountID, d))
}

// Shared read-only planning keeps save and preflight audience/subject rules equal.
func (s *server) planDraftRequest(accountID string, mb mailbox, req draftRequest) (draftRequest, string, int, error) {
	replyToMessageID := ""
	if req.ReplyToEntry != "" {
		target, err := s.store.ReplyTarget(req.ReplyToEntry)
		if err != nil {
			return req, "", http.StatusInternalServerError, fmt.Errorf("resolving reply thread: %w", err)
		}
		copy, ok := copyForAccount(corpus.ChainEntry{Copies: target.Copies}, accountID)
		if !ok {
			return req, "", http.StatusBadRequest, errors.New("replyToEntry has no Gmail message in the selected account")
		}
		replyToMessageID = copy.GmailID
		req.Body = spec.ComposeReply(req.Body, target).Text
		// Nil lists belong to the mailbox, not to corpus header estimates. Resolve
		// Reply-To and reply-all defaults before the draft makes them explicit.
		plan, err := mb.Reply(replyToMessageID, gmailclient.ReplyBody{Text: req.Body}, gmailclient.ReplyOptions{Send: false, All: req.ReplyAll, To: req.To, Cc: req.Cc})
		if err != nil {
			return req, "", http.StatusBadGateway, fmt.Errorf("preparing reply draft: %w", err)
		}
		req.To, req.Cc = []string{plan.To}, nil
		if plan.Cc != "" {
			req.Cc = []string{plan.Cc}
		}
		req.Subject = plan.Subject
	}
	return req, replyToMessageID, 0, nil
}

// Explicit read-only probe: ordinary draft lists/reads do not gain settings calls.
func (s *server) checkDraftSender(w http.ResponseWriter, r *http.Request, id string) {
	accountID, err := s.connectedAccount(r.URL.Query().Get("accountId"))
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	mb, err := s.mailboxForAccount(accountID)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("opening the mailbox: %w", err))
		return
	}
	checker, ok := mb.(interface{ CheckDraftSender(string) error })
	if !ok {
		fail(w, http.StatusBadGateway, errors.New("mailbox does not support draft sender checks"))
		return
	}
	if err := checker.CheckDraftSender(id); err != nil {
		status := gmailDraftErrorStatus(err)
		if errors.Is(err, gmailclient.ErrUnsupportedDraft) {
			status = http.StatusConflict
		}
		fail(w, status, fmt.Errorf("checking Gmail draft sender: %w", err))
		return
	}
	send(w, http.StatusOK, map[string]any{"accountId": accountID, "id": id, "senderCompatible": true})
}

// This diagnostic deliberately returns no identifiers or arbitrary error text.
func (s *server) checkDraftMIME(w http.ResponseWriter, r *http.Request, id string) {
	requested := r.URL.Query().Get("accountId")
	if strings.TrimSpace(requested) == "" {
		fail(w, http.StatusBadRequest, errors.New("accountId is required"))
		return
	}
	accountID, err := s.connectedAccount(requested)
	if err != nil {
		fail(w, http.StatusConflict, errors.New("selected Gmail account is unavailable"))
		return
	}
	mb, err := s.mailboxForAccount(accountID)
	if err != nil {
		fail(w, http.StatusBadGateway, errors.New("mailbox unavailable for draft MIME checking"))
		return
	}
	checker, ok := mb.(interface {
		CheckDraftMIME(string) (*gmailclient.DraftMIMEPart, error)
	})
	if !ok {
		fail(w, http.StatusBadGateway, errors.New("mailbox does not support draft MIME checks"))
		return
	}
	tree, err := checker.CheckDraftMIME(id)
	if err != nil {
		switch {
		case gmailclient.IsNotFound(err):
			fail(w, http.StatusNotFound, errors.New("Gmail draft no longer exists"))
		case errors.Is(err, gmailclient.ErrDraftMIMEMalformed):
			fail(w, http.StatusConflict, gmailclient.ErrDraftMIMEMalformed)
		case errors.Is(err, gmailclient.ErrDraftMIMELimit):
			fail(w, http.StatusConflict, gmailclient.ErrDraftMIMELimit)
		case errors.Is(err, gmailclient.ErrDraftMIMEEncoding):
			fail(w, http.StatusConflict, gmailclient.ErrDraftMIMEEncoding)
		default:
			fail(w, http.StatusBadGateway, errors.New("Gmail could not check draft MIME"))
		}
		return
	}
	if tree == nil {
		fail(w, http.StatusBadGateway, errors.New("Gmail could not check draft MIME"))
		return
	}
	send(w, http.StatusOK, tree)
}

// No identifiers, provider text or preparation errors leave this diagnostic.
func (s *server) checkDraftSave(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	requested := query.Get("accountId")
	if strings.TrimSpace(requested) == "" {
		fail(w, http.StatusBadRequest, errors.New("accountId is required"))
		return
	}
	witnesses, present := query["expectedMessageId"]
	if present && (len(witnesses) != 1 || strings.TrimSpace(witnesses[0]) == "") {
		fail(w, http.StatusBadRequest, errors.New("expectedMessageId must be nonempty and unique"))
		return
	}
	accountID, err := s.connectedAccount(requested)
	if err != nil {
		fail(w, http.StatusConflict, errors.New("selected Gmail account is unavailable"))
		return
	}
	mb, err := s.mailboxForAccount(accountID)
	if err != nil {
		fail(w, http.StatusBadGateway, errors.New("mailbox unavailable for draft save checking"))
		return
	}
	if present {
		s.checkDraftSaveWithContext(w, accountID, mb, id, witnesses[0])
		return
	}
	checker, ok := mb.(interface {
		CheckDraftSave(string) (gmailclient.DraftSaveCheck, error)
	})
	if !ok {
		fail(w, http.StatusBadGateway, errors.New("mailbox does not support draft save checks"))
		return
	}
	result, err := checker.CheckDraftSave(id)
	if err != nil {
		if gmailclient.IsNotFound(err) {
			fail(w, http.StatusNotFound, errors.New("Gmail draft no longer exists"))
		} else {
			fail(w, http.StatusBadGateway, errors.New("Gmail could not check draft save compatibility"))
		}
		return
	}
	send(w, http.StatusOK, result)
}

func (s *server) checkDraftSaveWithContext(w http.ResponseWriter, accountID string, mb mailbox, id, expected string) {
	out := gmailclient.DraftSaveCheck{Context: "unsupported", Status: "indeterminate"}
	reader, ok := mb.(interface {
		GetDraft(string) (gmailclient.Draft, error)
	})
	checker, canCheck := mb.(interface {
		CheckDraftSaveWithContext(gmailclient.Draft, string) (gmailclient.DraftSaveCheck, error)
	})
	if !ok || !canCheck {
		fail(w, http.StatusBadGateway, errors.New("mailbox does not support draft save checks"))
		return
	}
	d, err := reader.GetDraft(id)
	if err != nil {
		if gmailclient.IsNotFound(err) {
			fail(w, http.StatusNotFound, errors.New("Gmail draft no longer exists"))
		} else {
			fail(w, http.StatusBadGateway, errors.New("Gmail could not read draft for save checking"))
		}
		return
	}
	if d.MessageID != expected {
		out.Status = "revision_changed"
		send(w, http.StatusOK, out)
		return
	}
	parent, resolution := d.ReplyHeaders.Parent()
	parentID := ""
	switch resolution {
	case "compose":
	case "candidate":
		nav, err := s.store.ResolveDraftParent(accountID, parent)
		if err != nil || nav.Resolution != "resolved" {
			send(w, http.StatusOK, out)
			return
		}
		// Explicit empty Cc must not ask Reply to restore its default audience.
		to, toErr := draftCheckAudience(d.To)
		cc, ccErr := draftCheckAudience(d.Cc)
		if toErr != nil || ccErr != nil {
			out.Status = "unsupported"
			send(w, http.StatusOK, out)
			return
		}
		req := draftRequest{To: to, Cc: cc, Subject: d.Subject, Body: d.Body, ReplyToEntry: nav.ReplyToEntry}
		req, parentID, _, err = s.planDraftRequest(accountID, mb, req)
		if err != nil {
			send(w, http.StatusOK, out)
			return
		}
		d.To, d.Cc, d.Subject, d.Body = strings.Join(req.To, ", "), strings.Join(req.Cc, ", "), req.Subject, req.Body
	default:
		out.Status = "unsupported"
		send(w, http.StatusOK, out)
		return
	}
	out, err = checker.CheckDraftSaveWithContext(d, parentID)
	if err != nil {
		// Only this checker draft GET can propagate 404, never a parent GET.
		if gmailclient.IsNotFound(err) {
			fail(w, http.StatusNotFound, errors.New("Gmail draft no longer exists"))
		} else {
			fail(w, http.StatusBadGateway, errors.New("Gmail could not check draft save compatibility"))
		}
		return
	}
	send(w, http.StatusOK, out)
}

func draftCheckAudience(value string) ([]string, error) {
	out := []string{}
	if strings.TrimSpace(value) == "" {
		return out, nil
	}
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		out = append(out, address.String())
	}
	return out, nil
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
