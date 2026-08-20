// Package theme embeds the Waymark house stylesheet and favicon so the server
// binary is fully self-contained. theme.css is the single source of truth
// for the look of every themed page.
package theme

import _ "embed"

//go:embed theme.css
var CSS string

//go:embed logo-dark.svg
var FaviconSVG string
