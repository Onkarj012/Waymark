package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// config is the persisted CLI login.
type config struct {
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// configDir resolves the directory holding config.json:
//
//	$WAYMARK_CONFIG_DIR, else $XDG_CONFIG_HOME/waymark,
//	else ~/.config/waymark
func configDir() string {
	if d := os.Getenv("WAYMARK_CONFIG_DIR"); d != "" {
		return d
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "waymark")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "waymark")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

// loadConfig reads the saved config. A missing file yields a zero config and
// no error, so callers can treat "not logged in" as empty fields.
func loadConfig() (config, error) {
	var c config
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", configPath(), err)
	}
	return c, nil
}

// saveConfig writes the config with restrictive permissions (it holds a secret).
func saveConfig(c config) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(configPath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", configPath(), err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("secure %s: %w", configPath(), err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", configPath(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", configPath(), err)
	}
	return nil
}

// resolve applies flag/env/config precedence for the server and env/config
// precedence for the device token.
func resolve(serverFlag string) (server, token, serverSrc, tokenSrc string, err error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", "", "", "", err
	}

	switch {
	case strings.TrimSpace(serverFlag) != "":
		server, serverSrc = serverFlag, "flag"
	case os.Getenv("WAYMARK_URL") != "":
		server, serverSrc = os.Getenv("WAYMARK_URL"), "env"
	case cfg.URL != "":
		server, serverSrc = cfg.URL, "config"
	}

	switch {
	case os.Getenv("WAYMARK_TOKEN") != "":
		token, tokenSrc = os.Getenv("WAYMARK_TOKEN"), "env"
	case cfg.Token != "":
		token, tokenSrc = cfg.Token, "config"
	}

	return strings.TrimRight(strings.TrimSpace(server), "/"), strings.TrimSpace(token), serverSrc, tokenSrc, nil
}

// readLine prompts on stderr and reads one visible line from stdin.
func readLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
