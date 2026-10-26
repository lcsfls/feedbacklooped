package main

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func (a *App) handleForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 200, "submit", pageData{"Stamp": a.formStamp()})
}

// parseUpload limits the request size and parses the multipart form.
func (a *App) parseUpload(w http.ResponseWriter, r *http.Request) error {
	st := a.store.Settings()
	max := int64(st.MaxFiles)*int64(st.MaxFileMB)<<20 + 2<<20
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("Upload too large (max. %d files of %d MB each)", st.MaxFiles, st.MaxFileMB)
		}
		if errors.Is(err, http.ErrNotMultipart) {
			return r.ParseForm()
		}
		return errors.New("Could not read the form")
	}
	return nil
}

func formFiles(r *http.Request) []*multipart.FileHeader {
	if r.MultipartForm == nil {
		return nil
	}
	var out []*multipart.FileHeader
	for _, fh := range r.MultipartForm.File["files"] {
		if fh.Size > 0 {
			out = append(out, fh)
		}
	}
	return out
}

// saveUploads stores all files or none.
func (a *App) saveUploads(files []*multipart.FileHeader, already int) ([]Attachment, error) {
	st := a.store.Settings()
	if len(files) > 0 && st.MaxStorageMB > 0 {
		var incoming int64
		for _, fh := range files {
			incoming += fh.Size
		}
		if a.uploadsSize()+incoming > int64(st.MaxStorageMB)<<20 {
			return nil, errors.New("Uploads are temporarily not possible because the storage limit is reached. Please send your feedback without attachments.")
		}
	}
	if already+len(files) > st.MaxFiles*3 || len(files) > st.MaxFiles {
		return nil, fmt.Errorf("Too many files (max. %d per message)", st.MaxFiles)
	}
	var saved []Attachment
	for _, fh := range files {
		att, err := a.saveUpload(fh, int64(st.MaxFileMB)<<20)
		if err != nil {
			a.removeFiles(saved)
			return nil, err
		}
		saved = append(saved, att)
	}
	return saved, nil
}

func (a *App) removeFiles(atts []Attachment) {
	for _, att := range atts {
		os.Remove(filepath.Join(a.store.Uploads, att.File))
	}
}

func cleanText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

func (a *App) baseURL(r *http.Request) string {
	if b := a.store.Settings().BaseURL; b != "" {
		return strings.TrimRight(b, "/")
	}
	scheme := "http"
	if a.isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if a.cfg.TrustProxy && r.Header.Get("X-Forwarded-Host") != "" {
		host = r.Header.Get("X-Forwarded-Host")
	}
	return scheme + "://" + host
}

func (a *App) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.fail(w, r, 403, "Request from another site rejected.")
		return
	}
	if err := a.parseUpload(w, r); err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	// Bots: honeypot filled in or form submitted too fast.
	if r.FormValue("website") != "" || !a.checkFormStamp(r.FormValue("stamp")) {
		a.fail(w, r, 400, "Please wait a moment and submit the form again.")
		return
	}
	if !a.checkPoW(r) {
		a.fail(w, r, 400, "The spam check failed or expired. Please reload the page and try again (JavaScript is required).")
		return
	}
	if !a.submitLimit.AllowN(a.clientKey(r), max(1, a.store.Settings().RateLimitPerHour)) {
		a.fail(w, r, 429, "Too many submissions in a short time. Please try again later.")
		return
	}
	st := a.store.Settings()
	title := oneLine(cleanText(r.FormValue("title"), 200))
	body := cleanText(r.FormValue("body"), 20000)
	category := r.FormValue("category")
	contact := cleanText(r.FormValue("contact"), 200)
	if title == "" {
		a.fail(w, r, 400, "Please add a short summary.")
		return
	}
	if len(st.Categories) > 0 && !slices.Contains(st.Categories, category) {
		category = st.Categories[0]
	}
	atts, err := a.saveUploads(formFiles(r), 0)
	if err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	token := randomToken(24)
	now := time.Now()
	var id int
	err = a.store.Update(func(db *dbFile) error {
		id = db.NextID
		db.NextID++
		db.Tickets = append(db.Tickets, &Ticket{
			ID: id, BoardID: PublicBoardID, Title: title, Body: body, Category: category, Contact: contact,
			Status: "inbox", Pos: minPos(db, "inbox"), TokenHash: hashToken(token),
			Attachments: atts, CreatedAt: now, UpdatedAt: now,
		})
		return nil
	})
	if err != nil {
		a.removeFiles(atts)
		a.fail(w, r, 500, "Saving failed.")
		return
	}
	a.notify(Event{Kind: "new_ticket", TicketID: id, Title: title, Category: category, Body: body, Files: len(atts), At: now})

	statusURL := a.baseURL(r) + "/s/" + token
	if wantsJSON(r) {
		writeJSON(w, 200, map[string]any{"ok": true, "id": id, "status_url": statusURL})
		return
	}
	a.render(w, r, 200, "thanks", pageData{"ID": id, "StatusURL": statusURL})
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	t := a.store.TicketByToken(r.PathValue("token"))
	if t == nil {
		a.render(w, r, 404, "error", pageData{"Title": "Invalid link",
			"Message": "There is no submission for this link. It may have been deleted, or the link is incomplete."})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	token := r.PathValue("token")
	a.render(w, r, 200, "status", pageData{
		"T": t, "Token": token, "TokenQ": url.QueryEscape(token), "Stamp": a.formStamp(),
		"StatusURL": a.baseURL(r) + "/s/" + token,
	})
}

