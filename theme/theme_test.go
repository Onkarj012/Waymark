package theme

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLightThemeSmallTextContrast(t *testing.T) {
	for _, pair := range []struct {
		foreground string
		background string
	}{
		{foreground: "text-faint", background: "bg"},
		{foreground: "ok", background: "ok-soft"},
		{foreground: "warn", background: "warn-soft"},
	} {
		foreground := themeColor(t, pair.foreground)
		background := themeColor(t, pair.background)
		if ratio := contrastRatio(foreground, background); ratio < 4.5 {
			t.Errorf("--%s on --%s contrast = %.2f, want at least 4.5", pair.foreground, pair.background, ratio)
		}
	}
}

func TestDarkRegisterSmallTextContrast(t *testing.T) {
	// Small text lands on the field and on the recess (stat subs, table heads,
	// code), so both have to clear AA.
	for _, background := range []string{"bg", "surface-2"} {
		field := darkThemeColor(t, background)
		for _, name := range []string{"text", "text-muted", "text-faint", "accent-text", "warn", "bad", "blue", "purple"} {
			if ratio := contrastRatio(darkThemeColor(t, name), field); ratio < 4.5 {
				t.Errorf("dark --%s on --%s contrast = %.2f, want at least 4.5", name, background, ratio)
			}
		}
	}
}

// The three dark surface steps have to stay far enough apart in value that a
// code block or table head reads as a distinct object against the field. Too
// close and the whole page flattens into one sheet, which is what the original
// neutral ramp did.
func TestDarkSurfaceStepsAreSeparable(t *testing.T) {
	for _, pair := range [2][2]string{{"surface", "bg"}, {"surface-2", "surface"}} {
		ratio := contrastRatio(darkThemeColor(t, pair[0]), darkThemeColor(t, pair[1]))
		if ratio < 1.08 {
			t.Errorf("dark --%s against --%s contrast = %.3f, want at least 1.08", pair[0], pair[1], ratio)
		}
	}
}

func TestSectionNavigationResponsiveContract(t *testing.T) {
	for _, rule := range []string{
		".page:has(> .page-layout) { max-width: 1080px; }",
		"grid-template-columns: minmax(132px, 160px) minmax(0, 1fr);",
		".section-nav {\n  position: sticky;",
		"@media (max-width: 900px)",
		".page-layout { display: block; }",
	} {
		if !strings.Contains(CSS, rule) {
			t.Errorf("responsive section navigation rule %q was not found", rule)
		}
	}
}

// The section-nav script in internal/web/server.go sets these hooks. If a
// selector here is renamed without renaming it there, the nav silently stops
// reflecting reading position.
func TestSectionNavigationActiveStateContract(t *testing.T) {
	for _, rule := range []string{
		".section-nav a.is-active {",
		".section-nav a.is-active::before {",
		".section-nav a.is-read { color: var(--text-faint); }",
		"height: var(--spine, 0%);",
		".reading-progress {",
	} {
		if !strings.Contains(CSS, rule) {
			t.Errorf("section navigation active-state rule %q was not found", rule)
		}
	}
}

// A section's signal core has to reach its heading marker, list markers, quote
// rule and code gutter, or the color stops meaning "this is one section".
func TestSectionMarkerInheritanceContract(t *testing.T) {
	for _, rule := range []string{
		"{ --marker: var(--teal); }",
		"{ --marker: var(--seal); }",
		"background: var(--marker, var(--accent));",
		"li::marker { color: var(--marker, var(--text-faint)); }",
		"border-left: 2px solid var(--marker, var(--border-strong));",
		"border-left: 2px solid var(--marker, var(--accent));",
	} {
		if !strings.Contains(CSS, rule) {
			t.Errorf("section marker rule %q was not found", rule)
		}
	}
}

func TestGeneratedCreditStyleContract(t *testing.T) {
	for _, rule := range []string{
		".waymark-credit { justify-content: center; text-align: center; }",
		".waymark-credit a {",
		"text-decoration-color: transparent;",
	} {
		if !strings.Contains(CSS, rule) {
			t.Errorf("generated credit style %q was not found", rule)
		}
	}
}

func TestCalloutFallbackContract(t *testing.T) {
	for _, rule := range []string{
		".callout:not(:has(> .ico)) { flex-direction: column; }",
		".callout:not(:has(> .ico)) > :first-child { margin-top: 0; padding-top: 0; }",
		".callout:not(:has(> .ico)) > :last-child { margin-bottom: 0; }",
	} {
		if !strings.Contains(CSS, rule) {
			t.Errorf("callout fallback rule %q was not found", rule)
		}
	}
}

// darkThemeColor reads a variable from the :root[data-theme="dark"] block only,
// so a dark value is never silently compared against its light counterpart.
func darkThemeColor(t *testing.T, name string) [3]float64 {
	t.Helper()
	start := strings.Index(CSS, `:root[data-theme="dark"] {`)
	if start < 0 {
		t.Fatal("dark register block was not found")
	}
	end := strings.Index(CSS[start:], "\n}")
	if end < 0 {
		t.Fatal("dark register block was not terminated")
	}
	return colorFrom(t, CSS[start:start+end], name)
}

func themeColor(t *testing.T, name string) [3]float64 {
	t.Helper()
	return colorFrom(t, CSS, name)
}

func colorFrom(t *testing.T, css, name string) [3]float64 {
	t.Helper()
	re := regexp.MustCompile(fmt.Sprintf(`--%s:\s*#([0-9a-fA-F]{6})`, regexp.QuoteMeta(name)))
	match := re.FindStringSubmatch(css)
	if match == nil {
		t.Fatalf("theme variable --%s was not found", name)
	}

	var color [3]float64
	for i := range color {
		value, err := strconv.ParseUint(match[1][i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		color[i] = float64(value) / 255
	}
	return color
}

func contrastRatio(a, b [3]float64) float64 {
	light := relativeLuminance(a)
	dark := relativeLuminance(b)
	if light < dark {
		light, dark = dark, light
	}
	return (light + 0.05) / (dark + 0.05)
}

func relativeLuminance(color [3]float64) float64 {
	linear := func(value float64) float64 {
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(color[0]) + 0.7152*linear(color[1]) + 0.0722*linear(color[2])
}
