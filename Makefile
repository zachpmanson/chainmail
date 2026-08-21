# Operating the corpus. `make help` lists what each target does.
#
# The slurp targets are ordered because each feeds the next: twins removes
# duplicate rows before repair reads identities, and repair settles identities
# before dedupe weighs evidence about them. Running them out of order is not
# destructive, it just leaves work for the following run.

CORPUS      ?= $(HOME)/.local/state/chainmail/corpus.db
SLACK       ?= $(HOME)/.local/state/chainmail/slack
BIN         ?= $(HOME)/.local/bin/corpus
# Mail is paged by week: docket caps a search at 500 results and `ingest mail`
# takes a single page, so one wide query truncates silently. A week is well
# under the cap on this mailbox — see the `saw 500` warning in slurp-mail.
SINCE       ?= 2026-08-01
export CHAINMAIL_CORPUS = $(CORPUS)

.PHONY: help install test check slurp slurp-mail slurp-slack settle embed \
        backup page serve doctor

help:
	@printf 'Corpus\n'
	@printf '  make slurp          everything: slack, mail, settle, embed\n'
	@printf '  make slurp-mail     mail only, paged by week from SINCE=%s\n' '$(SINCE)'
	@printf '  make slurp-slack    refresh the slackdump archive, then ingest it\n'
	@printf '  make settle         twins, repair, then dedupe (dry run)\n'
	@printf '  make embed          vectors for entries that have none\n'
	@printf '  make backup         copy the corpus before anything risky\n'
	@printf '  make doctor         what is in the corpus and what is missing\n'
	@printf '\nBuild\n'
	@printf '  make install        build the corpus binary to %s\n' '$(BIN)'
	@printf '  make test           go + frontend tests\n'
	@printf '  make check          test, vet, gofmt, typecheck\n'
	@printf '  make page Q=...     generate a spec and render it\n'
	@printf '  make repage P=...   refresh that page and re-render it, marking what changed\n'
	@printf '  make serve          vite dev server\n'
	@printf '\nOverride CORPUS, SLACK, SINCE, BIN on the command line.\n'

install:
	go build -o $(BIN) ./cmd/corpus

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

slurp: slurp-slack slurp-mail settle embed

# `resume` continues from slackdump's own checkpoint, so this is incremental.
# Messages are treated as immutable, so ingest is insert-or-skip.
slurp-slack:
	slackdump resume $(SLACK)
	$(BIN) ingest slack -archive $(SLACK)/slackdump.sqlite

# A window reporting exactly `saw 500` hit docket's cap and is INCOMPLETE —
# split it and re-run. Nothing here detects that for you; issue #11 is the fix.
slurp-mail:
	@python3 -c "import datetime; a=datetime.date.fromisoformat('$(SINCE)'); \
b=datetime.date.today()+datetime.timedelta(days=1); \
[print(a.strftime('%Y/%m/%d'),(a:=min(a+datetime.timedelta(days=7),b)).strftime('%Y/%m/%d')) \
for _ in iter(lambda: a<b, False)]" | while read -r from to; do \
	  printf '%s..%s  ' "$$from" "$$to"; \
	  $(BIN) ingest mail -q "after:$$from before:$$to" -limit 500 | head -1; \
	done

# twins and repair are idempotent and refuse rather than guess. dedupe is a dry
# run on purpose: its merges weigh evidence and CANNOT be undone —
# person_merges records that a merge happened, not how to reverse it.
settle:
	$(BIN) twins -apply
	$(BIN) repair
	@echo
	@echo "dedupe below is a DRY RUN — review, then: $(BIN) dedupe -apply"
	@echo
	$(BIN) dedupe

# Needs the embedding daemon. OLLAMA_KEEP_ALIVE=-1 keeps the model resident, so
# a search does not pay a cold model load after five idle minutes.
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

# make page Q="ruralco billing csv"
page:
	@test -n "$(Q)" || { echo 'usage: make page Q="<query>" [T="<title>"]'; exit 2; }
	$(BIN) spec -q "$(Q)" -limit $(or $(LIMIT),6) -title "$(or $(T),$(Q))" \
	  -me $(or $(ME),zach@termina.io) -o $(HOME)/Downloads/spec.json
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
