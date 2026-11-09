package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type sourceEnvelope struct {
	Schema        string  `json:"schema"`
	Version       int     `json:"version"`
	ControlOrigin string  `json:"control_origin"`
	PublicOrigin  string  `json:"public_origin"`
	ID            string  `json:"id"`
	URL           string  `json:"url"`
	Title         string  `json:"title"`
	Slug          string  `json:"slug"`
	Raw           bool    `json:"raw"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ExpiresAt     *string `json:"expires_at"`
	HTML          string  `json:"html"`
	UTF8Bytes     int     `json:"utf8_bytes"`
	SHA256        string  `json:"sha256"`
}

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{12}$`)

func cmdPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	output := fs.String("output", "", "new destination HTML file (required)")
	legacy := fs.Bool("legacy-fragment", false, "explicitly export a non-publishable legacy fragment")
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 || !sourceIDPattern.MatchString(fs.Arg(0)) {
		return errors.New("pull requires exactly one 12-character page ID")
	}
	if *output == "" || *output == "-" {
		return errors.New("pull requires a named --output file; source is never printed to the terminal")
	}
	root, name, err := sourceDestination(*output)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, target := range []string{name, name + ".source.json"} {
		if _, err := root.Lstat(target); !os.IsNotExist(err) {
			return fmt.Errorf("destination %q is not absent; choose a new output filename", target)
		}
	}
	envelope, err := fetchSource(*server, fs.Arg(0))
	if err != nil {
		return err
	}
	if !envelope.Raw && !*legacy {
		return errors.New("page is a legacy fragment; use --legacy-fragment to export it without converting or publishing it")
	}
	if err := installSource(root, name, envelope); err != nil {
		return err
	}
	fmt.Printf("Saved source to %q and metadata to %q. No remote changes.\n", *output, *output+".source.json")
	return nil
}

func fetchSource(serverFlag, id string) (sourceEnvelope, error) {
	var envelope sourceEnvelope
	server, token, _, _, err := resolve(serverFlag)
	if err != nil {
		return envelope, err
	}
	server, err = normalizeServerURL(server)
	if err != nil {
		return envelope, err
	}
	if token == "" {
		return envelope, errors.New("not logged in; run `waymark login`")
	}
	discovery, err := discover(server)
	if err != nil {
		return envelope, fmt.Errorf("discover instance: %w", err)
	}
	control, err := normalizeServerURL(discovery.ControlURL)
	if err != nil {
		return envelope, fmt.Errorf("unsafe discovered control origin: %w", err)
	}
	public, err := normalizeServerURL(discovery.ContentURL)
	if err != nil || control == public {
		return envelope, errors.New("discovery requires distinct safe control and public origins")
	}
	if server != control && server != public {
		return envelope, errors.New("discovery does not describe the requested instance")
	}
	if err := checkSavedTokenOrigin(control); err != nil {
		return envelope, err
	}
	request, err := http.NewRequest(http.MethodGet, control+"/api/pages/"+id+"/source", nil)
	if err != nil {
		return envelope, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := safeHTTPClient(30 * time.Second).Do(request)
	if err != nil {
		return envelope, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusNotFound {
			return envelope, errors.New("page is missing/expired or this deployment does not support source export; upgrade both backends before retrying")
		}
		return envelope, fmt.Errorf("source export returned HTTP %d; no files written", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return envelope, errors.New("source export did not return application/json")
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return envelope, err
	}
	if !utf8.Valid(data) {
		return envelope, errors.New("source response is not valid UTF-8")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return envelope, fmt.Errorf("invalid source response: %w", err)
	}
	for _, field := range []string{"schema", "version", "control_origin", "public_origin", "id", "url", "title", "slug", "raw", "created_at", "updated_at", "expires_at", "html", "utf8_bytes", "sha256"} {
		value, ok := fields[field]
		if !ok || (field != "expires_at" && string(value) == "null") {
			return envelope, fmt.Errorf("incomplete source response: missing %s", field)
		}
		if !validSourceUnicode(value) {
			return envelope, errors.New("source response contains an unpaired Unicode surrogate")
		}
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return envelope, fmt.Errorf("invalid source response: %w", err)
	}
	if envelope.Schema != "waymark-source" || envelope.Version != 1 {
		return envelope, errors.New("unsupported source schema/version; upgrade the CLI or deployment")
	}
	if envelope.ID != id || envelope.ControlOrigin != control || envelope.PublicOrigin != public || envelope.URL != public+"/p/"+id {
		return envelope, errors.New("source identity or origin does not match the requested page")
	}
	for _, timestamp := range []string{envelope.CreatedAt, envelope.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return envelope, errors.New("source contains an invalid timestamp")
		}
	}
	if envelope.ExpiresAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *envelope.ExpiresAt); err != nil {
			return envelope, errors.New("source contains an invalid expiry")
		}
	}
	digest := sha256.Sum256([]byte(envelope.HTML))
	if !utf8.ValidString(envelope.HTML) || len(envelope.HTML) != envelope.UTF8Bytes || hex.EncodeToString(digest[:]) != envelope.SHA256 {
		return envelope, errors.New("source byte count or SHA-256 checksum does not match")
	}
	return envelope, nil
}

