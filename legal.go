package main

import "net/http"

// handleLegal serves the imprint or privacy page: the text from the settings
// if set, otherwise a redirect to the configured external URL.
func (a *App) handleLegal(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := a.store.Settings()
		title, text, link := "Imprint", st.ImprintText, st.ImprintURL
		if kind == "privacy" {
			title, text, link = "Privacy policy", st.PrivacyText, st.PrivacyURL
		}
		if text == "" {
			if link != "" {
				http.Redirect(w, r, link, http.StatusFound)
				return
			}
			a.notFound(w, r)
			return
		}
		a.render(w, r, 200, "legal", pageData{"Title": title, "Text": text})
	}
}
