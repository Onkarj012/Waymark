package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorInspectsSkillsWithoutRepairingCopies(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	report := doctorReport{OK: true}
	report.inspectSkills()
	requireDoctorCheck(t, report, "agent-skill", "warn")
	destination := filepath.Join(home, ".agents", "skills", "waymark")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("existing skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	report = doctorReport{OK: true}
	report.inspectSkills()
	requireDoctorCheck(t, report, "agent-skill", "warn")
	if !strings.Contains(report.Checks[0].Message, "copies may drift") {
		t.Fatal("installed copy not identified")
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("source skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(home, ".codex", "skills", "waymark")
	if err := os.MkdirAll(filepath.Dir(linked), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, linked); err != nil {
		t.Skip(err)
	}
	report = doctorReport{OK: true}
	report.inspectSkills()
	requireDoctorCheck(t, report, "agent-skill", "pass")
	data, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	if err != nil || string(data) != "existing skill" {
		t.Fatal("doctor changed an installed skill")
	}
}
