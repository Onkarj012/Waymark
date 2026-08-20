// Port of the themed/raw rendering contract in internal/render/render.go.
// Themed pages store body-only HTML; the server wraps it in a full document
// that links /theme.css. Raw pages are served exactly as submitted. Never
// blur the two (see AGENTS.md "Conventions & invariants").

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

/** Wraps body content in a full HTML document that links the house
 * stylesheet. The agent only writes the content that lives inside .page.
 * Mirrors renderThemed() in internal/render/render.go line-for-line. */
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
