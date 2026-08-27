// Port of the rendering contract in internal/render/render.go.
// New writes are complete raw documents. Legacy themed pages still store
// body-only HTML and are wrapped with /theme.css. Never blur the two
// (see AGENTS.md "Conventions & invariants").

/** Escapes text for safe inclusion inside an HTML text node — mirrors Go's
 * html.EscapeString (escapes & < > " ' as named entities in the same order
 * of precedence Go uses, so titles render identically byte-for-byte). */
export function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("'", "&#39;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&#34;");
}

// Light/dark toggle, injected into every themed page. The document starts dark
// before paint. The init script removes that default only for a stored light
// choice; a stored dark choice retains it. The button (top-right) flips and
// persists the choice; styling lives in theme.css (.theme-toggle).
// Byte-for-byte the same script as internal/render/render.go's themeInitScript.
export const THEME_INIT_SCRIPT = `<script>(function(){try{var t=localStorage.getItem("waymark-theme");if(t==="light")document.documentElement.removeAttribute("data-theme");else if(t==="dark")document.documentElement.setAttribute("data-theme","dark");}catch(e){}})();</script>
`;

export const THEME_TOGGLE_BUTTON = `<button class="theme-toggle" type="button" aria-label="Toggle light or dark theme" title="Toggle theme">
<svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true"><circle cx="10" cy="10" r="7.25" fill="none" stroke="currentColor" stroke-width="1.4"></circle><path d="M10 2.75a7.25 7.25 0 0 1 0 14.5z" fill="currentColor"></path></svg>
</button>
`;

// Byte-for-byte the same script as internal/render/render.go's themeToggleScript.
export const THEME_TOGGLE_SCRIPT = `<script>(function(){var root=document.documentElement,btn=document.querySelector(".theme-toggle");function apply(t){if(t==="dark")root.setAttribute("data-theme","dark");else root.removeAttribute("data-theme");}if(btn)btn.addEventListener("click",function(){var t=root.getAttribute("data-theme")==="dark"?"light":"dark";apply(t);try{localStorage.setItem("waymark-theme",t);}catch(e){}});})();</script>
`;

// The reading-progress hairline, injected on every themed page. It stays at
// zero width unless the section-nav script runs, and is hidden when printing.
// Byte-for-byte the same markup as internal/render/render.go's readingProgressBar.
export const READING_PROGRESS_BAR = `<div class="reading-progress" aria-hidden="true"></div>
`;

// Progressive enhancement for .page-layout reports: marks the section the
// reader is in, dims the ones behind them, lights the nav spine, and drives
// the reading-progress hairline. With scripting off the nav is exactly the
// list of links it always was, so nothing is hidden behind this.
// Byte-for-byte the same script as internal/render/render.go's sectionNavScript.
export const SECTION_NAV_SCRIPT = `<script>(function(){var links=[].slice.call(document.querySelectorAll(".section-nav a[href^='#']")),bar=document.querySelector(".reading-progress");if(!links.length&&!bar)return;var sections=links.map(function(a){return document.getElementById(decodeURIComponent(a.getAttribute("href").slice(1)));}),list=document.querySelector(".section-nav ul"),current=-1,ticking=false;function paint(i){if(i===current)return;current=i;links.forEach(function(a,n){a.classList.toggle("is-active",n===i);a.classList.toggle("is-read",n<i);if(n===i)a.setAttribute("aria-current","true");else a.removeAttribute("aria-current");});if(list)list.style.setProperty("--spine",(links.length>1?i/(links.length-1)*100:100)+"%");}function pick(){var line=innerHeight*.28,best=0,i,s;for(i=0;i<sections.length;i++){s=sections[i];if(s&&s.getBoundingClientRect().top<=line)best=i;}if(sections.length&&innerHeight+scrollY>=document.body.scrollHeight-4)best=sections.length-1;if(links.length)paint(best);if(bar){var max=document.documentElement.scrollHeight-innerHeight;bar.style.width=(max>0?Math.min(100,scrollY/max*100):0)+"%";}}function onScroll(){if(ticking)return;ticking=true;requestAnimationFrame(function(){pick();ticking=false;});}addEventListener("scroll",onScroll,{passive:true});addEventListener("resize",onScroll);pick();})();</script>
`;

