package gmailclient

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zachpmanson/docket/gmail/auth"
)

const syntheticProviderConfig = `default_calendar = "synthetic-calendar"
[provider]
client_id = "synthetic-client-id"
client_secret = "synthetic-not-a-secret"
auth_url = "https://oauth.example.test/auth"
token_url = "https://oauth.example.test/token"
redirect_uri = "http://localhost:8765"
scopes = ["synthetic-scope"]
use_pkce = true
`

// Offline dependency-compatibility coverage only. Never load a user's config,
// credential, account registry or provider; production paths remain unchanged.
func TestDocketConfigCompatibility(t *testing.T) {
	for _, permissions := range []bool{false, true} {
		for _, symlink := range []bool{false, true} {
			t.Run(map[bool]string{false: "legacy", true: "permissions"}[permissions]+map[bool]string{false: "-file", true: "-immutable-symlink"}[symlink], func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				t.Setenv("DOCKET_CLIENT_ID", "")
				t.Setenv("DOCKET_CLIENT_SECRET", "")
				path, err := auth.ConfigPath()
				if err != nil {
					t.Fatal(err)
				}
				if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				raw := []byte(syntheticProviderConfig)
				if permissions {
					raw = append(raw, []byte(`
[permissions.defaults]
"mail.read" = true
"mail.send" = false
[permissions.accounts.synthetic]
"mail.read" = false
`)...)
				}
				target := path
				if symlink {
					target = filepath.Join(t.TempDir(), "immutable-config.toml")
				}
				if err = os.WriteFile(target, raw, 0o400); err != nil {
					t.Fatal(err)
				}
				if symlink {
					if err = os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				}
				cfg, err := auth.LoadConfig()
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Provider.ClientID != "synthetic-client-id" || cfg.Provider.TokenURL != "https://oauth.example.test/token" || !cfg.Provider.UsePKCE || cfg.DefaultCalendar != "synthetic-calendar" {
					t.Fatalf("config conversion changed: %+v", cfg)
				}
				if permissions && (!cfg.Permissions.Defaults["mail.read"] || cfg.Permissions.Defaults["mail.send"] || cfg.Permissions.Accounts["synthetic"]["mail.read"]) {
					t.Fatal("quoted-operation policies changed")
				}
				after, err := os.ReadFile(target)
				if err != nil || !bytes.Equal(raw, after) {
					t.Fatal("LoadConfig rewrote operator config", err)
				}
				info, err := os.Stat(target)
				if err != nil || info.Mode().Perm() != 0o400 {
					t.Fatal("immutable config mode changed", err)
				}
				if symlink {
					if _, err = os.Readlink(path); err != nil {
						t.Fatal("operator symlink replaced", err)
					}
				}
			})
		}
	}
}
func TestDocketConfigRejectsUnsupportedPolicyShapes(t *testing.T) {
	for name, bad := range map[string]string{
		"unknown-provider-key":      "[provider]\nclient_id=\"synthetic\"\nunknown_key=true\n",
		"unknown-operation":         syntheticProviderConfig + "\n[permissions.defaults]\n\"mail.reed\"=true\n",
		"unquoted-operation":        syntheticProviderConfig + "\n[permissions.defaults]\nmail.read=true\n",
		"wrong-policy-type":         syntheticProviderConfig + "\n[permissions.defaults]\n\"mail.read\"=\"true\"\n",
		"invalid-account":           syntheticProviderConfig + "\n[permissions.accounts.\"../escape\"]\n\"mail.read\"=true\n",
		"invalid-read-only-account": "read_only_accounts=[\"../escape\"]\n" + syntheticProviderConfig,
		"wrong-account-type":        "read_only_accounts=true\n" + syntheticProviderConfig,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("DOCKET_CLIENT_ID", "")
			t.Setenv("DOCKET_CLIENT_SECRET", "")
			path, err := auth.ConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, []byte(bad), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = auth.LoadConfig(); err == nil {
				t.Fatal("invalid config did not fail closed")
			}
		})
	}
}
