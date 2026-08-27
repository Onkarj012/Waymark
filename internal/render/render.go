// Package render builds Waymark documents and validates publisher HTML.
//
// New pages are complete raw documents. Hosted() still wraps legacy themed
// body HTML for stored pages that predate the raw-only write contract.
package render

import (
	"fmt"
	"html"
	"strings"
)

// Hosted wraps body content in the hosted Waymark document, linking the house
// stylesheet at /theme.css.
func Hosted(title, content string) string {
	return themedDocument(title, content, `<link rel="stylesheet" href="/theme.css">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
`)
}

func themedDocument(title, content, stylesheet string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\" data-theme=\"dark\">\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title>\n")
	b.WriteString(stylesheet)
	b.WriteString(themeInitScript)
	b.WriteString("</head>\n<body>\n")
	b.WriteString(readingProgressBar)
	b.WriteString(themeToggleButton)
	b.WriteString("<main class=\"page\">\n")
	b.WriteString(content)
	b.WriteString("\n<footer class=\"waymark-credit\">\n")
	b.WriteString("<a href=\"https://github.com/Onkarj012/Waymark\" target=\"_blank\" rel=\"noopener noreferrer\">generated on Waymark</a>\n")
	b.WriteString("</footer>\n</main>\n")
	b.WriteString(themeToggleScript)
	b.WriteString(sectionNavScript)
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// Light/dark toggle, injected into every themed page. The document starts dark
// before paint. The init script removes that default only for a stored light
// choice; a stored dark choice retains it. The button (top-right) flips and
// persists the choice; styling lives in theme.css (.theme-toggle).
// Byte-for-byte the same script as workers/src/render.ts's THEME_INIT_SCRIPT.
const themeInitScript = `<script>(function(){try{var t=localStorage.getItem("waymark-theme");if(t==="light")document.documentElement.removeAttribute("data-theme");else if(t==="dark")document.documentElement.setAttribute("data-theme","dark");}catch(e){}})();</script>
`

const themeToggleButton = `<button class="theme-toggle" type="button" aria-label="Toggle light or dark theme" title="Toggle theme">
<svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true"><circle cx="10" cy="10" r="7.25" fill="none" stroke="currentColor" stroke-width="1.4"></circle><path d="M10 2.75a7.25 7.25 0 0 1 0 14.5z" fill="currentColor"></path></svg>
</button>
`

// Byte-for-byte the same script as workers/src/render.ts's THEME_TOGGLE_SCRIPT.
const themeToggleScript = `<script>(function(){var root=document.documentElement,btn=document.querySelector(".theme-toggle");function apply(t){if(t==="dark")root.setAttribute("data-theme","dark");else root.removeAttribute("data-theme");}if(btn)btn.addEventListener("click",function(){var t=root.getAttribute("data-theme")==="dark"?"light":"dark";apply(t);try{localStorage.setItem("waymark-theme",t);}catch(e){}});})();</script>
`

// The reading-progress hairline, injected on every themed page. It stays at
// zero width unless the section-nav script runs, and is hidden when printing.
// Byte-for-byte the same markup as workers/src/render.ts's READING_PROGRESS_BAR.
const readingProgressBar = `<div class="reading-progress" aria-hidden="true"></div>
`

// Progressive enhancement for .page-layout reports: marks the section the
// reader is in, dims the ones behind them, lights the nav spine, and drives
// the reading-progress hairline. With scripting off the nav is exactly the
// list of links it always was, so nothing is hidden behind this.
// Byte-for-byte the same script as workers/src/render.ts's SECTION_NAV_SCRIPT.
const sectionNavScript = `<script>(function(){var links=[].slice.call(document.querySelectorAll(".section-nav a[href^='#']")),bar=document.querySelector(".reading-progress");if(!links.length&&!bar)return;var sections=links.map(function(a){return document.getElementById(decodeURIComponent(a.getAttribute("href").slice(1)));}),list=document.querySelector(".section-nav ul"),current=-1,ticking=false;function paint(i){if(i===current)return;current=i;links.forEach(function(a,n){a.classList.toggle("is-active",n===i);a.classList.toggle("is-read",n<i);if(n===i)a.setAttribute("aria-current","true");else a.removeAttribute("aria-current");});if(list)list.style.setProperty("--spine",(links.length>1?i/(links.length-1)*100:100)+"%");}function pick(){var line=innerHeight*.28,best=0,i,s;for(i=0;i<sections.length;i++){s=sections[i];if(s&&s.getBoundingClientRect().top<=line)best=i;}if(sections.length&&innerHeight+scrollY>=document.body.scrollHeight-4)best=sections.length-1;if(links.length)paint(best);if(bar){var max=document.documentElement.scrollHeight-innerHeight;bar.style.width=(max>0?Math.min(100,scrollY/max*100):0)+"%";}}function onScroll(){if(ticking)return;ticking=true;requestAnimationFrame(function(){pick();ticking=false;});}addEventListener("scroll",onScroll,{passive:true});addEventListener("resize",onScroll);pick();})();</script>
`

