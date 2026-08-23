# Operating the corpus. `make help` lists what each target does.
#
# The sequence lives in the binary, as `corpus slurp`: the order matters for how
# much of the work a run finishes, and a Makefile does not ship in the nix
# package, so a host with the binaries would have had the phases and not the
# order. The targets below name the phases one at a time, for running one by hand.

CORPUS      ?= $(HOME)/.local/state/chainmail/corpus.db
SLACK       ?= $(HOME)/.local/state/chainmail/slack
BIN         ?= $(HOME)/.local/bin/corpus
SERVER      ?= $(HOME)/.local/bin/chainmail-server
PORT        ?= 8765
# SINCE bounds the mail query, and nothing else does: `ingest mail` follows
# docket's page token to the end of a query and reports whether it got there, so
# the window is a choice about how much to fetch rather than a way to stay under
# a cap.
SINCE       ?= 2026-08-01
# The address the spec treats as "me". No default: a mailbox owner is personal
# data and this repository is public.
ME          ?= $(CHAINMAIL_ME)
export CHAINMAIL_CORPUS = $(CORPUS)

.PHONY: help install test check slurp slurp-mail slurp-slack settle embed \
        backup page serve api doctor

help:
	@printf 'Corpus\n'
	@printf '  make slurp          corpus slurp: slack, mail, settle, embed, in order\n'
	@printf '  make slurp-mail     mail since SINCE=%s, paged to the end of the query\n' '$(SINCE)'
	@printf '  make slurp-slack    refresh the slackdump archive, then ingest it\n'
	@printf '  make settle         twins, repair, then dedupe (dry run)\n'
	@printf '  make embed          vectors for entries that have none\n'
	@printf '  make backup         copy the corpus before anything risky\n'
	@printf '  make doctor         what is in the corpus and what is missing\n'
	@printf '  make status         probe each backend, write the connection snapshot\n'
	@printf '\nBuild\n'
	@printf '  make install        build the corpus binary to %s\n' '$(BIN)'
	@printf '  make test           go + frontend tests\n'
	@printf '  make check          test, vet, gofmt, typecheck\n'
	@printf '  make page Q=...     generate a spec and render it\n'
	@printf '  make repage P=...   refresh that page and re-render it, marking what changed\n'
	@printf '  make serve          vite dev server\n'
	@printf '  make api            the read-only API on 127.0.0.1:%s\n' '$(PORT)'
	@printf '\nOverride CORPUS, SLACK, SINCE, BIN on the command line.\n'

install:
	go build -o $(BIN) ./cmd/corpus
	go build -o $(SERVER) ./cmd/server

test:
	go test ./...
	npx vitest run

check: test
	go vet ./...
	@test -z "$$(gofmt -l internal cmd)" || { gofmt -l internal cmd; exit 1; }
	npm run typecheck

# A copy, not a snapshot: sqlite needs the -wal and -shm sidecars too, or the
# copy will not open.
backup:
	@cp $(CORPUS) $(CORPUS).bak
	@for s in -wal -shm; do [ -f $(CORPUS)$$s ] && cp $(CORPUS)$$s $(CORPUS).bak$$s || true; done
	@echo "backed up to $(CORPUS).bak"

# One implementation of the order, and it is not this file: `corpus slurp` runs
# the phases and reports each, so a headless host runs the same sequence a
# checkout does. The targets below stay because a phase is often run alone —
# after fixing an identity by hand, or once ollama is finally up.
slurp:
	$(BIN) slurp -since $(SINCE) -archive $(SLACK)/slackdump.sqlite

# `resume` continues from slackdump's own checkpoint, so this is incremental.
# Messages are treated as immutable, so ingest is insert-or-skip.
slurp-slack:
	$(BIN) slurp -only slack -archive $(SLACK)/slackdump.sqlite

# One query, paged to its end. `ingest mail` prints "complete" when it reached
# the end and writes INCOMPLETE to stderr when a bound stopped it, so a short
# run is visible instead of being inferred from a count.
#
# Re-running is cheap, not just harmless: the cursor for this query holds the
# frontier the last completed run reached, and the walk stops there rather than
# reading back to SINCE.
#
# Straight to `ingest mail` rather than through slurp: one phase alone needs no
# sequencing, and this is the target a hand-written query gets pasted into.
slurp-mail:
	$(BIN) ingest mail -q "after:$(subst -,/,$(SINCE))"

# twins and repair are idempotent and refuse rather than guess. dedupe is a dry
# run on purpose: its merges weigh evidence and CANNOT be undone —
# person_merges records that a merge happened, not how to reverse it. `slurp`
# has no flag that would apply them either; this prints the command that does.
settle:
	$(BIN) slurp -only settle
	@echo
	@echo "the plan above is a DRY RUN — review, then: $(BIN) dedupe -apply"

# Needs the embedding daemon. OLLAMA_KEEP_ALIVE=-1 keeps the model resident, so
# a search does not pay a cold model load after five idle minutes. `slurp -only
# embed` skips rather than fails when the daemon is down, which is right for a
# nightly and wrong here: a target asked for by hand should say it did nothing.
embed:
	@curl -sf -m 3 http://localhost:11434/api/tags >/dev/null \
	  || { echo "no embedding daemon: OLLAMA_KEEP_ALIVE=-1 ollama serve &"; exit 1; }
	$(BIN) embed

doctor:
	@$(BIN) stats
	@echo
	@$(BIN) zones | head -5
	@echo
	@$(BIN) sigs -domains | head -4

# Probe each backend and write the connection snapshot the server's
# /v1/status serves. Intentionally shallow: mail is asked whether docket's
# session answers one query, the Slack archive whether it opens, and the
# embedding daemon whether it is up — none of them fetch anything.
status:
	@$(BIN) status -archive $(SLACK)/slackdump.sqlite

# make page Q="billing csv" ME=you@example.com
page:
	@test -n "$(Q)" || { echo 'usage: make page Q="<query>" [T="<title>"]'; exit 2; }
	@test -n "$(ME)" || { echo 'set ME=<your address> or CHAINMAIL_ME'; exit 2; }
	$(BIN) spec -q "$(Q)" -limit $(or $(LIMIT),6) -title "$(or $(T),$(Q))" \
	  -me $(ME) -o $(HOME)/Downloads/spec.json
	npm run render -- $(HOME)/Downloads/spec.json -o $(HOME)/Downloads/page.html
	@echo
	@echo "http://localhost:5173/?spec=/@fs$(HOME)/Downloads/spec.json"

# make repage P=$(HOME)/Downloads/page.html
#
# Takes the page rather than a spec, since a page carries the spec that made it:
# one file to name instead of two that have to be kept in step. It is copied
# aside first, because it is both the input and the diff base and the render
# would otherwise overwrite what it is being compared against.
repage:
	@test -n "$(P)" || { echo 'usage: make repage P="<page.html>" [FETCH=1]'; exit 2; }
	cp "$(P)" "$(P).prev"
	$(BIN) refresh "$(P).prev" $(if $(FETCH),-fetch,) -o $(HOME)/Downloads/spec.json
	npm run render -- $(HOME)/Downloads/spec.json -o "$(P)" --since "$(P).prev"

serve:
	npm run dev

# Loopback, and it refuses anything else without a flag that says why. There is
# no authentication and spec bodies are unsanitised sender HTML (#14), so the
# dev client reaches this through a Vite proxy rather than over CORS.
api:
	go run ./cmd/server -addr 127.0.0.1:$(PORT) -corpus $(CORPUS)
