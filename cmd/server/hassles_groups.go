package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/hassles"
)

// The provider must grant an exclusive lease for the entire invocation. There is
// deliberately no production default or in-memory fallback. Providers must honor
// context cancellation rather than waiting indefinitely for an external lease.
type hassleLeaseProvider func(context.Context) (hassles.GroupStore, func(), error)

type hassleRecovery struct {
	Status  string                `json:"status"`
	Fence   string                `json:"fence,omitempty"`
	Pending *hassles.GroupPending `json:"pending,omitempty"`
}

func (s *server) admitHassle(w http.ResponseWriter) bool {
	if !s.hasslesMu.TryLock() {
		fail(w, 409, errors.New("Hassles is busy; refresh before retrying"))
		return false
	}
	return true
}

func (s *server) acquireHassleLease(ctx context.Context) (hassles.GroupStore, func(), error) {
	if s.leaseHassles == nil {
		return nil, nil, errors.New("Hassles operation store is unavailable")
	}
	store, release, err := s.leaseHassles(ctx)
	if err != nil || store == nil || release == nil {
		if release != nil {
			release()
		}
		return nil, nil, errors.New("Hassles operation store is unavailable or busy")
	}
	return store, release, nil
}
func inspectHassleRecovery(ctx context.Context, store hassles.GroupStore) hassleRecovery {
	status, err := hassles.InspectGroupState(ctx, store)
	if err != nil {
		return hassleRecovery{Status: "unavailable"}
	}
	out := hassleRecovery{Status: "ready", Fence: status.Fence, Pending: status.Pending}
	if status.Pending != nil {
		out.Status = "pending"
	}
	return out
}
func (s *server) localHassleRecovery(ctx context.Context) hassleRecovery {
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if !s.hasslesMu.TryLock() {
		return hassleRecovery{Status: "busy"}
	}
	defer s.hasslesMu.Unlock()
	store, release, err := s.acquireHassleLease(ctx)
	if err != nil {
		return hassleRecovery{Status: "unavailable"}
	}
	defer release()
	return inspectHassleRecovery(ctx, store)
}
func (s *server) hassleRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	send(w, 200, s.localHassleRecovery(r.Context()))
}
func (s *server) hassleEmptyLease(w http.ResponseWriter, ctx context.Context) (hassles.GroupStore, func(), bool) {
	store, release, err := s.acquireHassleLease(ctx)
	if err != nil {
		fail(w, 409, err)
		return nil, nil, false
	}
	status := inspectHassleRecovery(ctx, store)
	if status.Status != "ready" {
		release()
		fail(w, 409, errors.New("Hassles writes and selection blocked: operation store is pending or unavailable"))
		return nil, nil, false
	}
	return store, release, true
}
func (s *server) hassleBinding(ctx context.Context, loc hassles.Location) (hassles.GroupBinding, error) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if err := ctx.Err(); err != nil {
		return hassles.GroupBinding{}, err
	}
	stored, err := s.hassleLocation()
	if err != nil || stored != loc || loc.AccountID == "" {
		return hassles.GroupBinding{}, errors.New("selected reminder location changed")
	}
	if _, err = s.connectedAccount(loc.AccountID); err != nil {
		return hassles.GroupBinding{}, err
	}
	accounts, err := s.store.GmailAccounts()
	if err != nil {
		return hassles.GroupBinding{}, err
	}
	for _, a := range accounts {
		if a.ID == loc.AccountID && a.Email != "" {
			return hassles.GroupBinding{Actor: hassles.GroupActor{ServiceUID: strconv.Itoa(os.Getuid()), AccountID: a.ID, Email: strings.ToLower(a.Email)}, Location: loc}, nil
		}
	}
	return hassles.GroupBinding{}, errors.New("known connected account identity required")
}
func (s *server) checkHassleBinding(ctx context.Context, binding hassles.GroupBinding) error {
	if !s.hasslesEnabled {
		return errors.New("Hassles is disabled")
	}
	current, err := s.hassleBinding(ctx, binding.Location)
	if err != nil {
		return err
	}
	if current != binding {
		return errors.New("reminder actor changed")
	}
	return nil
}
func readHassleBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		fail(w, 400, err)
		return nil, false
	}
	return raw, true
}
func (s *server) hassleGroupMutation(w http.ResponseWriter, r *http.Request) {
	if !s.hassleGrant(w) {
		return
	}
	raw, ok := readHassleBody(w, r)
	if !ok {
		return
	}
	in, err := hassles.DecodeLocatedGroupRequest(raw)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if !s.admitHassle(w) {
		return
	}
	defer s.hasslesMu.Unlock()
	store, release, err := s.acquireHassleLease(ctx)
	if err != nil {
		fail(w, 409, err)
		return
	}
	defer release()
	binding, err := s.hassleBinding(ctx, in.Location)
	if err != nil {
		fail(w, 409, err)
		return
	}
	b, err := s.selectedHasslesContext(ctx, in.Location)
	if err != nil {
		fail(w, 409, err)
		return
	}
	backend, ok := b.(hassles.GroupBackend)
	if !ok {
		fail(w, 503, errors.New("full calendar group transport unavailable"))
		return
	}
	out, err := hassles.ExecuteGroup(ctx, backend, store, in.Group, binding, s.checkHassleBinding, time.Now())
	s.sendHassleGroupResult(w, ctx, store, out, err)
}
func (s *server) sendHassleGroupResult(w http.ResponseWriter, ctx context.Context, store hassles.GroupStore, out hassles.GroupOutcome, err error) {
	// An ambiguous save may have persisted despite returning an error. Never infer
	// readiness from the outcome's zero value; reload the validated local record.
	status := inspectHassleRecovery(ctx, store)
	code := 200
	message := ""
	if err != nil || !out.Settled || status.Status != "ready" {
		code = 409
		message = "Group operation did not settle; inspect recovery before any further write"
	}
	send(w, code, struct {
		Recovery hassleRecovery `json:"recovery"`
		Settled  bool           `json:"settled"`
		Error    string         `json:"error,omitempty"`
	}{status, code == 200, message})
}
func (s *server) hassleRecoveryReconcile(w http.ResponseWriter, r *http.Request) {
	if !s.hassleGrant(w) {
		return
	}
	raw, ok := readHassleBody(w, r)
	if !ok {
		return
	}
	expected, err := hassles.DecodeGroupRecoveryRequest(raw)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if !s.admitHassle(w) {
		return
	}
	defer s.hasslesMu.Unlock()
	store, release, err := s.acquireHassleLease(ctx)
	if err != nil {
		fail(w, 409, err)
		return
	}
	defer release()
	status, err := hassles.InspectGroupState(ctx, store)
	if err != nil || status.Pending == nil {
		s.sendHassleGroupResult(w, ctx, store, hassles.GroupOutcome{}, errors.New("no readable pending operation"))
		return
	}
	if expected != "" && expected != status.Pending.OperationID {
		s.sendHassleGroupResult(w, ctx, store, hassles.GroupOutcome{}, hassles.ErrConflict)
		return
	}
	binding := status.Pending.Binding
	if err = s.checkHassleBinding(ctx, binding); err != nil {
		s.sendHassleGroupResult(w, ctx, store, hassles.GroupOutcome{}, err)
		return
	}
	b, err := s.selectedHasslesContext(ctx, binding.Location)
	if err != nil {
		s.sendHassleGroupResult(w, ctx, store, hassles.GroupOutcome{}, err)
		return
	}
	reader, ok := b.(hassles.GroupReader)
	if !ok {
		fail(w, 503, errors.New("full calendar group reader unavailable"))
		return
	}
	out, err := hassles.ReconcilePending(ctx, reader, store, expected, s.checkHassleBinding)
	s.sendHassleGroupResult(w, ctx, store, out, err)
}

// Lock order: hasslesMu -> lease -> authMu. OAuth reconnect takes authMu only.
func (s *server) hassleDisconnectGate(ctx context.Context, accountID string) (func(), error) {
	if !s.hasslesMu.TryLock() {
		return nil, errors.New("Hassles is busy; disconnect refused")
	}
	loc, err := s.hassleLocation()
	if err != nil {
		s.hasslesMu.Unlock()
		return nil, err
	}
	if s.leaseHassles == nil && loc.AccountID != accountID {
		s.hasslesMu.Unlock()
		return func() {}, nil
	}
	store, release, err := s.acquireHassleLease(ctx)
	if err != nil {
		s.hasslesMu.Unlock()
		return nil, err
	}
	status, err := hassles.InspectGroupState(ctx, store)
	if err != nil || status.Pending != nil && status.Pending.Binding.Actor.AccountID == accountID {
		release()
		s.hasslesMu.Unlock()
		return nil, errors.New("account disconnect blocked by unavailable or bound pending Hassles operation")
	}
	return func() { release(); s.hasslesMu.Unlock() }, nil
}
