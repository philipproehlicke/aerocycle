BIN      := aerocycle
VERSION  := 0.3.0
PREFIX   ?= $(HOME)/.local/bin
UID      := $(shell id -u)
LABEL    := com.aerocycle.agent
PLIST    := $(HOME)/Library/LaunchAgents/$(LABEL).plist
APP      := $(HOME)/Applications/Aerocycle.app
APPEXEC  := $(APP)/Contents/MacOS/$(BIN)

# Prefer a real code-signing identity (stable across rebuilds, so the macOS
# Accessibility grant sticks). Falls back to ad-hoc signing.
CODESIGN_ID ?= $(shell security find-identity -v -p codesigning 2>/dev/null | grep -oE '[0-9A-F]{40}' | head -1)

.PHONY: build run tui app install clean fmt vet \
        agent-install agent-restart agent-status agent-logs agent-uninstall

build:
	go build -o bin/$(BIN) .

run: build
	./bin/$(BIN) -debug

tui: build
	./bin/$(BIN) tui

# The app bundle gives macOS a clean "Aerocycle" identity for the Accessibility
# grant (a bare binary is fiddly to grant and loses the grant across rebuilds).
app: build
	mkdir -p $(APP)/Contents/MacOS
	sed 's|__VERSION__|$(VERSION)|g' app/Info.plist.tmpl > $(APP)/Contents/Info.plist
	cp bin/$(BIN) $(APPEXEC)
	@if [ -n "$(CODESIGN_ID)" ]; then \
		codesign --force --sign "$(CODESIGN_ID)" $(APP) >/dev/null 2>&1 \
			&& echo "signed Aerocycle.app with $(CODESIGN_ID)"; \
	else \
		codesign --force --sign - $(APP) >/dev/null 2>&1 \
			&& echo "signed Aerocycle.app ad-hoc"; \
	fi
	@echo "installed $(APP)"

# Expose the CLI on PATH as a symlink into the bundle, so the CLI and the agent
# share one code identity.
install: app
	mkdir -p $(PREFIX)
	ln -sf $(APPEXEC) $(PREFIX)/$(BIN)
	@echo "linked $(PREFIX)/$(BIN) -> $(APPEXEC)"

# --- launchd agent (start at login, keep running) --------------------------

agent-install: install
	mkdir -p $(HOME)/Library/LaunchAgents
	sed 's|__EXEC__|$(APPEXEC)|g' agent/$(LABEL).plist.tmpl > $(PLIST)
	@-launchctl bootout gui/$(UID)/$(LABEL) 2>/dev/null; sleep 1
	@launchctl bootstrap gui/$(UID) $(PLIST) 2>/dev/null \
		|| { -launchctl bootout gui/$(UID)/$(LABEL) 2>/dev/null; sleep 1; launchctl bootstrap gui/$(UID) $(PLIST); }
	@echo "agent installed at $(PLIST)"

agent-restart:
	launchctl kickstart -k gui/$(UID)/$(LABEL)
	@echo "agent restarted"

agent-status:
	@launchctl print gui/$(UID)/$(LABEL) 2>/dev/null | grep -E 'state|pid|program =' || echo "agent not loaded"

agent-logs:
	@tail -n 40 -f /tmp/aerocycle.err.log

agent-uninstall:
	-launchctl bootout gui/$(UID)/$(LABEL) 2>/dev/null
	rm -f $(PLIST)
	@echo "agent removed"

# --- misc ------------------------------------------------------------------

clean:
	rm -rf bin

fmt:
	gofmt -w .

vet:
	go vet ./...
