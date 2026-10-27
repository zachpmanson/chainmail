package gmailclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/zachpmanson/chainmail/internal/mailingest"
)

func historyClient(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	ctx := context.Background()
	svc, err := gmail.NewService(ctx, option.WithEndpoint(srv.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return Client{ctx: ctx, svc: svc}
}

func TestHistoryTransportReadsProfileAndAllTypedEvents(t *testing.T) {
	c := historyClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/gmail/v1/users/me/profile":
			fmt.Fprint(w, `{"historyId":"18446744073709551000"}`)
		case "/gmail/v1/users/me/history":
			q := r.URL.Query()
			if q.Get("startHistoryId") != "18446744073709551000" || q.Get("pageToken") != "next" || q.Get("maxResults") != "500" {
				t.Errorf("query %v", q)
			}
			if q.Has("labelId") || q.Has("historyTypes") {
				t.Error("history was filtered", q)
			}
			fmt.Fprint(w, `{"historyId":"18446744073709551010","nextPageToken":"last","history":[{"messages":[{"id":"redundant"}],"messagesAdded":[{"message":{"id":"added"}}],"labelsAdded":[{"message":{"id":"labelled"}}],"labelsRemoved":[{"message":{"id":"archived"}}],"messagesDeleted":[{"message":{"id":"deleted"}}]}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	})
	id, err := c.HistoryID()
	if err != nil || id != "18446744073709551000" {
		t.Fatalf("profile %s %v", id, err)
	}
	p, err := c.History(id, "next")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "18446744073709551010" || p.NextPageToken != "last" || !reflect.DeepEqual(p.Changed, []string{"added", "labelled", "archived"}) || !reflect.DeepEqual(p.Deleted, []string{"deleted"}) {
		t.Fatalf("page %+v", p)
	}
}

func TestHistoryTransportOnly404MeansExpiredOrGone(t *testing.T) {
	for _, code := range []int{404, 403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c := historyClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				fmt.Fprintf(w, `{"error":{"code":%d,"message":"injected"}}`, code)
			})
			_, err := c.History("123", "")
			if err == nil || errors.Is(err, mailingest.ErrHistoryExpired) != (code == 404) {
				t.Fatalf("history err %v", err)
			}
			_, err = c.Read("gone")
			if err == nil || errors.Is(err, mailingest.ErrMessageGone) != (code == 404) {
				t.Fatalf("read err %v", err)
			}
		})
	}
}
