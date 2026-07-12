import { describe, expect, it } from "vitest";
import { escapeHtml, renderThemed } from "../src/render";

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
    expect(html).toContain("<title>Status &lt;check&gt;</title>");
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

  it("injects the theme init script before <body> so dark mode applies before paint", () => {
    const html = renderThemed("Title", "<p>Body</p>");
    expect(html.indexOf("waymark-theme")).toBeLessThan(html.indexOf("<body>"));
  });
});
