// Command waymark is the Waymark client. It uploads an HTML file to the
// server and prints back a shareable link, and manages existing pages.
//
// Configuration (environment variables, overridable with flags):
//
//	WAYMARK_URL    server base URL, e.g. https://pages.example.com
//	WAYMARK_TOKEN  device token override
//
// Usage:
//
//	waymark login   [--server URL]
//	waymark create  --title "Title" [--slug s] [--raw] [--ttl N] <file|->
//	waymark list    [--limit N] [--json]
//	waymark get     [--json] <id>
//	waymark update  [--title T] [--slug s] [--raw] [--ttl N] <id> [<file|->]
//	waymark delete  <id>
//	waymark version
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "login":
		err = cmdLogin(os.Args[2:])
	case "logout":
		err = cmdLogout(os.Args[2:])
	case "status", "whoami":
		err = cmdStatus(os.Args[2:])
	case "create":
		err = cmdCreate(os.Args[2:])
	case "list", "ls":
		err = cmdList(os.Args[2:])
	case "get":
		err = cmdGet(os.Args[2:])
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "delete", "rm":
		err = cmdDelete(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("waymark " + version)
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `waymark — publish HTML pages to Waymark

Setup:
  login   [--server URL] [--device-name NAME] [--read-only]
  logout                            revoke device token and forget credentials
  status                            verify authentication and show token metadata

Commands:
  create  --title "Title" [--slug s] [--raw] [--ttl N] <file|->   publish a page
  list    [--limit N] [--json]                                    list pages
  get     [--json] <id>                                           show page metadata
  update  [--title T] [--slug s] [--raw] [--ttl N] <id> [<file>]  replace a page
  delete  <id>                                                    delete a page
  version                                                         print version

Auth:
  Normal login prints a browser verification URL and waits for owner approval.

Notes:
  • By default the file is body content wrapped in the house theme. Pass --raw
    to serve a complete HTML document verbatim (no theme).
  • --ttl N auto-deletes the page after N days (0 = never).
  • Put flags before positional arguments.
`)
}

// --- commands --------------------------------------------------------------

func cmdCreate(args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	title := fs.String("title", "", "page title (required)")
	slug := fs.String("slug", "", "optional human label")
	raw := fs.Bool("raw", false, "serve as a complete HTML document (no house theme)")
	ttl := fs.Int("ttl", 0, "auto-delete after N days (0 = never)")
	jsonOut := fs.Bool("json", false, "print the raw JSON response")
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}

	file := fs.Arg(0)
	if file == "" {
		return errors.New("missing <file> argument")
	}
	if strings.TrimSpace(*title) == "" {
		return errors.New("--title is required")
	}
	body, err := readInput(file)
	if err != nil {
		return err
	}
	c, err := newClient(server)
	if err != nil {
		return err
	}

	req := map[string]any{"title": *title, "slug": *slug, "html": body, "raw": *raw, "ttl_days": *ttl}
	var resp pageResp
	if err := c.do(http.MethodPost, "/api/pages", req, &resp); err != nil {
		return err
	}
	return report(resp, *jsonOut, "Published")
}

func cmdUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	title := fs.String("title", "", "new title")
	slug := fs.String("slug", "", "new slug")
	raw := fs.Bool("raw", false, "serve as a complete HTML document (no house theme)")
	ttl := fs.Int("ttl", 0, "auto-delete after N days (0 = never)")
	jsonOut := fs.Bool("json", false, "print the raw JSON response")
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}

	id := fs.Arg(0)
	if id == "" {
		return errors.New("missing <id> argument")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	req := map[string]any{}
	if file := fs.Arg(1); file != "" {
		body, err := readInput(file)
		if err != nil {
			return err
		}
		req["html"] = body
	}
	if set["title"] {
		req["title"] = *title
	}
	if set["slug"] {
		req["slug"] = *slug
	}
	if set["raw"] {
		req["raw"] = *raw
	}
	if set["ttl"] {
		req["ttl_days"] = *ttl
	}
	if len(req) == 0 {
		return errors.New("nothing to update: pass a <file> and/or --title/--slug/--raw/--ttl")
	}

	c, err := newClient(server)
	if err != nil {
		return err
	}
	var resp pageResp
	if err := c.do(http.MethodPut, "/api/pages/"+id, req, &resp); err != nil {
		return err
	}
	return report(resp, *jsonOut, "Updated")
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "max pages to show (0 = all)")
	jsonOut := fs.Bool("json", false, "print the raw JSON response")
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	c, err := newClient(server)
	if err != nil {
		return err
	}

	var out struct {
		Pages []pageResp `json:"pages"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/pages?limit=%d", *limit), nil, &out); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(out.Pages)
	}
	if len(out.Pages) == 0 {
		fmt.Println("No pages yet.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tCREATED\tEXPIRES\tURL")
	for _, p := range out.Pages {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			p.ID, truncate(p.Title, 32), p.CreatedAt.Local().Format("2006-01-02 15:04"), expiryStr(p.ExpiresAt), p.URL)
	}
	return tw.Flush()
}

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print the raw JSON response")
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return errors.New("missing <id> argument")
	}
	if fs.NArg() > 1 {
		return errors.New("unexpected argument after <id>; put flags before positional arguments")
	}
	c, err := newClient(server)
	if err != nil {
		return err
	}
	var resp pageResp
	if err := c.do(http.MethodGet, "/api/pages/"+id, nil, &resp); err != nil {
		return err
	}
	return report(resp, *jsonOut, "")
}

func cmdDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		return errors.New("missing <id> argument")
	}
	c, err := newClient(server)
	if err != nil {
		return err
	}
	if err := c.do(http.MethodDelete, "/api/pages/"+id, nil, nil); err != nil {
		return err
	}
	fmt.Printf("✓ Deleted %s\n", id)
	return nil
}

// --- setup commands --------------------------------------------------------

func cmdLogout(args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	if err := parse(fs, args); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	var revokeErr error
	if cfg.URL != "" && cfg.Token != "" {
		server, normalizeErr := normalizeServerURL(cfg.URL)
		if normalizeErr != nil {
			revokeErr = normalizeErr
		} else {
			c := &client{base: server, token: cfg.Token}
			revokeErr = c.do(http.MethodPost, "/api/auth/revoke", map[string]any{}, nil)
		}
	}
	if err := os.Remove(configPath()); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Already logged out.")
			return nil
		}
		return err
	}
	fmt.Println("✓ Logged out (removed " + configPath() + ")")
	if revokeErr != nil {
		fmt.Fprintf(os.Stderr, "warning: local credentials were removed, but server-side revocation could not be confirmed: %v\n", revokeErr)
	}
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	server := commonFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	srv, token, srvSrc, tokenSrc, err := resolve(*server)
	if err != nil {
		return err
	}

	fmt.Printf("Config file: %s\n", configPath())
	if srv == "" {
		fmt.Println("Server:      (not set) — run `waymark login`")
	} else {
		fmt.Printf("Server:      %s (from %s)\n", srv, srvSrc)
	}
	if token == "" {
		fmt.Println("Credential:  (not set) — run `waymark login`")
	} else {
		fmt.Printf("Credential:  device token (from %s)\n", tokenSrc)
	}

	if srv == "" || token == "" {
		return errors.New("authentication is not configured")
	}
	srv, err = normalizeServerURL(srv)
	if err != nil {
		return err
	}

	c := &client{base: srv, token: token}
	info, status, pingErr := c.authInfo()
	switch {
	case pingErr != nil:
		fmt.Printf("Auth:        could not reach server (%v)\n", pingErr)
		return errors.New("server is unreachable")
	case status == http.StatusOK:
		fmt.Println("Auth:        ✓ authenticated")
		credentialType := strings.ReplaceAll(info.CredentialType, "_", " ")
		if credentialType == "" {
			credentialType = "device token"
		}
		fmt.Printf("Type:        %s\n", credentialType)
		if info.Label != "" {
			fmt.Printf("Label:       %s\n", info.Label)
		}
		if len(info.Scopes) > 0 {
			fmt.Printf("Scopes:      %s\n", strings.Join(info.Scopes, ", "))
		}
		if info.ExpiresAt != nil {
			fmt.Printf("Expires:     %s\n", info.ExpiresAt.Local().Format(time.RFC3339))
		}
	case status == http.StatusUnauthorized:
		fmt.Println("Auth:        ✗ device token rejected, revoked, or expired")
		return errors.New("device token authentication failed")
	default:
		fmt.Printf("Auth:        unexpected HTTP %d\n", status)
		return fmt.Errorf("authentication check returned HTTP %d", status)
	}
	return nil
}

// --- client ----------------------------------------------------------------

type pageResp struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Title     string     `json:"title"`
	Slug      string     `json:"slug"`
	Raw       bool       `json:"raw"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Size      int        `json:"size"`
}