func scanTagEnd(content string, i int) int {
	var quote byte
	for i < len(content) {
		c := content[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
		} else {
			switch c {
			case '\'', '"':
				quote = c
			case '>':
				return i + 1
			}
		}
		i++
	}
	return len(content)
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isASCIIAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// ValidateRawDocument checks that html is a complete, self-contained document:
// doctype, html, head, viewport meta, title, style, and body are required.
// House-theme stylesheet links and data-plan-profile attributes are rejected.
func ValidateRawDocument(html string) error {
	var (
		sawDoctype  bool
		sawHTML     bool
		sawHead     bool
		sawViewport bool
		sawTitle    bool
		sawStyle    bool
		sawBody     bool
		inHTML      bool
		inHead      bool
	)

	for i := 0; i < len(html); {
		if html[i] != '<' {
			i++
			continue
		}
		if strings.HasPrefix(html[i:], "<!--") {
			if end := strings.Index(html[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			return fmt.Errorf("must not contain an unterminated comment")
		}

		j := i + 1
		for j < len(html) && isHTMLSpace(html[j]) {
			j++
		}
		if j < len(html) && html[j] == '!' {
			j++
			for j < len(html) && isHTMLSpace(html[j]) {
				j++
			}
			start := j
			for j < len(html) && isASCIIAlpha(html[j]) {
				j++
			}
			if strings.EqualFold(html[start:j], "doctype") {
				sawDoctype = true
			}
			i = scanTagEnd(html, j)
			continue
		}

		closing := false
		if j < len(html) && html[j] == '/' {
			closing = true
			j++
			for j < len(html) && isHTMLSpace(html[j]) {
				j++
			}
		}
		start := j
		for j < len(html) && (isASCIIAlpha(html[j]) || html[j] >= '0' && html[j] <= '9' || html[j] == '-' || html[j] == ':') {
			j++
		}
		if start == j {
			i++
			continue
		}
		name := strings.ToLower(html[start:j])
		attrs, selfClosing, end := parseTagAttributes(html, j)
		if hasPlanProfile(attrs) {
			return fmt.Errorf("must not contain data-plan-profile")
		}
		if closing {
			switch name {
			case "html":
				inHTML = false
			case "head":
				inHead = false
			}
			i = end
			continue
		}

		switch name {
		case "html":
			sawHTML = true
			inHTML = true
		case "head":
			sawHead = true
			if inHTML {
				inHead = true
			}
		case "body":
			sawBody = true
			inHead = false
		case "title":
			if inHead {
				sawTitle = true
			}
		case "style":
			if inHead {
				sawStyle = true
			}
		case "meta":
			if inHead && isViewportMeta(attrs) {
				sawViewport = true
			}
		case "link":
			if isHouseThemeHref(attrs["href"]) {
				return fmt.Errorf("must not link the house theme")
			}
		}

		i = end
		if !closing && !selfClosing && (name == "style" || name == "script") {
			i = skipToEndTag(html, i, name)
		}
	}

	switch {
	case !sawDoctype:
		return fmt.Errorf("must contain a doctype")
	case !sawHTML:
		return fmt.Errorf("must contain an <html> element")
	case !sawHead:
		return fmt.Errorf("must contain a <head> element")
	case !sawViewport:
		return fmt.Errorf("must contain a viewport meta tag")
	case !sawTitle:
		return fmt.Errorf("must contain a <title> element")
	case !sawStyle:
		return fmt.Errorf("must contain a <style> element")
	case !sawBody:
		return fmt.Errorf("must contain a <body> element")
	}
	return nil
}

func parseTagAttributes(content string, i int) (map[string]string, bool, int) {
	attrs := map[string]string{}
	selfClosing := false
	for i < len(content) {
		for i < len(content) && isHTMLSpace(content[i]) {
			i++
		}
		if i >= len(content) {
			break
		}
		if content[i] == '>' {
			return attrs, selfClosing, i + 1
		}
		if content[i] == '/' {
			selfClosing = true
			i++
			continue
		}
		start := i
		for i < len(content) && !isHTMLSpace(content[i]) && content[i] != '=' && content[i] != '>' && content[i] != '/' {
			i++
		}
		if start == i {
			i++
			continue
		}
		key := strings.ToLower(content[start:i])
		for i < len(content) && isHTMLSpace(content[i]) {
			i++
		}
		value := ""
		if i < len(content) && content[i] == '=' {
			i++
			for i < len(content) && isHTMLSpace(content[i]) {
				i++
			}
			if i < len(content) && (content[i] == '"' || content[i] == '\'') {
				quote := content[i]
				i++
				from := i
				for i < len(content) && content[i] != quote {
					i++
				}
				value = content[from:i]
				if i < len(content) {
					i++
				}
			} else {
				from := i
				for i < len(content) && !isHTMLSpace(content[i]) && content[i] != '>' {
					i++
				}
				value = content[from:i]
			}
		}
		attrs[key] = value
	}
	return attrs, selfClosing, len(content)
}

func skipToEndTag(content string, i int, name string) int {
	end := "</" + name
	for i < len(content) {
		if content[i] == '<' && i+len(end) <= len(content) && strings.EqualFold(content[i:i+len(end)], end) {
			return scanTagEnd(content, i+len(end))
		}
		i++
	}
	return len(content)
}

func hasPlanProfile(attrs map[string]string) bool {
	_, ok := attrs["data-plan-profile"]
	return ok
}

func isViewportMeta(attrs map[string]string) bool {
	if !strings.EqualFold(strings.TrimSpace(attrs["name"]), "viewport") {
		return false
	}
	return strings.Contains(strings.ToLower(attrs["content"]), "width=device-width")
}

func isHouseThemeHref(href string) bool {
	href = strings.TrimSpace(href)
	if href == "" {
		return false
	}
	if i := strings.IndexAny(href, "?#"); i >= 0 {
		href = href[:i]
	}
	lower := strings.ToLower(href)
	return lower == "/theme.css" || lower == "theme.css" || strings.HasSuffix(lower, "/theme.css")
}
