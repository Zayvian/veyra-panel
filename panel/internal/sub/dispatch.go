package sub

import (
	"net/http"
	"strings"
	"time"
)

// Format is the shape a subscription is rendered in.
type Format string

const (
	FormatSingBox Format = "singbox" // sing-box JSON
	FormatClash   Format = "clash"   // mihomo YAML
	FormatBase64  Format = "base64"  // base64 of newline-joined share links
)

// nowFunc is swapped in tests.
var nowFunc = time.Now

// userAgentFormats maps a substring of the User-Agent to a format. Matched
// case-insensitively, longest first, so "sing-box" wins over a bare "box" and
// "clash-verge" is not mistaken for something else.
var userAgentFormats = []struct {
	needle string
	format Format
}{
	{"sing-box", FormatSingBox},
	{"singbox", FormatSingBox},
	{"sfa", FormatSingBox}, // sing-box for Android
	{"sfi", FormatSingBox}, // sing-box for iOS
	{"sfm", FormatSingBox}, // sing-box for macOS
	{"sft", FormatSingBox}, // sing-box for tvOS
	{"hiddify", FormatSingBox},
	{"karing", FormatSingBox},

	{"clash-verge", FormatClash},
	{"clashmeta", FormatClash},
	{"clash.meta", FormatClash},
	{"flclash", FormatClash},
	{"mihomo", FormatClash},
	{"clash", FormatClash},
	{"stash", FormatClash},
}

// Detect picks a format for a request.
//
// A subscription URL is a bearer credential. It deliberately has no public
// HTML preview: browsers receive the same Base64 response as a generic client,
// so opening a link does not directly disclose its node list and user details.
func Detect(r *http.Request) Format {
	if q := strings.ToLower(r.URL.Query().Get("format")); q != "" {
		switch q {
		case "singbox", "sing-box":
			return FormatSingBox
		case "clash", "mihomo":
			return FormatClash
		case "base64", "v2ray", "shadowrocket":
			return FormatBase64
		}
	}

	ua := strings.ToLower(r.Header.Get("User-Agent"))
	for _, m := range userAgentFormats {
		if strings.Contains(ua, m.needle) {
			return m.format
		}
	}

	// Base64 share links are the format with the widest client support, so an
	// unrecognised client gets something it can probably use rather than an
	// error.
	return FormatBase64
}

// ContentType is the type to serve a format as.
func ContentType(f Format) string {
	switch f {
	case FormatSingBox:
		return "application/json; charset=utf-8"
	case FormatClash:
		return "text/yaml; charset=utf-8"
	default:
		// Deliberately text/plain: browsers that reach this by accident should
		// show it, not download it.
		return "text/plain; charset=utf-8"
	}
}
