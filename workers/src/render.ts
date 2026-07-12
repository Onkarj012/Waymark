// Port of the themed/raw rendering contract in internal/web/server.go.
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

// Light/dark toggle, injected into every themed page. The init script runs in
// <head> before paint to avoid a flash: it follows the visitor's stored
// choice, or the system preference on first load. The button (top-right)
// flips and persists the choice; styling lives in theme.css (.theme-toggle).
// Byte-for-byte the same script as internal/web/server.go's themeInitScript.
export const THEME_INIT_SCRIPT = `<script>(function(){try{var t=localStorage.getItem("waymark-theme");if(!t)t=matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light";if(t==="dark")document.documentElement.setAttribute("data-theme","dark");}catch(e){}})();</script>
`;

export const THEME_TOGGLE_BUTTON = `<button class="theme-toggle" type="button" aria-label="Toggle light or dark theme" title="Toggle theme">
<svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true"><circle cx="10" cy="10" r="7.25" fill="none" stroke="currentColor" stroke-width="1.4"></circle><path d="M10 2.75a7.25 7.25 0 0 1 0 14.5z" fill="currentColor"></path></svg>
</button>
`;

export const THEME_TOGGLE_SCRIPT = `<script>(function(){var root=document.documentElement,btn=document.querySelector(".theme-toggle"),mq=matchMedia("(prefers-color-scheme: dark)");function apply(t){if(t==="dark")root.setAttribute("data-theme","dark");else root.removeAttribute("data-theme");}if(btn)btn.addEventListener("click",function(){var t=root.getAttribute("data-theme")==="dark"?"light":"dark";apply(t);try{localStorage.setItem("waymark-theme",t);}catch(e){}});try{if(!localStorage.getItem("waymark-theme"))mq.addEventListener("change",function(e){apply(e.matches?"dark":"light");});}catch(e){}})();</script>
`;

/** Wraps body content in a full HTML document that links the house
 * stylesheet. The agent only writes the content that lives inside .page.
 * Mirrors renderThemed() in internal/web/server.go line-for-line. */
export function renderThemed(title: string, content: string): string {
  return (
    `<!DOCTYPE html>\n<html lang="en">\n<head>\n` +
    `<meta charset="utf-8">\n` +
    `<meta name="viewport" content="width=device-width, initial-scale=1">\n` +
    `<title>${escapeHtml(title)}</title>\n` +
    `<link rel="stylesheet" href="/theme.css">\n` +
    THEME_INIT_SCRIPT +
    `</head>\n<body>\n` +
    THEME_TOGGLE_BUTTON +
    `<main class="page">\n` +
    content +
    `\n<footer class="waymark-credit">\n` +
    `<a href="https://github.com/Onkarj012/Waymark" target="_blank" rel="noopener noreferrer">generated on Waymark</a>\n` +
    `</footer>\n</main>\n` +
    THEME_TOGGLE_SCRIPT +
    `</body>\n</html>\n`
  );
}
