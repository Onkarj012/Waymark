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
		`<link rel="icon" href="/favicon.svg" type="image/svg+xml">`,
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
	if !strings.Contains(got, `href="data:image/svg+xml;base64,`) {
		t.Fatal("standalone output missing embedded favicon")
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

func validRawDocument(inner string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Title</title>
<style>body { margin: 0; }</style>
</head>
<body>
` + inner + `
</body>
</html>
`
}

func TestValidateRawDocumentAcceptsCompleteSelfContainedHTML(t *testing.T) {
	for _, inner := range []string{
		`<p>Hello</p>`,
		`<article class="plan"><section id="one"><h1>Plan</h1></section></article>`,
		`<figure><svg viewBox="0 0 10 10"><path d="M0 0L10 10"></path></svg></figure>`,
		`<!-- note --><p>Safe</p>`,
	} {
		if err := ValidateRawDocument(validRawDocument(inner)); err != nil {
			t.Errorf("ValidateRawDocument(valid inner %q) = %v", inner, err)
		}
	}
}

func TestValidateRawDocumentRequiresDocumentStructure(t *testing.T) {
	full := validRawDocument("<p>Hi</p>")
	cases := []struct {
		name    string
		html    string
		wantErr string
	}{
		{"doctype", strings.Replace(full, "<!DOCTYPE html>", "", 1), "doctype"},
		{"html", strings.NewReplacer("<html lang=\"en\">", "", "</html>", "").Replace(full), "<html>"},
		{"head", strings.NewReplacer("<head>", "", "</head>\n", "").Replace(full), "<head>"},
		{"viewport", strings.Replace(full, `<meta name="viewport" content="width=device-width, initial-scale=1">`, "", 1), "viewport"},
		{"title", strings.Replace(full, "<title>Title</title>", "", 1), "<title>"},
		{"style", strings.Replace(full, "<style>body { margin: 0; }</style>", "", 1), "<style>"},
		{"body", strings.NewReplacer("<body>\n", "", "\n</body>", "").Replace(full), "<body>"},
		{"style in body", `<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width"><title>T</title></head><body><style>x{}</style></body></html>`, "<style>"},
		{"house theme link", strings.Replace(full, "<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="/theme.css">`, 1), "house theme"},
		{"relative house theme", strings.Replace(full, "<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="theme.css">`, 1), "house theme"},
		{"absolute house theme", strings.Replace(full, "<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="https://pages.example/theme.css">`, 1), "house theme"},
		{"plan profile", validRawDocument(`<article class="plan" data-plan-profile="systems"><p>x</p></article>`), "data-plan-profile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRawDocument(tc.html)
			if err == nil {
				t.Fatalf("ValidateRawDocument(%s) succeeded", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateRawDocumentIgnoresPlanProfileInsideStyle(t *testing.T) {
	html := validRawDocument("<p>Hi</p>")
	html = strings.Replace(html, "body { margin: 0; }", `article.plan[data-plan-profile] { color: red; }`, 1)
	if err := ValidateRawDocument(html); err != nil {
		t.Fatalf("profile selector in CSS rejected: %v", err)
	}
}
