package main

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed contrib/linux/burnrate.service contrib/linux/burnrate.timer contrib/macos/BurnrateBar.swift contrib/macos/burnrate.plist
var contrib embed.FS

var (
	unitDir  = filepath.Join(home, ".config", "systemd", "user")
	appDir   = filepath.Join(home, "Library", "Application Support", "burnrate")
	agent    = filepath.Join(home, "Library", "LaunchAgents", "burnrate.plist")
	launchID = fmt.Sprintf("gui/%d", os.Getuid())
)

// install runs burnrate every minute: a systemd user timer on Linux, a menu
// bar app started at login on macOS. Both point at this binary.
func install() error {
	bin, err := os.Executable()
	if err == nil {
		bin, err = filepath.EvalSymlinks(bin)
	}
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "linux":
		for _, name := range []string{"burnrate.service", "burnrate.timer"} {
			if err := writeTemplate("contrib/linux/"+name, filepath.Join(unitDir, name), "@BIN@", bin); err != nil {
				return err
			}
		}
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return run("systemctl", "--user", "enable", "--now", "burnrate.timer")
	case "darwin":
		src, bar := filepath.Join(appDir, "BurnrateBar.swift"), filepath.Join(appDir, "burnrate-bar")
		if err := writeTemplate("contrib/macos/BurnrateBar.swift", src); err != nil {
			return err
		}
		if err := run("swiftc", "-O", src, "-o", bar); err != nil {
			return fmt.Errorf("%w (install Xcode Command Line Tools: xcode-select --install)", err)
		}
		if err := writeTemplate("contrib/macos/burnrate.plist", agent, "@BAR@", bar, "@BIN@", bin); err != nil {
			return err
		}
		_ = exec.Command("launchctl", "bootout", launchID, agent).Run()
		return run("launchctl", "bootstrap", launchID, agent)
	}
	return fmt.Errorf("install supports Linux and macOS, not %s", runtime.GOOS)
}

func uninstall() error {
	switch runtime.GOOS {
	case "linux":
		_ = run("systemctl", "--user", "disable", "--now", "burnrate.timer")
		os.Remove(filepath.Join(unitDir, "burnrate.service"))
		os.Remove(filepath.Join(unitDir, "burnrate.timer"))
		return run("systemctl", "--user", "daemon-reload")
	case "darwin":
		_ = run("launchctl", "bootout", launchID, agent)
		os.Remove(agent)
		return os.RemoveAll(appDir)
	}
	return fmt.Errorf("uninstall supports Linux and macOS, not %s", runtime.GOOS)
}

// writeTemplate writes an embedded file with placeholders replaced. It removes
// the old file first, so it never writes through a symlink.
func writeTemplate(name, dst string, replace ...string) error {
	b, err := contrib.ReadFile(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	os.Remove(dst)
	return os.WriteFile(dst, []byte(strings.NewReplacer(replace...).Replace(string(b))), 0o644)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
