BINDIR ?= $(HOME)/.local/bin
REM_HOME ?= $(HOME)/.local/share/rem

.PHONY: install test

# Build beside the target and rename, so a scheduled run never sees a partial binary.
# macOS may kill a launchd job's first run after a rebuild (OS_REASON_CODESIGNING); the next run passes.
install:
	mkdir -p $(BINDIR) $(REM_HOME)
	go build -o $(BINDIR)/.rem.tmp ./cmd/rem
	mv -f $(BINDIR)/.rem.tmp $(BINDIR)/rem
	go build -o $(BINDIR)/.knowledge-sync.tmp ./cmd/knowledge-sync
	mv -f $(BINDIR)/.knowledge-sync.tmp $(BINDIR)/knowledge-sync
	rm -rf $(REM_HOME)/hooks $(REM_HOME)/lib
	cp -R hooks lib $(REM_HOME)/

test:
	GOWORK=off go test ./...
