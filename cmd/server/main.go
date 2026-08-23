// Command server serves the corpus over HTTP for the browser client: search for
// candidate chains, then build a timeline spec from the ones a human chose.
//
//	server                        loopback on the default port
//	server -addr 127.0.0.1:9000   another port
//	server -corpus ./corpus.db    another corpus
//
// Every heavy thing it does lives in internal/corpus and internal/spec. Nothing
// here ranks, fuses or assembles; it parses a request, calls that code and
// marshals the answer. api/openapi.json is the contract, and openapi_test.go
// holds the handlers to it.
//
// Operator commands stay on the CLI. ingest, embed, eval, dedupe, twins,
// repair, merge, alias and refresh all write to the corpus, and a browser is
// the wrong place to trigger a merge that person_merges cannot reverse.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zachpmanson/chainmail/internal/corpus"
	mailembed "github.com/zachpmanson/chainmail/internal/embed"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address; must be loopback")
	path := fs.String("corpus", defaultCorpusPath(), "corpus database")
	uploads := fs.String("uploads", defaultUploadDir(),
		"archive upload root; attachment thumbnails are embedded from here (\"\" to embed none)")
	model := fs.String("model", mailembed.DefaultModel, "embedding model, for mode=semantic|hybrid")
	dim := fs.Int("dim", mailembed.DefaultDim, "dimensions that model returns")
	url := fs.String("url", mailembed.DefaultBaseURL, "ollama endpoint")
	timeout := fs.Duration("embed-timeout", 2*time.Minute, "how long to wait for the model")
	serveRemote := fs.Bool(unsafeBindFlag, false,
		"permit a non-loopback -addr; read what it prints before you use it")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The bind is checked before the corpus is opened, so a refused address
	// costs nothing and cannot half-start.
	if err := checkBind(*addr, *serveRemote); err != nil {
		return err
	}

	store, err := corpus.Open(*path)
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &server{
		store:     store,
		uploads:   *uploads,
		specSlots: make(chan struct{}, specConcurrency),
		slotWait:  specSlotWait,
		embedder: func() *mailembed.Ollama {
			return &mailembed.Ollama{BaseURL: *url, Name: *model, Dimension: *dim,
				Client: &http.Client{Timeout: *timeout}}
		},
		embedWait: *timeout,
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	h := &http.Server{
		Handler: srv.routes(),
		// A spec build is seconds of HTML recovery and boilerplate detection, and
		// a write deadline would cut a large page off mid-JSON — which reads to a
		// client as a corrupt spec rather than as a timeout. specConcurrency is
		// the bound on that work instead.
		WriteTimeout:      0,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "server: %s on http://%s\n", *path, ln.Addr())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errs := make(chan error, 1)
	go func() { errs <- h.Serve(ln) }()
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return h.Shutdown(ctx)
	}
}

// unsafeBindFlag is spelled as a sentence because its name is the last warning
// anyone reads. See checkBind for what it costs.
const unsafeBindFlag = "serve-personal-mail-to-the-network"

// checkBind refuses any address this process could accept a non-loopback
// connection on.
//
// There is no authentication here, of any kind, and POST /v1/spec returns
// sender HTML with no sanitisation (#14) — so a page it builds can run script
// from anyone who ever emailed this mailbox, against an origin that can read
// the whole corpus. The loopback bind is not a default: it is the only thing
// making the surface safe. Closing #14 is a prerequisite for widening it, not
// a follow-up.
//
// A wildcard host ("" or 0.0.0.0 or ::) is refused rather than resolved, since
// it binds every interface the machine has, including ones acquired later.
func checkBind(addr string, override bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-addr %q: want host:port, e.g. 127.0.0.1:8765", addr)
	}
	if port == "" {
		return fmt.Errorf("-addr %q: no port", addr)
	}
	remote := ""
	switch {
	case host == "":
		remote = "every interface"
	case net.ParseIP(host) != nil:
		if ip := net.ParseIP(host); !ip.IsLoopback() {
			if ip.IsUnspecified() {
				remote = "every interface"
			} else {
				remote = ip.String()
			}
		}
	default:
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("-addr %q: %w", addr, err)
		}
		for _, ip := range ips {
			if !ip.IsLoopback() {
				remote = host + " (" + ip.String() + ")"
				break
			}
		}
	}
	if remote == "" {
		return nil
	}
	if !override {
		return fmt.Errorf("-addr %q reaches %s, and this serves personal mail with no "+
			"authentication and unsanitised sender HTML (#14). Bind 127.0.0.1, or pass "+
			"-%s if you have read what that means",
			addr, remote, unsafeBindFlag)
	}
	fmt.Fprintf(os.Stderr,
		"server: WARNING serving personal mail on %s — no authentication, and\n"+
			"server: WARNING spec bodies are unsanitised sender HTML (#14). Anyone who can\n"+
			"server: WARNING reach this port can read the whole corpus.\n", remote)
	return nil
}

func defaultCorpusPath() string {
	if p := os.Getenv("CHAINMAIL_CORPUS"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "chainmail", "corpus.db")
}

// defaultUploadDir matches the CLI's: the Slack downloader's sibling of the
// archive it filled.
func defaultUploadDir() string {
	if p := os.Getenv("CHAINMAIL_SLACK_ARCHIVE"); p != "" {
		return filepath.Join(filepath.Dir(p), "__uploads")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "chainmail", "slack", "__uploads")
}
