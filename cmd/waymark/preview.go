package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Onkarj012/Waymark/internal/render"
	"github.com/Onkarj012/Waymark/theme"
)

func cmdPreview(args []string) error {
	return runPreview(args, os.Stdin, os.Stdout, os.Stderr)
}

func runPreview(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "page title (required)")
	output := fs.String("output", "", "standalone HTML output file (required)")
	force := fs.Bool("force", false, "overwrite an existing output file")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: waymark preview --title TITLE --output FILE [--force] <body-file|->")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Render a self-contained themed HTML document without credentials or network access.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	if err := parse(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return errors.New("--title is required")
	}
	if strings.TrimSpace(*output) == "" {
		return errors.New("--output is required")
	}
	if fs.NArg() == 0 {
		return errors.New("missing <body-file|-> argument")
	}
	if fs.NArg() > 1 {
		return errors.New("expected exactly one <body-file|-> argument; put flags before the positional argument")
	}

	body, err := readPreviewInput(fs.Arg(0), stdin)
	if err != nil {
		return err
	}
	if err := render.ValidateThemedBody(body); err != nil {
		return fmt.Errorf("invalid themed body: %w", err)
	}

	document := []byte(render.Standalone(*title, body, theme.CSS))
	if err := writePreview(*output, document, *force); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "✓ Generated preview\n%s\n", *output)
	return nil
}

func readPreviewInput(path string, stdin io.Reader) (string, error) {
	if path == "-" {
		body, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read preview body from stdin: %w", err)
		}
		return string(body), nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read preview body %q: %w", path, err)
	}
	return string(body), nil
}

func writePreview(path string, document []byte, force bool) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create output directory %q: %w", parent, err)
	}

	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if !force && os.IsExist(err) {
			return fmt.Errorf("output %q already exists; pass --force to overwrite", path)
		}
		return fmt.Errorf("open output %q: %w", path, err)
	}

	removePartial := true
	defer func() {
		if removePartial {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(document); err != nil {
		_ = file.Close()
		return fmt.Errorf("write output %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close output %q: %w", path, err)
	}
	removePartial = false
	return nil
}
