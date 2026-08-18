// Package render builds Waymark themed HTML documents and validates body-only
// HTML intended for those documents.
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
`)
}

// Standalone wraps body content in a self-contained Waymark document using the
// caller-provided CSS. Callers can pass theme.CSS for the house theme.
func Standalone(title, content, css string) string {
	var stylesheet strings.Builder
	stylesheet.WriteString("<style>\n")
	stylesheet.WriteString(escapeStyleEndTag(css))
	if css != "" && !strings.HasSuffix(css, "\n") {
		stylesheet.WriteByte('\n')
	}
	stylesheet.WriteString("</style>\n")
	return themedDocument(title, content, stylesheet.String())
}

// escapeStyleEndTag keeps caller CSS inside the stylesheet element. CSS
// escapes are understood by the CSS parser, while the HTML parser no longer
// sees a case-insensitive </style sequence that could terminate the element.
func escapeStyleEndTag(css string) string {
	var b strings.Builder
	for i := 0; i < len(css); i++ {
		if css[i] == '<' && i+7 <= len(css) && strings.EqualFold(css[i:i+7], "</style") {
			b.WriteString(`\3c `)
			continue
		}
		b.WriteByte(css[i])
	}
	return b.String()
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

var forbiddenThemedElements = map[string]struct{}{
	"html":   {},
	"head":   {},
	"body":   {},
	"style":  {},
	"script": {},
	"link":   {},
	"base":   {},
	"meta":   {},
	"title":  {},
}

// ValidateThemedBody rejects document-level, style, and script markup that
// cannot safely live inside the themed document wrapper. Semantic body HTML and
// inline SVG are permitted.
func ValidateThemedBody(content string) error {
	for i := 0; i < len(content); {
		if content[i] != '<' {
			i++
			continue
		}
		if strings.HasPrefix(content[i:], "<!--") {
			if end := strings.Index(content[i+4:], "-->"); end >= 0 {
				i += 4 + end + 3
				continue
			}
			i += 4
			continue
		}

		j := i + 1
		for j < len(content) && isHTMLSpace(content[j]) {
			j++
		}
		if j < len(content) && content[j] == '!' {
			j++
			for j < len(content) && isHTMLSpace(content[j]) {
				j++
			}
			start := j
			for j < len(content) && isASCIIAlpha(content[j]) {
				j++
			}
			if strings.EqualFold(content[start:j], "doctype") {
				return fmt.Errorf("themed body must not contain a doctype")
			}
			i = scanTagEnd(content, j)
			continue
		}
		if j < len(content) && content[j] == '/' {
			j++
			for j < len(content) && isHTMLSpace(content[j]) {
				j++
			}
		}
		start := j
		for j < len(content) && (isASCIIAlpha(content[j]) || content[j] >= '0' && content[j] <= '9' || content[j] == '-' || content[j] == ':') {
			j++
		}
		if start == j {
			i++
			continue
		}
		name := strings.ToLower(content[start:j])
		if _, forbidden := forbiddenThemedElements[name]; forbidden {
			return fmt.Errorf("themed body must not contain <%s> elements", name)
		}
		i = scanTagEnd(content, j)
	}
	return nil
}

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
