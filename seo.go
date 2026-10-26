package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

// Only the public pages may ever be indexed. Status pages, attachments, the
// admin area and the APIs are always excluded, because a status link is the
// only thing protecting a submission.
func (a *App) handleRobots(w http.ResponseWriter, r *http.Request) {
	st := a.store.Settings()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !st.SearchIndexing {
		w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	var b strings.Builder
	b.WriteString("User-agent: *\nAllow: /$\n")
	if st.RoadmapEnabled {
		b.WriteString("Allow: /roadmap$\n")
	}
	if st.ImprintText != "" {
		b.WriteString("Allow: /imprint$\n")
	}
	if st.PrivacyText != "" {
		b.WriteString("Allow: /privacy$\n")
	}
	b.WriteString("Allow: /static/\nAllow: /brand/\nDisallow: /\n")
	fmt.Fprintf(&b, "\nSitemap: %s/sitemap.xml\n", a.baseURL(r))
	w.Write([]byte(b.String()))
}

func (a *App) handleSitemap(w http.ResponseWriter, r *http.Request) {
	st := a.store.Settings()
	if !st.SearchIndexing {
		a.notFound(w, r)
		return
	}
	type url struct {
		Loc string `xml:"loc"`
	}
	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		NS      string   `xml:"xmlns,attr"`
		URLs    []url    `xml:"url"`
	}
	base := a.baseURL(r)
	set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: []url{{base + "/"}}}
	if st.RoadmapEnabled {
		set.URLs = append(set.URLs, url{base + "/roadmap"})
	}
	if st.ImprintText != "" {
		set.URLs = append(set.URLs, url{base + "/imprint"})
	}
	if st.PrivacyText != "" {
		set.URLs = append(set.URLs, url{base + "/privacy"})
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(set)
}