const HTML_SPACE = /[ \t\n\r\f]/;

function isHTMLSpace(c: string | undefined): boolean {
  return c !== undefined && HTML_SPACE.test(c);
}

function isASCIIAlpha(c: string | undefined): boolean {
  return c !== undefined && ((c >= "a" && c <= "z") || (c >= "A" && c <= "Z"));
}

function scanTagEnd(content: string, i: number): number {
  let quote = "";
  for (; i < content.length; i++) {
    const c = content[i];
    if (quote) {
      if (c === quote) quote = "";
    } else if (c === "'" || c === '"') {
      quote = c;
    } else if (c === ">") {
      return i + 1;
    }
  }
  return content.length;
}

function parseTagAttributes(content: string, i: number): { attrs: Record<string, string>; selfClosing: boolean; end: number } {
  const attrs: Record<string, string> = {};
  let selfClosing = false;
  while (i < content.length) {
    while (i < content.length && isHTMLSpace(content[i])) i++;
    if (i >= content.length) break;
    if (content[i] === ">") return { attrs, selfClosing, end: i + 1 };
    if (content[i] === "/") {
      selfClosing = true;
      i++;
      continue;
    }
    const start = i;
    while (i < content.length && !isHTMLSpace(content[i]) && content[i] !== "=" && content[i] !== ">" && content[i] !== "/") i++;
    if (start === i) {
      i++;
      continue;
    }
    const key = content.slice(start, i).toLowerCase();
    while (i < content.length && isHTMLSpace(content[i])) i++;
    let value = "";
    if (i < content.length && content[i] === "=") {
      i++;
      while (i < content.length && isHTMLSpace(content[i])) i++;
      if (i < content.length && (content[i] === '"' || content[i] === "'")) {
        const quote = content[i];
        i++;
        const from = i;
        while (i < content.length && content[i] !== quote) i++;
        value = content.slice(from, i);
        if (i < content.length) i++;
      } else {
        const from = i;
        while (i < content.length && !isHTMLSpace(content[i]) && content[i] !== ">" && content[i] !== "/") i++;
        value = content.slice(from, i);
      }
    }
    attrs[key] = value;
  }
  return { attrs, selfClosing, end: content.length };
}

function skipToEndTag(content: string, i: number, name: string): number {
  const end = `</${name}`;
  while (i < content.length) {
    if (content[i] === "<" && i + end.length <= content.length && content.slice(i, i + end.length).toLowerCase() === end) {
      return scanTagEnd(content, i + end.length);
    }
    i++;
  }
  return content.length;
}

function isViewportMeta(attrs: Record<string, string>): boolean {
  if (attrs.name?.trim().toLowerCase() !== "viewport") return false;
  return (attrs.content ?? "").toLowerCase().includes("width=device-width");
}

