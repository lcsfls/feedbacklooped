package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

//go:embed web/widget.js
var widgetJS string

// parseOrigins turns one origin per line into a clean list of
// "scheme://host[:port]" values for frame-ancestors.
func parseOrigins(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' || r == '\r' }) {
		u, err := url.Parse(strings.TrimSpace(line))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(u.Host, "*;'\"") {
			continue
		}
		o := u.Scheme + "://" + strings.ToLower(u.Host)
		if !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	return out
}

// handleWidget serves the embeddable script. Its settings come from the
// script tag's data attributes; colors and the base URL from this server.
func (a *App) handleWidget(w http.ResponseWriter, r *http.Request) {
	st := a.store.Settings()
	cfg := map[string]any{
		"base":    a.baseURL(r),
		"color":   defaultColor(st.Brand.PrimaryLight, "#171717"),
		"enabled": len(st.EmbedOrigins) > 0,
	}
	b, _ := json.Marshal(cfg)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write([]byte("(function(){var CFG=" + string(b) + ";\n" + widgetJS + "\n})();"))
}
