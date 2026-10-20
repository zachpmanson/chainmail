package gmailclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestAccountTokenSourceReReadsDiskAndNeverResurrects(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := AccountTokenPath("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "old", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	src, err := TokenSourceForPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "replacement", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	tok, err := src.Token()
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "replacement" {
		t.Fatalf("source returned cached token: %q", tok.AccessToken)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Token(); err == nil {
		t.Fatal("source ignored invalid disk credential")
	}
	if err := DisconnectAccount("legacy"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := src.Token(); err == nil {
			t.Fatal("disconnected source returned a token")
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source recreated credential: %v", err)
	}
}

func TestAccountTokenRefreshSerializedWithDisconnect(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, _ := AccountTokenPath("legacy")
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "expired", RefreshToken: "fake-refresh", Expiry: time.Now().Add(-time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"refreshed-fake","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer endpoint.Close()
	cfg := &oauth2.Config{ClientID: "fake", Endpoint: oauth2.Endpoint{TokenURL: endpoint.URL}}
	src := &accountTokenSource{ctx: context.Background(), config: cfg, path: path}
	refreshed := make(chan error, 1)
	go func() { _, err := src.Token(); refreshed <- err }()
	<-entered
	disconnected := make(chan error, 1)
	go func() { disconnected <- DisconnectAccount("legacy") }()
	close(release)
	if err := <-refreshed; err != nil {
		t.Fatal(err)
	}
	if err := <-disconnected; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := src.Token(); err == nil {
				t.Error("source returned token after disconnect")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("refresh recreated credential: %v", err)
	}
}

func TestAccountTokenSourcesShareRefreshedDiskCredential(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, _ := AccountTokenPath("legacy")
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "expired", RefreshToken: "fake-refresh", Expiry: time.Now().Add(-time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"refreshed-fake","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer endpoint.Close()
	cfg := &oauth2.Config{ClientID: "fake", Endpoint: oauth2.Endpoint{TokenURL: endpoint.URL}}
	var wg sync.WaitGroup
	for range 10 {
		src := &accountTokenSource{ctx: context.Background(), config: cfg, path: path}
		wg.Go(func() {
			tok, err := src.Token()
			if err != nil {
				t.Error(err)
				return
			}
			if tok.AccessToken != "refreshed-fake" {
				t.Errorf("token=%q", tok.AccessToken)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d want 1", calls.Load())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		t.Fatal(err)
	}
	if tok.RefreshToken != "fake-refresh" {
		t.Fatal("refresh token lost")
	}
}
