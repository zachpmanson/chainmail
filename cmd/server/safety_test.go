package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	"github.com/zachpmanson/chainmail/internal/gmailclient"
	"golang.org/x/oauth2"
)

func TestCrossOriginMutationsAreRejected(t *testing.T) {
	for _, path := range []string{"/v1/settings", "/v1/people/1", "/v1/ops/merge", "/auth/accounts/legacy/disconnect"} {
		for _, fetchSite := range []string{"cross-site", "same-site", ""} {
			t.Run(path+"/"+fetchSite, func(t *testing.T) {
				srv := testServer(t)
				req := httptest.NewRequest("POST", "http://localhost:9876"+path, strings.NewReader(`{"slurpEvery":"off"}`))
				req.Header.Set("Content-Type", "text/plain")
				req.Header.Set("Origin", "http://attacker.invalid")
				if fetchSite != "" {
					req.Header.Set("Sec-Fetch-Site", fetchSite)
				}
				w := httptest.NewRecorder()
				srv.routes().ServeHTTP(w, req)
				if w.Code != http.StatusForbidden {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
				if _, ok, err := srv.store.Setting(corpus.SettingSlurpEvery); err != nil || ok {
					t.Fatalf("mutation reached store: present=%v err=%v", ok, err)
				}
			})
		}
	}
}

func TestSameOriginAndNonBrowserSettingsStillWork(t *testing.T) {
	for _, browser := range []bool{false, true} {
		srv := testServer(t)
		req := httptest.NewRequest("POST", "http://localhost:9876/v1/settings", strings.NewReader(`{"slurpEvery":"off"}`))
		req.Header.Set("Content-Type", "application/json")
		if browser {
			req.Header.Set("Origin", "http://localhost:9876")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("browser=%v status=%d: %s", browser, w.Code, w.Body.String())
		}
	}
}

func TestSettingsRejectedRequestChangesNothing(t *testing.T) {
	for _, body := range []string{
		`{"defaultFolder":"TRASH","defaultFolderAccountId":"legacy","slurpEvery":"invalid"}`,
		`{"defaultFolder":"TRASH","slurpEvery":"off","mePersonId":9223372036854775807}`,
		`{"defaultFolder":"TRASH","slurpEvery":"off","mePersonId":-1}`,
	} {
		srv := testServer(t)
		if err := srv.store.SetDefaultFolder("INBOX", "legacy"); err != nil {
			t.Fatal(err)
		}
		if err := srv.store.PutSetting(corpus.SettingMe, "old@example.invalid"); err != nil {
			t.Fatal(err)
		}
		before := srv.do(t, "GET", "/v1/settings", nil)
		res := srv.do(t, "POST", "/v1/settings", []byte(body))
		after := srv.do(t, "GET", "/v1/settings", nil)
		if res.status != 400 || !bytes.Equal(before.body, after.body) {
			t.Fatalf("status=%d before=%s after=%s", res.status, before.body, after.body)
		}
	}
}

type safetyTransport func(*http.Request) (*http.Response, error)

func (f safetyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeOAuth(t *testing.T, identity string, status int, exchange func()) {
	t.Helper()
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	http.DefaultClient = &http.Client{Transport: safetyTransport(func(r *http.Request) (*http.Response, error) {
		body, code := `{"access_token":"new-fake-token","token_type":"Bearer","expires_in":3600}`, 200
		switch {
		case r.URL.Host == "oauth.invalid":
			if exchange != nil {
				exchange()
			}
		case r.URL.Host == "www.googleapis.com":
			body, code = identity, status
		default:
			return nil, fmt.Errorf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
}

func beginFakeLogin(t *testing.T, srv *harness, accountID string) (string, string) {
	t.Helper()
	res := srv.do(t, "GET", "/auth/login"+accountID, nil)
	if res.status != 302 {
		t.Fatalf("login: %d %s", res.status, res.body)
	}
	srv.loginMu.Lock()
	defer srv.loginMu.Unlock()
	srv.login.Config.Endpoint = oauth2.Endpoint{TokenURL: "https://oauth.invalid/token"}
	return srv.login.State, srv.loginAccountID
}

func TestOAuthValidatesBeforeActivatingCredential(t *testing.T) {
	for _, tc := range []struct {
		name, existingEmail, otherEmail, identity string
		infoStatus, wantStatus                    int
		activationFails, newAccount               bool
	}{
		{name: "established identity cannot change", existingEmail: "a@test.invalid", identity: `{"email":"b@test.invalid"}`, infoStatus: 200, wantStatus: 409},
		{name: "wrong identity also belongs elsewhere", existingEmail: "a@test.invalid", otherEmail: "b@test.invalid", identity: `{"email":"b@test.invalid"}`, infoStatus: 200, wantStatus: 409},
		{name: "unidentified legacy cannot duplicate", otherEmail: "b@test.invalid", identity: `{"email":"b@test.invalid"}`, infoStatus: 200, wantStatus: 409},
		{name: "identity lookup fails", existingEmail: "a@test.invalid", identity: `{}`, infoStatus: 503, wantStatus: 502},
		{name: "empty identity", existingEmail: "a@test.invalid", identity: `{}`, infoStatus: 200, wantStatus: 502},
		{name: "matching reauthentication", existingEmail: "a@test.invalid", identity: `{"email":"a@test.invalid"}`, infoStatus: 200, wantStatus: 200},
		{name: "legacy first binding", identity: `{"email":"a@test.invalid"}`, infoStatus: 200, wantStatus: 200},
		{name: "new account", existingEmail: "a@test.invalid", identity: `{"email":"b@test.invalid"}`, infoStatus: 200, wantStatus: 200, newAccount: true},
		{name: "activation failure preserves credential", existingEmail: "a@test.invalid", identity: `{"email":"a@test.invalid"}`, infoStatus: 200, wantStatus: 500, activationFails: true},
		{name: "new activation failure stays disconnected", existingEmail: "a@test.invalid", identity: `{"email":"b@test.invalid"}`, infoStatus: 200, wantStatus: 500, activationFails: true, newAccount: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			srv := testServer(t)
			if err := srv.store.PutGmailAccount(corpus.GmailAccount{ID: "legacy", Email: tc.existingEmail, DisplayName: "Legacy"}); err != nil {
				t.Fatal(err)
			}
			if tc.otherEmail != "" {
				if err := srv.store.PutGmailAccount(corpus.GmailAccount{ID: "personal", Email: tc.otherEmail, DisplayName: "Personal"}); err != nil {
					t.Fatal(err)
				}
			}
			legacyPath, _ := gmailclient.AccountTokenPath("legacy")
			if err := gmailclient.SaveAccountToken(&oauth2.Token{AccessToken: "original-fake-token"}, legacyPath); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(legacyPath)
			if err != nil {
				t.Fatal(err)
			}
			query := "?account=legacy"
			if tc.newAccount {
				query = ""
			}
			state, accountID := beginFakeLogin(t, srv, query)
			path, _ := gmailclient.AccountTokenPath(accountID)
			if tc.activationFails {
				// Docket stages a rename at .tmp; a directory makes saving fail even as root.
				if err := os.MkdirAll(path+".tmp", 0o700); err != nil {
					t.Fatal(err)
				}
			}
			fakeOAuth(t, tc.identity, tc.infoStatus, nil)
			res := srv.do(t, "GET", "/?code=fake&state="+url.QueryEscape(state), nil)
			if res.status != tc.wantStatus {
				t.Fatalf("status=%d body=%s", res.status, res.body)
			}
			if tc.wantStatus == 409 || tc.wantStatus == 502 {
				accounts, err := srv.store.GmailAccounts()
				if err != nil {
					t.Fatal(err)
				}
				for _, account := range accounts {
					if account.ID == "legacy" && account.Email != tc.existingEmail {
						t.Fatalf("rejected callback changed account metadata: %+v", account)
					}
				}
			}
			data, err := os.ReadFile(legacyPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantStatus != 200 || tc.newAccount {
				if !bytes.Equal(data, before) {
					t.Fatal("rejected/unrelated callback changed original credential")
				}
			}
			if tc.wantStatus == 200 {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var tok oauth2.Token
				if err := json.Unmarshal(data, &tok); err != nil {
					t.Fatal(err)
				}
				if tok.AccessToken != "new-fake-token" {
					t.Fatalf("active token=%q", tok.AccessToken)
				}
			}
			if tc.newAccount && tc.activationFails {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("failed activation left credential: %v", err)
				}
				accounts, err := srv.store.GmailAccounts()
				if err != nil {
					t.Fatal(err)
				}
				if len(accounts) != 2 {
					t.Fatalf("expected metadata-only account, got %+v", accounts)
				}
			}
		})
	}
}

func TestOAuthCallbackConsumesPendingOnce(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := testServer(t)
	state, _ := beginFakeLogin(t, srv, "?account=legacy")
	var consumed atomic.Int32
	callback := srv.authCallbackOr(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/?error=access_denied&state="+state, nil)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			http.NewCrossOriginProtection().Handler(callback).ServeHTTP(w, req)
			if w.Code == 400 {
				consumed.Add(1)
			} else if w.Code != 418 {
				t.Errorf("status=%d", w.Code)
			}
		})
	}
	wg.Wait()
	if consumed.Load() != 1 {
		t.Fatalf("callbacks consumed=%d, want 1", consumed.Load())
	}
}

func TestOAuthInvalidExpiredAndNonGETCallbacksDoNotConsume(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := testServer(t)
	state, _ := beginFakeLogin(t, srv, "?account=legacy")
	callback := srv.authCallbackOr(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	for _, tc := range []struct{ method, state string }{{"GET", "wrong"}, {"POST", state}} {
		w := httptest.NewRecorder()
		callback(w, httptest.NewRequest(tc.method, "/?error=denied&state="+tc.state, nil))
		if w.Code != 418 {
			t.Fatalf("status=%d", w.Code)
		}
		if srv.login == nil {
			t.Fatal("invalid callback consumed flow")
		}
	}
	srv.loginExpires = time.Now().Add(-time.Second)
	w := httptest.NewRecorder()
	callback(w, httptest.NewRequest("GET", "/?error=denied&state="+state, nil))
	if w.Code != 418 {
		t.Fatalf("expired callback status=%d", w.Code)
	}
}

func TestOAuthReplacementDuringExchangeKeepsCapturedAccountAndNewPending(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := testServer(t)
	if err := srv.store.PutGmailAccount(corpus.GmailAccount{ID: "personal", Email: "b@test.invalid", DisplayName: "B"}); err != nil {
		t.Fatal(err)
	}
	state, _ := beginFakeLogin(t, srv, "?account=legacy")
	entered, release := make(chan struct{}), make(chan struct{})
	fakeOAuth(t, `{"email":"a@test.invalid"}`, 200, func() { close(entered); <-release })
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		srv.routes().ServeHTTP(w, httptest.NewRequest("GET", "/?code=fake&state="+state, nil))
		done <- w
	}()
	<-entered
	duplicate := srv.do(t, "GET", "/?code=fake&state="+state, nil)
	if duplicate.status != 200 || !strings.Contains(string(duplicate.body), "<html") {
		t.Fatalf("duplicate callback did not fall through: %d %s", duplicate.status, duplicate.body)
	}
	replacement, _ := beginFakeLogin(t, srv, "?account=personal")
	close(release)
	w := <-done
	if w.Code != 200 {
		t.Fatalf("callback=%d %s", w.Code, w.Body.String())
	}
	srv.loginMu.Lock()
	remaining := srv.login.State
	account := srv.loginAccountID
	srv.loginMu.Unlock()
	if remaining != replacement || account != "personal" {
		t.Fatal("old callback consumed replacement flow")
	}
	path, _ := gmailclient.AccountTokenPath("personal")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("callback activated replacement account: %v", err)
	}
}

func TestDisconnectCachedTokenCannotReturnOrRestoreCredential(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := testServer(t)
	path, _ := gmailclient.AccountTokenPath("legacy")
	if err := gmailclient.SaveAccountToken(&oauth2.Token{AccessToken: "fake", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	src, err := gmailclient.TokenSourceForPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if res := srv.do(t, "POST", "/auth/accounts/legacy/disconnect", nil); res.status != 200 {
		t.Fatalf("disconnect=%d %s", res.status, res.body)
	}
	if _, err := src.Token(); err == nil {
		t.Fatal("cached source returned credential after disconnect")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cached source restored credential: %v", err)
	}
}

func TestOAuthMetadataFailureDoesNotActivateCredential(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := testServer(t)
	path, _ := gmailclient.AccountTokenPath("legacy")
	if err := gmailclient.SaveAccountToken(&oauth2.Token{AccessToken: "original-fake-token"}, path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.store.DB().Exec(`create trigger reject_account before update on gmail_accounts begin select raise(abort,'injected metadata failure'); end`); err != nil {
		t.Fatal(err)
	}
	state, _ := beginFakeLogin(t, srv, "?account=legacy")
	fakeOAuth(t, `{"email":"a@test.invalid"}`, 200, nil)
	res := srv.do(t, "GET", "/?code=fake&state="+state, nil)
	if res.status != 409 {
		t.Fatalf("status=%d: %s", res.status, res.body)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("metadata failure replaced credential")
	}
}

func TestOAuthConcurrentLoginAndCallbackReplacement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := testServer(t)
	// The fixture's :memory: database belongs to one physical connection.
	srv.store.DB().SetMaxOpenConns(1)
	callback := srv.authCallbackOr(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(418) })
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			srv.authLogin(w, httptest.NewRequest("GET", "/auth/login?account=legacy", nil))
			if w.Code != 302 {
				t.Errorf("login=%d %s", w.Code, w.Body.String())
				return
			}
			u, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Error(err)
				return
			}
			callback(httptest.NewRecorder(), httptest.NewRequest("GET", "/?error=denied&state="+url.QueryEscape(u.Query().Get("state")), nil))
		})
	}
	wg.Wait()
}
