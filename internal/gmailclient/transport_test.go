package gmailclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the production constructor and subsequent mailbox calls, not just
// Token(): a service-level credential cache would bypass the disk guard.
func TestGmailClientTransportHonorsCredentialReplacementAndDisconnect(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := AccountTokenPath("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "inert-old", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var headers []string
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	http.DefaultClient = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		// No request can reach a real Google endpoint, including unexpected ones.
		if r.URL.Host != "gmail.googleapis.com" {
			return nil, fmt.Errorf("unexpected fixture URL: %s", r.URL)
		}
		body := `{"messages":[]}`
		if strings.HasSuffix(r.URL.Path, "/labels") {
			body = `{"labels":[]}`
		}
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	client, err := NewForTokenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Search("in:anywhere", 1, ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := append([]string(nil), headers...)
	mu.Unlock()
	for _, header := range first {
		if header != "Bearer inert-old" {
			t.Fatalf("initial Authorization=%q", header)
		}
	}
	if len(first) < 2 {
		t.Fatalf("expected constructor and search requests, got %d", len(first))
	}
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "inert-replacement", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Search("in:anywhere", 1, ""); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	replacement := headers[len(headers)-1]
	before := len(headers)
	mu.Unlock()
	if replacement != "Bearer inert-replacement" {
		t.Fatalf("reauthentication ignored by Gmail transport: %q", replacement)
	}
	if err := DisconnectAccount("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Search("in:anywhere", 1, ""); err == nil {
		t.Fatal("Gmail client continued after disconnect")
	}
	mu.Lock()
	after := len(headers)
	mu.Unlock()
	if after != before {
		t.Fatalf("disconnected request reached HTTP fixture: before=%d after=%d", before, after)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("transport recreated credential: %v", err)
	}
}

func TestGmailServiceTransportHonorsDiskChangesAtLocalEndpoint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := AccountTokenPath("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "inert-old", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	src, err := TokenSourceForPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newService(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var headers []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"labels":[]}`))
	}))
	defer endpoint.Close()
	svc.BasePath = endpoint.URL + "/"
	if _, err := svc.Users.Labels.List("me").Do(); err != nil {
		t.Fatal(err)
	}
	if err := SaveAccountToken(&oauth2.Token{AccessToken: "inert-replacement", Expiry: time.Now().Add(time.Hour)}, path); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Users.Labels.List("me").Do(); err != nil {
		t.Fatal(err)
	}
	if err := DisconnectAccount("legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Users.Labels.List("me").Do(); err == nil {
		t.Fatal("disconnected service reached endpoint")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(headers) != 2 || headers[0] != "Bearer inert-old" || headers[1] != "Bearer inert-replacement" {
		t.Fatalf("HTTP requests Authorization=%q", headers)
	}
}

func TestGmailServicePreservesCustomHTTPClientAndRequestContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-value")
	var calls int
	base := &http.Client{Timeout: time.Second, Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Context().Value(contextKey{}) != "request-value" {
			t.Error("request context lost")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("custom client timeout lost")
		}
		if r.Header.Get("Authorization") != "Bearer inert-fixture" {
			t.Errorf("Authorization=%q", r.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"labels":[]}`)), Request: r}, nil
	})}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	svc, err := newService(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "inert-fixture"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Users.Labels.List("me").Context(ctx).Do(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("custom transport calls=%d", calls)
	}
	if _, ok := base.Transport.(fixtureTransport); !ok {
		t.Fatal("service mutated the supplied HTTP client")
	}
}

func TestGmailServiceHonorsCancellationAndTimeout(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer endpoint.Close()
	base := endpoint.Client()
	base.Timeout = 50 * time.Millisecond
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, base)
	svc, err := newService(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "inert-fixture"}))
	if err != nil {
		t.Fatal(err)
	}
	svc.BasePath = endpoint.URL + "/"
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.Users.Labels.List("me").Context(canceled).Do(); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	if _, err := svc.Users.Labels.List("me").Context(ctx).Do(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out request: %v", err)
	}
}