func (a *App) handleSubmitterReply(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.fail(w, r, 403, "Request from another site rejected.")
		return
	}
	token := r.PathValue("token")
	t := a.store.TicketByToken(token)
	if t == nil {
		a.notFound(w, r)
		return
	}
	if err := a.parseUpload(w, r); err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	if r.FormValue("website") != "" || !a.checkFormStamp(r.FormValue("stamp")) {
		a.fail(w, r, 400, "Please wait a moment and send the message again.")
		return
	}
	if !a.checkPoW(r) {
		a.fail(w, r, 400, "The spam check failed or expired. Please reload the page and try again (JavaScript is required).")
		return
	}
	if !a.submitLimit.AllowN(a.clientKey(r), max(1, a.store.Settings().RateLimitPerHour)) {
		a.fail(w, r, 429, "Too many messages in a short time. Please try again later.")
		return
	}
	body := cleanText(r.FormValue("body"), 20000)
	files := formFiles(r)
	if body == "" && len(files) == 0 {
		a.fail(w, r, 400, "The message is empty.")
		return
	}
	atts, err := a.saveUploads(files, len(t.Attachments))
	if err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	now := time.Now()
	err = a.store.Update(func(db *dbFile) error {
		tt := findTicket(db, t.ID)
		if tt == nil {
			return errors.New("gone")
		}
		mid := tt.nextMessageID()
		tt.Messages = append(tt.Messages, Message{ID: mid, Body: body, CreatedAt: now})
		for i := range atts {
			atts[i].MessageID = mid
		}
		tt.Attachments = append(tt.Attachments, atts...)
		tt.UnreadReply = true
		tt.UpdatedAt = now
		return nil
	})
	if err != nil {
		a.removeFiles(atts)
		a.fail(w, r, 500, "Saving failed.")
		return
	}
	a.notify(Event{Kind: "reply", TicketID: t.ID, Title: t.Title, Category: t.Category, Body: body, Files: len(atts), At: now})
	if wantsJSON(r) {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	http.Redirect(w, r, "/s/"+url.PathEscape(token)+"?msg=reply#thread", http.StatusSeeOther)
}

func (a *App) handleRoadmap(w http.ResponseWriter, r *http.Request) {
	st := a.store.Settings()
	if !st.RoadmapEnabled {
		a.notFound(w, r)
		return
	}
	cols := []struct {
		Key, Label string
		Tickets    []*Ticket
	}{{"review", "In review", nil}, {"planned", "Planned", nil}, {"done", "Done", nil}}
	for _, t := range a.store.Tickets() {
		if !t.OnRoadmap || t.BoardID != PublicBoardID {
			continue
		}
		for i := range cols {
			if cols[i].Key == t.Status {
				cols[i].Tickets = append(cols[i].Tickets, t)
			}
		}
	}
	// Done: most recently finished first, max. 25.
	done := cols[2].Tickets
	sort.SliceStable(done, func(i, j int) bool { return done[i].UpdatedAt.After(done[j].UpdatedAt) })
	if len(done) > 25 {
		cols[2].Tickets = done[:25]
	}
	a.render(w, r, 200, "roadmap", pageData{"Cols": cols})
}

func (a *App) handleMedia(w http.ResponseWriter, r *http.Request) {
	t := a.store.Ticket(pathInt(r, "tid"))
	if t == nil {
		http.NotFound(w, r)
		return
	}
	allowed := a.isAdmin(r)
	if !allowed {
		tok := r.URL.Query().Get("t")
		allowed = tok != "" && t.TokenHash != "" && hashToken(tok) == t.TokenHash
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	var att *Attachment
	for i := range t.Attachments {
		if t.Attachments[i].ID == r.PathValue("aid") {
			att = &t.Attachments[i]
		}
	}
	if att == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.store.Uploads, att.File))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", att.Mime)
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	disp := "inline"
	if !att.IsImage() && !att.IsVideo() || r.URL.Query().Get("dl") == "1" {
		disp = "attachment"
	}
	h.Set("Content-Disposition", disp+"; filename*=UTF-8''"+url.PathEscape(att.Name))
	http.ServeContent(w, r, "", att.CreatedAt, f)
}

// oneLine collapses line breaks and control characters into spaces.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' || r < 32 }), " ")
}

// uploadsSize sums the stored attachments (fast enough for a feedback board;
// the store holds every attachment's size).
func (a *App) uploadsSize() int64 {
	var n int64
	a.store.View(func(db *dbFile) {
		for _, t := range db.Tickets {
			for _, at := range t.Attachments {
				n += at.Size
			}
		}
	})
	return n
}
