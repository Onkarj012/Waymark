package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Onkarj012/Waymark/internal/store"
)

func documentWithBytes(size int, pattern string) string {
	remaining := size - len(validRawHTML("Report", ""))
	inner := strings.Repeat(pattern, remaining/len(pattern)) + strings.Repeat("a", remaining%len(pattern))
	return validRawHTML("Report", inner)
}

func TestPageWriteDocumentByteLimits(t *testing.T) {
	for _, pattern := range []string{"a", "é", "\"\\", "\x01"} {
		for _, size := range []int{maxDocumentBytes, maxDocumentBytes + 1} {
			t.Run(fmt.Sprintf("pattern=%q/size=%d", pattern, size), func(t *testing.T) {
				h, st, _, token := sourceTestServer(t)
				html := documentWithBytes(size, pattern)
				body := `{"title":"Report","slug":"report","raw":true,"html":` + mustJSON(t, html) + `}`
				created := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(body))
				if size > maxDocumentBytes {
					if created.Code != 413 || apiError(t, created) != "html exceeds maximum of 1000000 UTF-8 bytes" {
						t.Fatalf("create status = %d: %s", created.Code, created.Body.String())
					}
					pages, err := st.List(0)
					if err != nil || len(pages) != 0 {
						t.Fatalf("rejected create persisted pages: %#v, error %v", pages, err)
					}
				} else {
					if created.Code != 201 {
						t.Fatalf("create status = %d: %s", created.Code, created.Body.String())
					}
					var meta pageResp
					if err := json.Unmarshal(created.Body.Bytes(), &meta); err != nil {
						t.Fatal(err)
					}
					saved, err := st.GetSource(meta.ID)
					if err != nil || saved.HTML != html {
						t.Fatal("create changed document bytes")
					}
				}
				now := time.Now().UTC()
				legacy := &store.Page{ID: "legacy", Title: "Original", HTML: "<p>legacy</p>", CreatedAt: now, UpdatedAt: now}
				if err := st.Create(legacy); err != nil {
					t.Fatal(err)
				}
				before, err := st.GetSource(legacy.ID)
				if err != nil {
					t.Fatal(err)
				}
				updated := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/legacy", token, strings.NewReader(body))
				after, err := st.GetSource(legacy.ID)
				if err != nil {
					t.Fatal(err)
				}
				if size > maxDocumentBytes {
					if updated.Code != 413 || apiError(t, updated) != "html exceeds maximum of 1000000 UTF-8 bytes" || !reflect.DeepEqual(before, after) {
						t.Fatal("oversized replacement did not fail atomically")
					}
				} else if updated.Code != 200 || after.HTML != html || !after.Raw {
					t.Fatalf("exact-limit replacement status = %d", updated.Code)
				}
			})
		}
	}
}

func TestPageWriteMetadataByteLimits(t *testing.T) {
	for _, field := range []string{"title", "slug"} {
		for _, size := range []int{maxMetadataBytes, maxMetadataBytes + 1} {
			t.Run(fmt.Sprintf("%s/%d", field, size), func(t *testing.T) {
				h, st, _, token := sourceTestServer(t)
				value := strings.Repeat("é", size/2) + strings.Repeat("a", size%2)
				body := map[string]any{"title": "Report", "raw": true, "html": validRawHTML("Report", "<p>Hi</p>"), field: "  " + value + "  "}
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				created := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(string(encoded)))
				want := 201
				if size > maxMetadataBytes {
					want = 413
				}
				if created.Code != want {
					t.Fatalf("create status = %d, want %d: %s", created.Code, want, created.Body.String())
				}
				now := time.Now().UTC()
				if err := st.Create(&store.Page{ID: "metadata", Title: "Original", HTML: "<p>Hi</p>", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
				before, err := st.GetSource("metadata")
				if err != nil {
					t.Fatal(err)
				}
				patch := `{"` + field + `":` + mustJSON(t, "  "+value+"  ") + `}`
				updated := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/metadata", token, strings.NewReader(patch))
				want = 200
				if size > maxMetadataBytes {
					want = 413
				}
				if updated.Code != want {
					t.Fatalf("update status = %d, want %d", updated.Code, want)
				}
				after, err := st.GetSource("metadata")
				if err != nil {
					t.Fatal(err)
				}
				if size > maxMetadataBytes {
					message := fmt.Sprintf("%s exceeds maximum of 1024 UTF-8 bytes", field)
					if apiError(t, created) != message || apiError(t, updated) != message || !reflect.DeepEqual(before, after) {
						t.Fatal("oversized metadata did not fail atomically")
					}
				} else if field == "title" && after.Title != value || field == "slug" && after.Slug != value {
					t.Fatal("metadata was not trimmed and preserved")
				}
			})
		}
	}
}

func TestPageWritesRejectInvalidUnicodeWithoutMutation(t *testing.T) {
	h, st, _, token := sourceTestServer(t)
	now := time.Now().UTC()
	if err := st.Create(&store.Page{ID: "legacy", Title: "Original", HTML: "<p>legacy</p>", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	before, err := st.GetSource("legacy")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"\xff", `\ud800`, `\udc00`, `\ud800x`, `\ud800\ud800`} {
		for _, field := range []string{"title", "slug", "html"} {
			body := `{"title":"Report","slug":"report","raw":true,"html":` + mustJSON(t, validRawHTML("Report", "UNICODE")) + `}`
			if field == "html" {
				body = strings.Replace(body, "UNICODE", invalid, 1)
			} else {
				body = strings.Replace(body, `"`+field+`":"`+map[string]string{"title": "Report", "slug": "report"}[field]+`"`, `"`+field+`":"`+invalid+`"`, 1)
			}
			for _, method := range []string{http.MethodPost, http.MethodPut} {
				path := "/api/pages"
				if method == http.MethodPut {
					path += "/legacy"
				}
				response := authenticatedRequest(t, h, method, "control.localhost", path, token, strings.NewReader(body))
				if response.Code != 400 || !strings.Contains(apiError(t, response), "invalid UTF-8 or unpaired Unicode surrogate") {
					t.Fatalf("%s field %s unicode %q status = %d: %s", method, field, invalid, response.Code, response.Body.String())
				}
			}
		}
	}
	after, err := st.GetSource("legacy")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected Unicode writes changed legacy page")
	}
	pages, err := st.List(0)
	if err != nil || len(pages) != 1 {
		t.Fatal("rejected Unicode creates persisted rows")
	}
	for _, valid := range []string{`\ud83c\udf0d`, `\\ud800`, `\ufffd`, "é e\u0301"} {
		body := `{"title":"Report","raw":true,"html":` + mustJSON(t, validRawHTML("Report", "UNICODE")) + `}`
		body = strings.Replace(body, "UNICODE", valid, 1)
		response := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(body))
		if response.Code != 201 {
			t.Fatalf("valid Unicode %q status = %d: %s", valid, response.Code, response.Body.String())
		}
	}
}

