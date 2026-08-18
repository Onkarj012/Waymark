package render

import (
	"strings"
	"testing"
)

func TestHostedPreservesWrapperBehavior(t *testing.T) {
	got := Hosted("Status <check>", "<section><p>Hello</p></section>")
	for _, want := range []string{
		`<html lang="en" data-theme="dark">`,
		`<title>Status &lt;check&gt;</title>`,
		`<link rel="stylesheet" href="/theme.css">`,
		`<section><p>Hello</p></section>`,
		`<div class="reading-progress" aria-hidden="true"></div>`,
		`.section-nav a[href^='#']`,
		`<footer class="waymark-credit">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Hosted output missing %q", want)
		}
	}
	if strings.Contains(got, "prefers-color-scheme") || strings.Contains(got, "matchMedia") {
		t.Fatal("Hosted output still uses OS theme preference")
	}
}

func TestHostedThemeSelectionIsDarkFirstAndHonorsStoredLight(t *testing.T) {
	got := Hosted("Title", "<p>Body</p>")
	initStart := strings.Index(got, `<script>(function(){try{var t=localStorage.getItem("waymark-theme")`)
	bodyStart := strings.Index(got, "<body>")
	if initStart < 0 || initStart > bodyStart {
		t.Fatal("theme initialization must run in <head> before <body>")
	}
	if !strings.Contains(got, `if(t==="light")document.documentElement.removeAttribute("data-theme")`) {
		t.Fatal("stored light choice does not remove the dark default")
	}
	if !strings.Contains(got, `else if(t==="dark")document.documentElement.setAttribute("data-theme","dark")`) {
		t.Fatal("stored dark choice does not retain the dark theme")
	}
}

func TestStandaloneEmbedsCallerCSSCleanly(t *testing.T) {
	got := Standalone("Preview", "<p>Body</p>", "html { color: red; }")
	if strings.Contains(got, `href="/theme.css"`) {
		t.Fatal("standalone output linked hosted stylesheet")
	}
	if !strings.Contains(got, "<style>\nhtml { color: red; }\n</style>\n") {
		t.Fatalf("standalone stylesheet was not embedded cleanly: %s", got)
	}
}

func TestStandaloneEscapesCaseInsensitiveStyleEndTagInCallerCSS(t *testing.T) {
	got := Standalone("Preview", "<p>Body</p>", `.x::after { content: "</StYlE><script>bad</script>"; }`)
	if strings.Count(strings.ToLower(got), "</style>") != 1 {
		t.Fatal("caller CSS added an extra style end tag to standalone output")
	}
	if !strings.Contains(got, `\3c /StYlE`) {
		t.Fatalf("caller CSS was not safely escaped: %s", got)
	}
}

func TestValidateThemedBody(t *testing.T) {
	for _, content := range []string{
		`<article><header><h1>Plan</h1></header><section><p>Body</p></section></article>`,
		`<figure><svg viewBox="0 0 10 10"><path d="M0 0L10 10"></path></svg></figure>`,
		`<p data-example="<script>">Text mentioning &lt;html&gt;</p>`,
		`<!-- <script> is discussed here --><p>Safe</p>`,
		`<!-- unfinished comment`,
	} {
		if err := ValidateThemedBody(content); err != nil {
			t.Errorf("ValidateThemedBody(%q) = %v", content, err)
		}
	}

	for _, content := range []string{
		`<!DOCTYPE html><p>Body</p>`,
		`<HTML><body>Body</body></HTML>`,
		`<head><title>x</title></head>`,
		`<body>Body</body>`,
		`<style>.x{}</style>`,
		`<script>alert(1)</script>`,
		`<link rel="stylesheet" href="evil.css">`,
		`<base href="https://evil.example/">`,
		`<meta http-equiv="refresh" content="0;url=https://evil.example/">`,
		`<title>Title</title>`,
	} {
		if err := ValidateThemedBody(content); err == nil {
			t.Errorf("ValidateThemedBody(%q) unexpectedly succeeded", content)
		}
	}
}
