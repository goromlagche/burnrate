BIN := $(HOME)/.local/bin/burnrate
UNITS := $(HOME)/.config/systemd/user
SWIFTBAR := $(shell defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null)

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

install-macos: build
	@test -n "$(SWIFTBAR)" || { echo "Open SwiftBar once and pick a plugin folder first."; exit 1; }
	mkdir -p $(dir $(BIN))
	ln -sfn $(CURDIR)/burnrate $(BIN)
	ln -sfn $(CURDIR)/contrib/macos/burnrate-claude.1m.sh "$(SWIFTBAR)/burnrate-claude.1m.sh"
	ln -sfn $(CURDIR)/contrib/macos/burnrate-codex.1m.sh "$(SWIFTBAR)/burnrate-codex.1m.sh"

uninstall-macos:
	rm -f $(BIN) "$(SWIFTBAR)/burnrate-claude.1m.sh" "$(SWIFTBAR)/burnrate-codex.1m.sh"

.PHONY: build test install-linux uninstall-linux install-macos uninstall-macos