func TestOversizedLegacyRemainsReadableMetadataUpdatableAndExportable(t *testing.T) {
	h, st, _, token := sourceTestServer(t)
	now := time.Now().UTC()
	legacy := &store.Page{ID: "oversized-legacy", Title: strings.Repeat("t", maxMetadataBytes+1), Slug: strings.Repeat("s", maxMetadataBytes+1),
		HTML: documentWithBytes(maxDocumentBytes+1, "é"), CreatedAt: now, UpdatedAt: now}
	if err := st.Create(legacy); err != nil {
		t.Fatal(err)
	}
	public := doRequest(t, h, http.MethodGet, "pages.localhost", "/p/"+legacy.ID, nil, nil, "")
	public.Body.Close()
	if public.StatusCode != 200 {
		t.Fatalf("public read status = %d", public.StatusCode)
	}
	updated := authenticatedRequest(t, h, http.MethodPut, "control.localhost", "/api/pages/"+legacy.ID, token, strings.NewReader(`{"ttl_days":0}`))
	if updated.Code != 200 {
		t.Fatalf("metadata-only status = %d: %s", updated.Code, updated.Body.String())
	}
	response := sourceRequest(h, "control.localhost", legacy.ID, token)
	var exported sourceResp
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &exported) != nil || exported.HTML != legacy.HTML || exported.Raw || exported.Title != legacy.Title || exported.Slug != legacy.Slug {
		t.Fatal("oversized legacy export changed source or metadata")
	}
}

func TestPageWriteTransportLimitRemainsIndependent(t *testing.T) {
	h, _, _, token := sourceTestServer(t)
	body := strings.Repeat(" ", maxBodyBytes+1)
	response := authenticatedRequest(t, h, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(body))
	if response.Code != 413 || !strings.Contains(apiError(t, response), "8388608") {
		t.Fatalf("transport limit status = %d: %s", response.Code, response.Body.String())
	}
}

func TestPageWriteRejectsTrailingJSONBeforeStorage(t *testing.T) {
	handler, persistence, _, token := sourceTestServer(t)
	request := `{"title":"Report","raw":true,"html":` + mustJSON(t, validRawHTML("Report", "content")) + `}`
	for _, suffix := range []string{` {"raw":false}`, " trailing text"} {
		response := authenticatedRequest(t, handler, http.MethodPost, "control.localhost", "/api/pages", token, strings.NewReader(request+suffix))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("trailing input accepted: %d", response.Code)
		}
	}
	for _, body := range []string{"null", "[]", ""} {
		response := authenticatedRequest(t, handler, http.MethodPut, "control.localhost", "/api/pages/missing", token, strings.NewReader(body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("non-object request accepted: %q returned %d", body, response.Code)
		}
	}
	pages, err := persistence.List(0)
	if err != nil || len(pages) != 0 {
		t.Fatal("invalid request persisted a page")
	}
}
