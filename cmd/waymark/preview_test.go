package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Onkarj012/Waymark/theme"
)

func TestPreviewFromFileIsDeterministicAndDarkFirst(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "body.html")
	body := `<header><h1>Preview</h1></header><section><p>Body</p></section>`
	if err := os.WriteFile(bodyPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(dir, "nested", "first.html")
	second := filepath.Join(dir, "second.html")
	for _, output := range []string{first, second} {
		var stdout, stderr bytes.Buffer
		err := runPreview([]string{"--title", "Status <check>", "--output", output, bodyPath}, strings.NewReader(""), &stdout, &stderr)
		if err != nil {
			t.Fatalf("runPreview(%q): %v\nstderr: %s", output, err, stderr.String())
		}
		if got, want := stdout.String(), "✓ Generated preview\n"+output+"\n"; got != want {
			t.Fatalf("stdout = %q, want %q", got, want)
		}
	}

	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("identical preview inputs produced different bytes")
	}
	got := string(firstBytes)
	for _, want := range []string{
		`<!DOCTYPE html>`,
		`<html lang="en" data-theme="dark">`,
		`<title>Status &lt;check&gt;</title>`,
		"<style>\n" + theme.CSS,
		body,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("preview output missing %q", want)
		}
	}
	if strings.Contains(got, `href="/theme.css"`) {
		t.Fatal("offline preview links the hosted stylesheet")
	}
	if runtime.GOOS != "windows" {
		assertMode(t, first, 0o644)
		assertMode(t, filepath.Dir(first), 0o755)
	}
}

func TestPreviewReadsStdinAndAllowsInlineSVG(t *testing.T) {
	output := filepath.Join(t.TempDir(), "preview.html")
	body := `<figure><svg viewBox="0 0 10 10"><path d="M0 0L10 10"></path></svg></figure>`
	if err := runPreview([]string{"--title", "Chart", "--output", output, "-"}, strings.NewReader(body), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), body) {
		t.Fatal("preview output omitted stdin body with inline SVG")
	}
}

func TestPreviewRefusesOverwriteUnlessForced(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "body.html")
	output := filepath.Join(dir, "preview.html")
	if err := os.WriteFile(bodyPath, []byte("<p>New</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runPreview([]string{"--title", "Preview", "--output", output, bodyPath}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwrite error = %v", err)
	}
	got, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "existing" {
		t.Fatalf("refused overwrite changed output to %q", got)
	}

	if err := runPreview([]string{"--title", "Preview", "--output", output, "--force", bodyPath}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "<p>New</p>") || string(got) == "existing" {
		t.Fatal("--force did not replace the existing output")
	}
}

func TestPreviewRejectsForbiddenThemedMarkup(t *testing.T) {
	for _, body := range []string{
		`<!DOCTYPE html><p>Body</p>`,
		`<html><p>Body</p></html>`,
		`<head><title>Title</title></head>`,
		`<body><p>Body</p></body>`,
		`<style>p { color: red; }</style>`,
		`<script>alert(1)</script>`,
	} {
		body := body
		t.Run(body, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "preview.html")
			err := runPreview([]string{"--title", "Preview", "--output", output, "-"}, strings.NewReader(body), &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "invalid themed body") {
				t.Fatalf("runPreview() error = %v", err)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("rejected body created output; stat error = %v", statErr)
			}
		})
	}
}

func TestPreviewRequiresTitleOutputAndOneInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "title", args: []string{"--output", "preview.html", "-"}, want: "--title is required"},
		{name: "blank title", args: []string{"--title", "  ", "--output", "preview.html", "-"}, want: "--title is required"},
		{name: "output", args: []string{"--title", "Preview", "-"}, want: "--output is required"},
		{name: "blank output", args: []string{"--title", "Preview", "--output", "  ", "-"}, want: "--output is required"},
		{name: "input", args: []string{"--title", "Preview", "--output", "preview.html"}, want: "missing <body-file|-> argument"},
		{name: "extra input", args: []string{"--title", "Preview", "--output", "preview.html", "one.html", "two.html"}, want: "exactly one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runPreview(tt.args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("runPreview() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPreviewHelp(t *testing.T) {
	var stderr bytes.Buffer
	err := runPreview([]string{"--help"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runPreview(--help) error = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{
		"Usage: waymark preview --title TITLE --output FILE [--force] <body-file|->",
		"without credentials or network access",
		"-force",
		"-output",
		"-title",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("help missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestPreviewDoesNotDependOnAuthConfig(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMARK_CONFIG_DIR", configDir)
	t.Setenv("WAYMARK_URL", "https://unreachable.invalid")
	t.Setenv("WAYMARK_TOKEN", "invalid-token")

	output := filepath.Join(t.TempDir(), "preview.html")
	if err := runPreview([]string{"--title", "Offline", "--output", output, "-"}, strings.NewReader("<p>Offline</p>"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("offline preview consulted auth configuration: %v", err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}
