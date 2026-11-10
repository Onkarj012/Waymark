package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func skillTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	return home
}

func skillTestSource(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), ".skills", "waymark")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("# Waymark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return source
}

func skillTestSymlink(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Symlink(source, destination); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func skillTestLink(t *testing.T, destination, want string) {
	t.Helper()
	got, err := os.Readlink(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("link target = %q, want %q", got, want)
	}
}

func TestSkillInstallClients(t *testing.T) {
	tests := []struct {
		name   string
		client string
		base   string
	}{
		{name: "default", base: ".agents"},
		{name: "agents", client: "agents", base: ".agents"},
		{name: "codex", client: "codex", base: ".codex"},
		{name: "claude", client: "claude", base: ".claude"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := skillTestHome(t)
			source := skillTestSource(t)
			args := []string{"install", "--source", source}
			if test.client != "" {
				args = append(args, "--client", test.client)
			}
			if err := cmdSkill(args); err != nil {
				t.Fatal(err)
			}
			canonical, err := filepath.EvalSymlinks(source)
			if err != nil {
				t.Fatal(err)
			}
			skillTestLink(t, filepath.Join(home, test.base, "skills", "waymark"), canonical)
		})
	}
}

func TestSkillInstallCodexHome(t *testing.T) {
	home := skillTestHome(t)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	source := skillTestSource(t)
	if err := cmdSkill([]string{"install", "--client", "codex", "--source", source}); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	skillTestLink(t, filepath.Join(codexHome, "skills", "waymark"), canonical)
	if _, err := os.Lstat(filepath.Join(home, ".codex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default Codex directory should be untouched: %v", err)
	}
}

func TestSkillInstallRejectsArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing subcommand", want: "install subcommand"},
		{name: "unknown subcommand", args: []string{"remove"}, want: "install subcommand"},
		{name: "missing source", args: []string{"install"}, want: "--source is required"},
		{name: "empty source", args: []string{"install", "--source", ""}, want: "--source is required"},
		{name: "missing source value", args: []string{"install", "--source"}, want: "flag needs an argument"},
		{name: "unknown client", args: []string{"install", "--source", "source", "--client", "other"}, want: "unsupported skill client"},
		{name: "empty client", args: []string{"install", "--source", "source", "--client", ""}, want: "unsupported skill client"},
		{name: "positional source", args: []string{"install", "source"}, want: "accepts flags only"},
		{name: "extra argument", args: []string{"install", "--source", "source", "extra"}, want: "accepts flags only"},
		{name: "unknown flag", args: []string{"install", "--unknown"}, want: "flag provided but not defined"},
		{name: "replace unsupported", args: []string{"install", "--replace"}, want: "flag provided but not defined"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := skillTestHome(t)
			err := cmdSkill(test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("cmdSkill() error = %v, want %q", err, test.want)
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid arguments changed home: %v, %v", entries, err)
			}
		})
	}
}

func TestSkillInstallHelp(t *testing.T) {
	skillTestHome(t)
	for _, args := range [][]string{{"--help"}, {"-h"}, {"install", "--help"}, {"install", "-h"}} {
		if err := cmdSkill(args); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("cmdSkill(%q) error = %v, want help", args, err)
		}
	}
}

func TestSkillInstallRejectsInvalidSources(t *testing.T) {
	for _, name := range []string{"missing directory", "repository root", "skill file", "missing SKILL.md", "directory SKILL.md", "symlink SKILL.md", "dangling SKILL.md"} {
		t.Run(name, func(t *testing.T) {
			home := skillTestHome(t)
			source := skillTestSource(t)
			skillFile := filepath.Join(source, "SKILL.md")
			switch name {
			case "missing directory":
				source = filepath.Join(t.TempDir(), "missing")
			case "repository root":
				source = filepath.Dir(filepath.Dir(source))
			case "skill file":
				source = skillFile
			default:
				if err := os.Remove(skillFile); err != nil {
					t.Fatal(err)
				}
				switch name {
				case "directory SKILL.md":
					if err := os.Mkdir(skillFile, 0o755); err != nil {
						t.Fatal(err)
					}
				case "symlink SKILL.md":
					skillTestSymlink(t, filepath.Join(skillTestSource(t), "SKILL.md"), skillFile)
				case "dangling SKILL.md":
					skillTestSymlink(t, filepath.Join(source, "missing"), skillFile)
				}
			}
			if err := cmdSkill([]string{"install", "--source", source}); err == nil {
				t.Fatal("invalid source accepted")
			}
			if _, err := os.Lstat(filepath.Join(home, ".agents")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid source created destination parent: %v", err)
			}
		})
	}
}

func TestSkillInstallCanonicalSource(t *testing.T) {
	home := skillTestHome(t)
	source := skillTestSource(t)
	alias := filepath.Join(t.TempDir(), "alias")
	skillTestSymlink(t, source, alias)
	t.Chdir(filepath.Dir(alias))
	if err := cmdSkill([]string{"install", "--source", "alias"}); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	skillTestLink(t, filepath.Join(home, ".agents", "skills", "waymark"), canonical)
}

func TestSkillInstallIdempotent(t *testing.T) {
	home := skillTestHome(t)
	source := skillTestSource(t)
	args := []string{"install", "--source", source}
	if err := cmdSkill(args); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(home, ".agents", "skills", "waymark")
	before, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdSkill(args); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("identical link was replaced")
	}
}

func TestSkillInstallRelativeExistingLink(t *testing.T) {
	home := skillTestHome(t)
	source := skillTestSource(t)
	parent := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(parent, source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "waymark")
	skillTestSymlink(t, relative, destination)
	if err := cmdSkill([]string{"install", "--source", source}); err != nil {
		t.Fatal(err)
	}
	skillTestLink(t, destination, relative)
}

func TestSkillInstallPreservesExistingEntries(t *testing.T) {
	for _, name := range []string{"file", "directory", "different link", "dangling link"} {
		t.Run(name, func(t *testing.T) {
			home := skillTestHome(t)
			source := skillTestSource(t)
			destination := filepath.Join(home, ".agents", "skills", "waymark")
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			preserved := destination
			switch name {
			case "directory":
				if err := os.Mkdir(destination, 0o755); err != nil {
					t.Fatal(err)
				}
				preserved = filepath.Join(destination, "keep")
			case "different link":
				preserved = filepath.Join(skillTestSource(t), "SKILL.md")
				skillTestSymlink(t, filepath.Dir(preserved), destination)
			case "dangling link":
				skillTestSymlink(t, filepath.Join(home, "missing"), destination)
				preserved = filepath.Join(home, "keep")
			}
			if err := os.WriteFile(preserved, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}
			if err := cmdSkill([]string{"install", "--source", source}); err == nil {
				t.Fatal("existing entry was accepted")
			}
			after, err := os.Lstat(destination)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("existing entry changed: %v", err)
			}
			data, err := os.ReadFile(preserved)
			if err != nil || string(data) != "keep" {
				t.Fatalf("existing data changed: %q, %v", data, err)
			}
		})
	}
}

func TestSkillInstallRejectsBlockedParent(t *testing.T) {
	home := skillTestHome(t)
	parent := filepath.Join(home, ".agents")
	if err := os.WriteFile(parent, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdSkill([]string{"install", "--source", skillTestSource(t)}); err == nil {
		t.Fatal("installation through a file accepted")
	}
	data, err := os.ReadFile(parent)
	if err != nil || string(data) != "keep" {
		t.Fatalf("blocking file changed: %q, %v", data, err)
	}
}
