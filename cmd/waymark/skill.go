package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const skillInstallHelp = `Usage: waymark skill install --source DIR [--client agents|codex|claude]

DIR must be the local skill directory containing a regular, non-symlink SKILL.md,
normally the repository's .skills/waymark directory, not the repository root or
the SKILL.md file itself. Directory symlinks are resolved to a canonical path.

Installs a symlink to DIR. Keep that directory available after installation.
The default client is agents (~/.agents/skills/waymark). Other clients use
$CODEX_HOME/skills/waymark (or ~/.codex/skills/waymark) and ~/.claude/skills/waymark.
Existing files, directories, and different symlinks are never replaced.
`

func cmdSkill(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(os.Stderr, skillInstallHelp)
		return flag.ErrHelp
	}
	if len(args) == 0 || args[0] != "install" {
		return errors.New("skill requires the install subcommand; use 'waymark skill install --help'")
	}
	return cmdSkillInstall(args[1:])
}

func cmdSkillInstall(args []string) error {
	fs := flag.NewFlagSet("skill install", flag.ContinueOnError)
	source := fs.String("source", "", "local directory containing SKILL.md (required)")
	client := fs.String("client", "agents", "agents, codex, or claude")
	fs.Usage = func() { fmt.Fprint(fs.Output(), skillInstallHelp) }
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("skill install accepts flags only; pass the skill directory with --source")
	}
	if *source == "" {
		return errors.New("--source is required; specify the directory containing SKILL.md, normally .skills/waymark")
	}
	destination, err := skillDestination(*client)
	if err != nil {
		return err
	}
	canonical, err := filepath.Abs(*source)
	if err != nil {
		return fmt.Errorf("resolve skill source: %w", err)
	}
	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		return fmt.Errorf("resolve skill source: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return fmt.Errorf("inspect skill source: %w", err)
	}
	if !info.IsDir() {
		return errors.New("--source must be a directory containing SKILL.md, not a file")
	}
	info, err = os.Lstat(filepath.Join(canonical, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("inspect source SKILL.md: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("source SKILL.md must be a regular file, not a symlink or directory")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create skill installation directory: %w", err)
	}
	if err := os.Symlink(canonical, destination); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("link skill: %w", err)
		}
		info, err := os.Lstat(destination)
		if err != nil {
			return fmt.Errorf("inspect existing skill installation: %w", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to overwrite existing file or directory at %s", destination)
		}
		existing, err := filepath.EvalSymlinks(destination)
		if err != nil || existing != canonical {
			return fmt.Errorf("refusing to replace different symlink at %s; inspect and remove it manually only if you intend to replace it", destination)
		}
		fmt.Printf("Skill already installed at %s -> %s\n", destination, canonical)
		return nil
	}
	fmt.Printf("Installed skill at %s -> %s\n", destination, canonical)
	return nil
}

func skillDestination(client string) (string, error) {
	switch client {
	case "agents", "codex", "claude":
	default:
		return "", fmt.Errorf("unsupported skill client %q; choose agents, codex, or claude", client)
	}
	if client == "codex" && os.Getenv("CODEX_HOME") != "" {
		return filepath.Abs(filepath.Join(os.Getenv("CODEX_HOME"), "skills", "waymark"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory for skill installation: %w", err)
	}
	return filepath.Abs(filepath.Join(home, "."+client, "skills", "waymark"))
}