type client struct {
	base  string
	token string
}

func newClient(serverFlag *string) (*client, error) {
	server, token, _, _, err := resolve(*serverFlag)
	if err != nil {
		return nil, err
	}
	if server == "" {
		return nil, errors.New("no server configured — run `waymark login`")
	}
	server, err = normalizeServerURL(server)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errors.New("not logged in — run `waymark login`")
	}
	return &client{base: server, token: token}, nil
}

type authInfoResponse struct {
	OK             bool       `json:"ok"`
	CredentialType string     `json:"credential_type"`
	Scopes         []string   `json:"scopes"`
	Label          string     `json:"label"`
	ExpiresAt      *time.Time `json:"expires_at"`
}

func (c *client) authInfo() (authInfoResponse, int, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/auth", nil)
	if err != nil {
		return authInfoResponse{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return authInfoResponse{}, 0, err
	}
	defer resp.Body.Close()
	var info authInfoResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return info, resp.StatusCode, err
		}
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return info, resp.StatusCode, nil
}

func (c *client) do(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("server %d: %s", resp.StatusCode, e.Error)
		}
		return fmt.Errorf("server %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// --- output ----------------------------------------------------------------

// report prints a page result. When jsonOut is true it prints the raw JSON;
// otherwise it prints a human summary with the URL on its own line. verb, when
// non-empty, prefixes a "✓ <verb>" headline (used by create/update).
func report(p pageResp, jsonOut bool, verb string) error {
	if jsonOut {
		return printJSON(p)
	}
	if verb != "" {
		fmt.Printf("✓ %s %q\n", verb, p.Title)
	} else {
		fmt.Printf("%s\n", p.Title)
	}
	fmt.Println(p.URL)

	meta := "  id " + p.ID
	if p.Raw {
		meta += " · raw"
	}
	if p.Size > 0 {
		meta += fmt.Sprintf(" · %s", humanSize(p.Size))
	}
	if p.ExpiresAt != nil {
		meta += " · expires " + p.ExpiresAt.Local().Format("2006-01-02 15:04")
	}
	fmt.Println(meta)
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- helpers ---------------------------------------------------------------

func commonFlags(fs *flag.FlagSet) *string {
	return fs.String("server", "", "server base URL (overrides saved login)")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parse(fs *flag.FlagSet, args []string) error {
	return fs.Parse(args)
}

func normalizeServerURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "", errors.New("server URL is required")
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("server URL must be an absolute http:// or https:// URL")
	}
	if u.User != nil {
		return "", errors.New("server URL must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("server URL must not contain a query or fragment")
	}
	if u.Path != "" {
		return "", errors.New("server URL must not contain a path")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return "", errors.New("refusing to send credentials over plain HTTP; use HTTPS (HTTP is allowed for loopback development)")
	}
	return value, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// readInput reads a file, or stdin when path is "-".
func readInput(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func expiryStr(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
