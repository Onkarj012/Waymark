package main

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMutationCommandsRespectFlagTerminator(t *testing.T) {
	for _, command := range []struct {
		name   string
		method string
		path   string
		run    func([]string) error
		args   []string
	}{
		{"create", http.MethodPost, "/api/pages", cmdCreate, []string{"--title", "Review", "--", "-report.html"}},
		{"update", http.MethodPut, "/api/pages/AbCdEf123456", cmdUpdate, []string{"--", "AbCdEf123456", "-report.html"}},
	} {
		t.Run(command.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("WAYMARK_CONFIG_DIR", t.TempDir())
			t.Setenv("WAYMARK_TOKEN", "")
			t.Setenv("WAYMARK_URL", "")
			const html = "<!DOCTYPE html><html><head><meta name=\"viewport\" content=\"width=device-width\"><title>Review</title><style>body{}</style></head><body>Report</body></html>"
			if err := os.WriteFile("-report.html", []byte(html), 0o600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.Method != command.method || request.URL.Path != command.path || request.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
				}
				var payload struct {
					HTML string `json:"html"`
					Raw  bool   `json:"raw"`
				}
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.HTML != html || !payload.Raw {
					t.Errorf("literal filename source was not sent: %v", err)
				}
				writer.Header().Set("Content-Type", "application/json")
				json.NewEncoder(writer).Encode(pageResp{ID: "AbCdEf123456", Title: "Review"})
			}))
			defer server.Close()
			if err := saveConfig(config{URL: server.URL, Token: "test-token"}); err != nil {
				t.Fatal(err)
			}
			if err := command.run(command.args); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Fatalf("got %d requests, want one", requests.Load())
			}
		})
	}
}

func TestParseDistinguishesTerminatorFromFlagValue(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{"boolean flag", []string{"--json", "--", "-report.html"}, []string{"-report.html"}, false},
		{"flag-like value", []string{"--title", "--title", "--", "-report.html"}, []string{"-report.html"}, false},
		{"equals value", []string{"--title=--", "--", "--"}, []string{"--"}, false},
		{"terminator value then terminator", []string{"--title", "--", "--", "-report.html"}, []string{"-report.html"}, false},
		{"terminator value is not terminator", []string{"--title", "--", "file.html", "--ttl", "7"}, nil, true},
		{"unconsumed terminator", []string{"file.html", "--", "--ttl", "7"}, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			flags.String("title", "", "")
			flags.Bool("json", false, "")
			flags.Int("ttl", 0, "")
			err := parse(flags, test.args)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "flags before positional arguments") {
					t.Fatalf("got %v, want argument ordering error", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(flags.Args(), test.want) {
				t.Fatalf("got args %q, error %v; want %q", flags.Args(), err, test.want)
			}
		})
	}
}
