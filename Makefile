BINDIR ?= $(HOME)/.local/bin
REM_HOME ?= $(HOME)/.local/share/rem

.PHONY: install test

install:
	mkdir -p $(BINDIR) $(REM_HOME)
	go build -o $(BINDIR)/rem ./cmd/rem
	go build -o $(BINDIR)/knowledge-sync ./cmd/knowledge-sync
	rm -rf $(REM_HOME)/hooks $(REM_HOME)/lib
	cp -R hooks lib $(REM_HOME)/

test:
	GOWORK=off go test ./...
