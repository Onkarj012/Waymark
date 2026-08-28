import { describe, expect, it } from "vitest";
import { escapeHtml, renderThemed, validateRawDocument } from "../src/render";

function validRawDocument(inner = "<p>Hi</p>", title = "Title"): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>body { margin: 0; }</style>
</head>
<body>
${inner}
</body>
</html>
`;
}

describe("escapeHtml", () => {
  it("escapes & ' < > \" like Go's html.EscapeString", () => {
    expect(escapeHtml(`Status <check> & "quoted" 'single'`)).toBe(
      "Status &lt;check&gt; &amp; &#34;quoted&#34; &#39;single&#39;",
    );
  });
});

describe("renderThemed", () => {
  it("wraps body content, escapes the title, links /theme.css, and keeps publisher HTML unescaped", () => {
    const html = renderThemed("Status <check>", "<script>window.demo=true</script><p>Hello</p>");
    expect(html).toContain('<html lang="en" data-theme="dark">');
    expect(html).toContain("<title>Status &lt;check&gt;</title>");
    expect(html).toContain('<link rel="icon" href="/favicon.svg" type="image/svg+xml">');
    expect(html).toContain('<link rel="stylesheet" href="/theme.css">');
    expect(html).toContain("<script>window.demo=true</script><p>Hello</p>");
  });

  it("places the generated-on-Waymark credit after publisher content, inside <main class=\"page\">", () => {
    const html = renderThemed("Title", "<p>Body</p>");
    const credit = `<footer class="waymark-credit">\n<a href="https://github.com/Onkarj012/Waymark" target="_blank" rel="noopener noreferrer">generated on Waymark</a>\n</footer>`;
    expect(html).toContain(credit);
    expect(html.indexOf("<p>Body</p>")).toBeLessThan(html.indexOf(credit));
    expect(html).toContain('<main class="page">');
  });

  it("defaults first visits to dark and honors stored light before paint", () => {
    const html = renderThemed("Title", "<p>Body</p>");
    expect(html.indexOf("waymark-theme")).toBeLessThan(html.indexOf("<body>"));
    expect(html).toContain('if(t==="light")document.documentElement.removeAttribute("data-theme")');
    expect(html).toContain('else if(t==="dark")document.documentElement.setAttribute("data-theme","dark")');
    expect(html).not.toContain("prefers-color-scheme");
    expect(html).not.toContain("matchMedia");
  });

  it("injects the reading-progress bar and the section-nav script", () => {
    const html = renderThemed("Title", "<p>Body</p>");
    expect(html).toContain('<div class="reading-progress" aria-hidden="true"></div>');
    // the hooks the theme styles against — renaming either here or in
    // theme.css without the other silently breaks reading-position tracking
    expect(html).toContain('.section-nav a[href^=\'#\']');
    expect(html).toContain('"is-active"');
    expect(html).toContain('"is-read"');
    expect(html).toContain('setProperty("--spine"');
    // the script runs after the content it observes
    expect(html.indexOf("<p>Body</p>")).toBeLessThan(html.indexOf('"is-active"'));
  });
});

describe("validateRawDocument", () => {
  it("accepts a complete self-contained document", () => {
    expect(validateRawDocument(validRawDocument())).toBeNull();
    expect(validateRawDocument(validRawDocument("<article class=\"plan\"><p>x</p></article>"))).toBeNull();
    expect(validateRawDocument(validRawDocument("<!-- note --><p>Safe</p>"))).toBeNull();
  });

  it("requires doctype, html, head, viewport, title, style, and body", () => {
    const full = validRawDocument();
    const cases: Array<[string, string, string]> = [
      ["doctype", full.replace("<!DOCTYPE html>", ""), "doctype"],
      ["html", full.replace('<html lang="en">', "").replace("</html>", ""), "<html>"],
      ["head", full.replace("<head>", "").replace("</head>\n", ""), "<head>"],
      ["viewport", full.replace('<meta name="viewport" content="width=device-width, initial-scale=1">', ""), "viewport"],
      ["title", full.replace("<title>Title</title>", ""), "<title>"],
      ["style", full.replace("<style>body { margin: 0; }</style>", ""), "<style>"],
      ["body", full.replace("<body>\n", "").replace("\n</body>", ""), "<body>"],
      [
        "style in body",
        `<!DOCTYPE html><html><head><meta name="viewport" content="width=device-width"><title>T</title></head><body><style>x{}</style></body></html>`,
        "<style>",
      ],
    ];
    for (const [name, html, want] of cases) {
      const err = validateRawDocument(html);
      expect(err, name).toBeTruthy();
      expect(err, name).toContain(want);
    }
  });

  it("rejects house-theme links and data-plan-profile attributes", () => {
    const full = validRawDocument();
    expect(validateRawDocument(full.replace("<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="/theme.css">`))).toContain(
      "house theme",
    );
    expect(validateRawDocument(full.replace("<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="theme.css">`))).toContain(
      "house theme",
    );
    expect(
      validateRawDocument(full.replace("<style>body { margin: 0; }</style>", `<link rel="stylesheet" href="https://pages.example/theme.css">`)),
    ).toContain("house theme");
    expect(validateRawDocument(full.replace("<style>body { margin: 0; }</style>", `<link rel=stylesheet href=/theme.css>`))).toContain("house theme");
    expect(validateRawDocument(validRawDocument(`<article class="plan" data-plan-profile="systems"><p>x</p></article>`))).toContain(
      "data-plan-profile",
    );
  });

  it("rejects unterminated comments", () => {
    expect(validateRawDocument(`${validRawDocument()}<!-- unfinished`)).toContain("unterminated comment");
  });

  it("ignores data-plan-profile when it only appears inside a stylesheet", () => {
    const html = validRawDocument().replace("body { margin: 0; }", "article.plan[data-plan-profile] { color: red; }");
    expect(validateRawDocument(html)).toBeNull();
  });
});
