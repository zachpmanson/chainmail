package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"github.com/zachpmanson/chainmail/internal/hassles"
)

type hassleConfig struct {
	Enabled  bool             `json:"enabled"`
	Location hassles.Location `json:"location"`
}

func (s *server) hassleLocation() (hassles.Location, error) {
	var loc hassles.Location
	value, ok, err := s.store.Setting(corpus.SettingHasslesLocation)
	if err != nil || !ok {
		return loc, err
	}
	err = json.Unmarshal([]byte(value), &loc)
	return loc, err
}
func (s *server) hassleBackend(ctx context.Context, account string) (hassles.Backend, error) {
	if account == "" {
		return nil, errors.New("select an explicitly connected Google account")
	}
	if _, err := s.connectedAccount(account); err != nil {
		return nil, err
	}
	if s.openHassles != nil {
		return s.openHassles(ctx, account)
	}
	path, err := gmailclient.AccountTokenPath(account)
	if err != nil {
		return nil, err
	}
	source, err := gmailclient.TokenSourceForPath(ctx, path)
	if err != nil {
		return nil, errors.New("selected account is disconnected; reconnect it")
	}
	accounts, err := s.store.GmailAccounts()
	if err != nil {
		return nil, err
	}
	for _, metadata := range accounts {
		if metadata.ID == account {
			return hassles.NewClient(source, metadata.Email)
		}
	}
	return nil, errors.New("selected account metadata is unavailable")
}
func validateHassleCalendar(ctx context.Context, b hassles.Backend, id string) error {
	all, err := b.Calendars(ctx)
	if err != nil {
		return err
	}
	canonical, err := hassles.Collection(id)
	if err != nil || canonical != id {
		return errors.New("invalid calendar selection")
	}
	for _, c := range all {
		if c.ID == id {
			return nil
		}
	}
	return errors.New("selected calendar is unavailable for this account; choose it again")
}
func decodeHassle(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		fail(w, 400, err)
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, errors.New("expected one JSON object"))
		return false
	}
	return true
}
func (s *server) hassleGrant(w http.ResponseWriter) bool {
	if !s.hasslesEnabled {
		fail(w, 403, errors.New("Hassles is disabled; the operator must explicitly enable -hassles Calendar access"))
		return false
	}
	return true
}
func (s *server) getHassleLocation(w http.ResponseWriter, r *http.Request) {
	s.hasslesMu.Lock()
	defer s.hasslesMu.Unlock()
	loc, err := s.hassleLocation()
	if err != nil {
		fail(w, 500, err)
		return
	}
	send(w, 200, hassleConfig{s.hasslesEnabled, loc})
}
func (s *server) setHassleLocation(w http.ResponseWriter, r *http.Request) {
	if !s.hassleGrant(w) {
		return
	}
	var in struct {
		Location  hassles.Location `json:"location"`
		Previous  hassles.Location `json:"previous"`
		Dedicated bool             `json:"dedicated"`
	}
	if !decodeHassle(w, r, &in) {
		return
	}
	if !in.Dedicated {
		fail(w, 400, errors.New("acknowledge that every event in this dedicated calendar will be treated as a task"))
		return
	}
	s.hasslesMu.Lock()
	defer s.hasslesMu.Unlock()
	old, err := s.hassleLocation()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if old != in.Previous {
		fail(w, 409, hassles.ErrConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	b, err := s.hassleBackend(ctx, in.Location.AccountID)
	if err == nil {
		err = validateHassleCalendar(ctx, b, in.Location.CalendarID)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	raw, _ := json.Marshal(in.Location)
	if err = s.store.PutSetting(corpus.SettingHasslesLocation, string(raw)); err != nil {
		fail(w, 500, err)
		return
	}
	send(w, 200, hassleConfig{s.hasslesEnabled, in.Location})
}
func (s *server) hassleCalendars(w http.ResponseWriter, r *http.Request) {
	if !s.hassleGrant(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	b, err := s.hassleBackend(ctx, r.URL.Query().Get("accountId"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	calendars, err := b.Calendars(ctx)
	if err != nil {
		fail(w, 502, err)
		return
	}
	send(w, 200, struct {
		Calendars []hassles.Calendar `json:"calendars"`
	}{calendars})
}
func (s *server) hassleInventory(w http.ResponseWriter, r *http.Request) {
	if !s.hassleGrant(w) {
		return
	}
	loc := hassles.Location{AccountID: r.URL.Query().Get("accountId"), CalendarID: r.URL.Query().Get("calendarId")}
	s.hasslesMu.Lock()
	defer s.hasslesMu.Unlock()
	b, ctx, cancel, err := s.selectedHassles(r, loc)
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		fail(w, 409, err)
		return
	}
	objects, err := b.Objects(ctx, loc.CalendarID)
	if err != nil {
		fail(w, 502, err)
		return
	}
	tasks, err := hassles.SelectedInventory(objects, loc, time.Now())
	if err != nil {
		fail(w, 502, err)
		return
	}
	send(w, 200, struct {
		Tasks []hassles.Task `json:"tasks"`
	}{tasks})
}
func (s *server) selectedHassles(r *http.Request, loc hassles.Location) (hassles.Backend, context.Context, context.CancelFunc, error) {
	stored, err := s.hassleLocation()
	if err != nil {
		return nil, nil, nil, err
	}
	if loc.AccountID == "" || loc != stored {
		return nil, nil, nil, errors.New("reminder location changed or is unselected; refresh and select again")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	b, err := s.hassleBackend(ctx, loc.AccountID)
	if err == nil {
		err = validateHassleCalendar(ctx, b, loc.CalendarID)
	}
	return b, ctx, cancel, err
}
func (s *server) hassleMutation(w http.ResponseWriter, r *http.Request) {
	if !s.hassleGrant(w) {
		return
	}
	var in hassles.Mutation
	if !decodeHassle(w, r, &in) {
		return
	}
	if in.Action != "create" && in.Action != "rename" && in.Action != "edit" && in.Action != "done" && in.Action != "delete" && in.Action != "adopt" && in.Action != "done-instance" {
		fail(w, 400, errors.New("unsupported task action"))
		return
	}
	if err := hassles.ValidateAdoptionFields(in); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Action == "create" && !hassles.ValidUUID(in.ID) {
		fail(w, 400, errors.New("invalid task UUID"))
		return
	}
	if in.Action == "create" || in.Action == "edit" {
		if _, _, err := in.Schedule.Validate(); err != nil {
			fail(w, 400, err)
			return
		}
	}
	s.hasslesMu.Lock()
	defer s.hasslesMu.Unlock()
	b, ctx, cancel, err := s.selectedHassles(r, in.Location)
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		fail(w, 409, err)
		return
	}
	guarded := selectedHassleWriter{Backend: b, check: func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		stored, err := s.hassleLocation()
		if err != nil {
			return err
		}
		if !s.hasslesEnabled || stored != in.Location {
			return hassles.ErrConflict
		}
		_, err = s.connectedAccount(in.Location.AccountID)
		return err
	}}
	result, err := hassles.Mutate(ctx, guarded, in, time.Now())
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, hassles.ErrConflict) {
			status = 409
		}
		fail(w, status, fmt.Errorf("%w (inventory must be refreshed; writes are not atomic)", err))
		return
	}
	send(w, 200, result)
}

type selectedHassleWriter struct {
	hassles.Backend
	check func() error
}

func (b selectedHassleWriter) Write(ctx context.Context, collection, href, etag, body string) error {
	if err := b.check(); err != nil {
		return err
	}
	return b.Backend.Write(ctx, collection, href, etag, body)
}
