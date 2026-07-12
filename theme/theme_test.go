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

func themeColor(t *testing.T, name string) [3]float64 {
	t.Helper()
	re := regexp.MustCompile(fmt.Sprintf(`--%s:\s*#([0-9a-fA-F]{6})`, regexp.QuoteMeta(name)))
	match := re.FindStringSubmatch(CSS)
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