func validSourceUnicode(value []byte) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			continue
		}
		index++
		if index >= len(value) || value[index] != 'u' {
			continue
		}
		if index+4 >= len(value) {
			return false
		}
		code, err := strconv.ParseUint(string(value[index+1:index+5]), 16, 16)
		if err != nil {
			return false
		}
		index += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if index+6 >= len(value) || string(value[index+1:index+3]) != `\u` {
			return false
		}
		low, err := strconv.ParseUint(string(value[index+3:index+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		index += 6
	}
	return true
}

func sourceDestination(output string) (*os.Root, string, error) {
	for _, component := range strings.Split(filepath.ToSlash(output), "/") {
		if component == ".." {
			return nil, "", errors.New("output must not contain parent traversal")
		}
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return nil, "", err
	}
	base := filepath.VolumeName(absolute) + string(filepath.Separator)
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	directory, name := filepath.Split(absolute)
	for _, component := range strings.Split(strings.TrimPrefix(directory, base), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		info, err := root.Lstat(component)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", errors.New("output directory must exist and contain no symlinks")
		}
		next, err := root.OpenRoot(component)
		root.Close()
		if err != nil {
			return nil, "", err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, "", errors.New("output directory changed while opening it")
		}
		root = next
	}
	if name == "" || name == "." {
		root.Close()
		return nil, "", errors.New("output must name a file")
	}
	return root, name, nil
}

func stageSource(root *os.Root, data []byte) (string, os.FileInfo, error) {
	random, err := randomDeviceSecret()
	if err != nil {
		return "", nil, err
	}
	name := ".waymark-source-" + random
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	cleanup := func(err error) (string, os.FileInfo, error) {
		file.Close()
		root.Remove(name)
		return "", nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return cleanup(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return cleanup(errors.New("filesystem cannot protect source files with mode 0600"))
	}
	if _, err := file.Write(data); err != nil {
		return cleanup(err)
	}
	if err := file.Sync(); err != nil {
		return cleanup(err)
	}
	if err := file.Close(); err != nil {
		return cleanup(err)
	}
	return name, info, nil
}

func installSource(root *os.Root, name string, envelope sourceEnvelope) error {
	metadata, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	var sidecar map[string]any
	if err := json.Unmarshal(metadata, &sidecar); err != nil {
		return err
	}
	delete(sidecar, "html")
	sidecar["source_path"] = name
	sidecar["publishable"] = envelope.Raw
	metadata, err = json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return err
	}
	staged, sourceInfo, err := stageSource(root, []byte(envelope.HTML))
	if err != nil {
		return err
	}
	defer root.Remove(staged)
	stagedMetadata, metadataInfo, err := stageSource(root, append(metadata, '\n'))
	if err != nil {
		return err
	}
	defer root.Remove(stagedMetadata)
	if err := root.Link(staged, name); err != nil {
		return fmt.Errorf("cannot install source without overwriting: %w", err)
	}
	installed, err := root.Lstat(name)
	if err != nil || !os.SameFile(installed, sourceInfo) || !installed.Mode().IsRegular() {
		return errors.New("source installation changed unexpectedly; no trusted sidecar installed, inspect the output manually")
	}
	if err := syncSourceDirectory(root); err != nil {
		return fmt.Errorf("source installed but directory durability could not be confirmed; no trusted sidecar installed: %w", err)
	}
	if err := root.Link(stagedMetadata, name+".source.json"); err != nil {
		return fmt.Errorf("source saved as %q but metadata was not installed; inspect orphan source and choose a new output: %w", name, err)
	}
	installed, err = root.Lstat(name + ".source.json")
	if err != nil || !os.SameFile(installed, metadataInfo) || !installed.Mode().IsRegular() {
		return errors.New("sidecar installation changed unexpectedly; inspect both output files manually")
	}
	if err := syncSourceDirectory(root); err != nil {
		return fmt.Errorf("source and sidecar installed but directory durability could not be confirmed: %w", err)
	}
	return nil
}

func syncSourceDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
