BIN := $(HOME)/.local/bin/burnrate
UNITS := $(HOME)/.config/systemd/user
AGENT := $(HOME)/Library/LaunchAgents/burnrate.plist

# Installed files are symlinks back into this checkout.
build:
	go build -o burnrate .

test:
	go vet ./...
	go test ./...

install-linux: build
	mkdir -p $(dir $(BIN)) $(UNITS)
	ln -sfn $(CURDIR)/burnrate $(BIN)
	ln -sfn $(CURDIR)/contrib/linux/burnrate.service $(UNITS)/burnrate.service
	ln -sfn $(CURDIR)/contrib/linux/burnrate.timer $(UNITS)/burnrate.timer
	systemctl --user daemon-reload
	systemctl --user enable --now burnrate.timer

uninstall-linux:
	-systemctl --user disable --now burnrate.timer
	rm -f $(BIN) $(UNITS)/burnrate.service $(UNITS)/burnrate.timer
	systemctl --user daemon-reload

# Needs Xcode Command Line Tools for swiftc.
install-macos: build
	swiftc -O contrib/macos/BurnrateBar.swift -o burnrate-bar
	mkdir -p $(dir $(BIN)) $(dir $(AGENT))
	ln -sfn $(CURDIR)/burnrate $(BIN)
	sed 's|@BAR@|$(CURDIR)/burnrate-bar|' contrib/macos/burnrate.plist > $(AGENT)
	-launchctl bootout gui/$$(id -u) $(AGENT) 2>/dev/null
	launchctl bootstrap gui/$$(id -u) $(AGENT)

uninstall-macos:
	-launchctl bootout gui/$$(id -u) $(AGENT)
	rm -f $(BIN) $(AGENT)

.PHONY: build test install-linux uninstall-linux install-macos uninstall-macos
