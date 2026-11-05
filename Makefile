BINDIR ?= $(HOME)/.local/bin
REM_HOME ?= $(HOME)/.local/share/rem

.PHONY: install test

# Build beside the target and rename: overwriting a binary in place can get its next run killed by macOS.
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
