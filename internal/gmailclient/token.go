package gmailclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/zachpmanson/docket/gmail/auth"
	"golang.org/x/oauth2"
)

// No cached credential survives deletion or reauthentication. Both the read
// and any refresh/write must be inside the same lock as DisconnectAccount.
type accountTokenSource struct {
	ctx    context.Context
	config *oauth2.Config
	path   string
}

func (s *accountTokenSource) Token() (*oauth2.Token, error) {
	lock := flock.New(s.path + ".lock")
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("locking Gmail token: %w", err)
	}
	defer lock.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("reading Gmail token (account may be disconnected): %w", err)
	}
	var disk oauth2.Token
	if err := json.Unmarshal(data, &disk); err != nil {
		return nil, fmt.Errorf("parsing Gmail token: %w", err)
	}
	if disk.AccessToken == "" {
		return nil, fmt.Errorf("Gmail token has no access token")
	}
	if disk.Expiry.IsZero() || time.Now().Add(2*time.Minute).Before(disk.Expiry) {
		return &disk, nil
	}
	tok, err := s.config.TokenSource(s.ctx, &disk).Token()
	if err != nil {
		return nil, err
	}
	if err := auth.SaveToken(tok, s.path); err != nil {
		return nil, err
	}
	return tok, nil
}

// SaveAccountToken activates an interactive login under the refresh/disconnect
// lock. Docket's atomic rename preserves the previous token if saving fails.
func SaveAccountToken(tok *oauth2.Token, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("locking Gmail token: %w", err)
	}
	defer lock.Unlock()
	return auth.SaveToken(tok, path)
}