function isHouseThemeHref(href: string | undefined): boolean {
  if (!href) return false;
  let value = href.trim();
  if (!value) return false;
  const cut = value.search(/[?#]/);
  if (cut >= 0) value = value.slice(0, cut);
  const lower = value.toLowerCase();
  return lower === "/theme.css" || lower === "theme.css" || lower.endsWith("/theme.css");
}

/** Complete-document validator. Mirrors ValidateRawDocument in
 * internal/render/render.go. Returns the same error strings so Go and Worker
 * API messages stay aligned. */
export function validateRawDocument(html: string): string | null {
  let sawDoctype = false;
  let sawHTML = false;
  let sawHead = false;
  let sawViewport = false;
  let sawTitle = false;
  let sawStyle = false;
  let sawBody = false;
  let inHTML = false;
  let inHead = false;

  for (let i = 0; i < html.length; ) {
    if (html[i] !== "<") {
      i++;
      continue;
    }
    if (html.startsWith("<!--", i)) {
      const end = html.indexOf("-->", i + 4);
      if (end >= 0) {
        i = end + 3;
        continue;
      }
      break;
    }

    let j = i + 1;
    while (j < html.length && isHTMLSpace(html[j])) j++;
    if (j < html.length && html[j] === "!") {
      j++;
      while (j < html.length && isHTMLSpace(html[j])) j++;
      const start = j;
      while (j < html.length && isASCIIAlpha(html[j])) j++;
      if (html.slice(start, j).toLowerCase() === "doctype") sawDoctype = true;
      i = scanTagEnd(html, j);
      continue;
    }

    let closing = false;
    if (j < html.length && html[j] === "/") {
      closing = true;
      j++;
      while (j < html.length && isHTMLSpace(html[j])) j++;
    }
    const start = j;
    while (j < html.length) {
      const char = html[j];
      if (!(isASCIIAlpha(char) || (char !== undefined && char >= "0" && char <= "9") || char === "-" || char === ":")) {
        break;
      }
      j++;
    }
    if (start === j) {
      i++;
      continue;
    }
    const name = html.slice(start, j).toLowerCase();
    const parsed = parseTagAttributes(html, j);
    if ("data-plan-profile" in parsed.attrs) return "must not contain data-plan-profile";
    if (closing) {
      if (name === "html") inHTML = false;
      if (name === "head") inHead = false;
      i = parsed.end;
      continue;
    }

    switch (name) {
      case "html":
        sawHTML = true;
        inHTML = true;
        break;
      case "head":
        sawHead = true;
        if (inHTML) inHead = true;
        break;
      case "body":
        sawBody = true;
        inHead = false;
        break;
      case "title":
        if (inHead) sawTitle = true;
        break;
      case "style":
        if (inHead) sawStyle = true;
        break;
      case "meta":
        if (inHead && isViewportMeta(parsed.attrs)) sawViewport = true;
        break;
      case "link":
        if (isHouseThemeHref(parsed.attrs.href)) return "must not link the house theme";
        break;
    }

    i = parsed.end;
    if (!closing && !parsed.selfClosing && (name === "style" || name === "script")) {
      i = skipToEndTag(html, i, name);
    }
  }

  if (!sawDoctype) return "must contain a doctype";
  if (!sawHTML) return "must contain an <html> element";
  if (!sawHead) return "must contain a <head> element";
  if (!sawViewport) return "must contain a viewport meta tag";
  if (!sawTitle) return "must contain a <title> element";
  if (!sawStyle) return "must contain a <style> element";
  if (!sawBody) return "must contain a <body> element";
  return null;
}

/** Wraps body content in a full HTML document that links the house
 * stylesheet. Kept for legacy themed pages stored before the raw-only write
 * contract. Mirrors renderThemed() in internal/render/render.go. */
export function renderThemed(title: string, content: string): string {
  return (
    `<!DOCTYPE html>\n<html lang="en" data-theme="dark">\n<head>\n` +
    `<meta charset="utf-8">\n` +
    `<meta name="viewport" content="width=device-width, initial-scale=1">\n` +
    `<title>${escapeHtml(title)}</title>\n` +
    `<link rel="icon" href="/favicon.svg" type="image/svg+xml">\n` +
    `<link rel="stylesheet" href="/theme.css">\n` +
    THEME_INIT_SCRIPT +
    `</head>\n<body>\n` +
    READING_PROGRESS_BAR +
    THEME_TOGGLE_BUTTON +
    `<main class="page">\n` +
    content +
    `\n<footer class="waymark-credit">\n` +
    `<a href="https://github.com/Onkarj012/Waymark" target="_blank" rel="noopener noreferrer">generated on Waymark</a>\n` +
    `</footer>\n</main>\n` +
    THEME_TOGGLE_SCRIPT +
    SECTION_NAV_SCRIPT +
    `</body>\n</html>\n`
  );
}
