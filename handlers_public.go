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

// Prefill holds values passed in a link or by the widget:
// /?category=Bug&title=…&context=app+1.4&page=https://…
type Prefill struct {
	Category, Title, Context, Page string
}

func (a *App) prefill(r *http.Request) Prefill {
	q := r.URL.Query()
	st := a.store.Settings()
	p := Prefill{
		Title:   oneLine(cleanText(q.Get("title"), 200)),
		Context: oneLine(cleanText(q.Get("context"), 200)),
		Page:    cleanPageURL(q.Get("page")),
	}
	for _, c := range st.Categories {
		if strings.EqualFold(c, q.Get("category")) {
			p.Category = c
		}
	}
	return p
}

// cleanPageURL keeps http(s) URLs only and drops the fragment.
func cleanPageURL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1000 {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func (a *App) handleForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 200, "submit", pageData{"Stamp": a.formStamp(), "P": a.prefill(r), "Embed": r.URL.Query().Get("embed") == "1"})
}

// parseUpload limits the request size and parses the multipart form.
// uploadSlots limits how many large uploads are processed at the same time.
// Others wait (their bodies are not read yet, so they cost almost no memory).
// Without it, 500 simultaneous 2 MB screenshots peaked at over 3 GB of RAM.
var uploadSlots = make(chan struct{}, 4)

// parseUpload reads a (possibly multipart) form. The returned release must
// be called when the handler is done with the files.
func (a *App) parseUpload(w http.ResponseWriter, r *http.Request) (release func(), err error) {
	release = func() {}
	if r.ContentLength < 0 || r.ContentLength > 256<<10 {
		select {
		case uploadSlots <- struct{}{}:
			release = func() { <-uploadSlots }
		case <-r.Context().Done():
			return release, errors.New("The upload was cancelled.")
		}
	}
	st := a.store.Settings()
	max := int64(st.MaxFiles)*int64(st.MaxFileMB)<<20 + 2<<20
	r.Body = http.MaxBytesReader(w, r.Body, max)
	// Keep at most 1 MB per request in memory; larger files go to temp files.
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return release, fmt.Errorf("Upload too large (max. %d files of %d MB each)", st.MaxFiles, st.MaxFileMB)
		}
		if errors.Is(err, http.ErrNotMultipart) {
			return release, r.ParseForm()
		}
		return release, errors.New("Could not read the form")
	}
	return release, nil
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
	release, err := a.parseUpload(w, r)
	defer release()
	if err != nil {
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
	context := oneLine(cleanText(r.FormValue("context"), 200))
	pageURL := cleanPageURL(r.FormValue("page"))
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
			ID: id, BoardID: PublicBoardID, Title: title, Body: body, Category: category, Contact: contact, Context: context, PageURL: pageURL,
			Status: "inbox", Pos: minPos(db, "inbox"), TokenHash: hashToken(token),
			ShareMedia:  r.FormValue("share_media") == "1",
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
	type step struct {
		Label            string
		Reached, Current bool
	}
	var steps []step
	reached := true
	for _, s := range a.store.Settings().StatusesFor(t.Status) {
		if s.Key == "rejected" {
			continue
		}
		cur := s.Key == t.Status
		steps = append(steps, step{s.PublicLabel, reached, cur})
		if cur {
			reached = false
		}
	}
	a.render(w, r, 200, "status", pageData{
		"T": t, "Steps": steps, "Token": token, "TokenQ": url.QueryEscape(token), "Stamp": a.formStamp(),
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
	release, err := a.parseUpload(w, r)
	defer release()
	if err != nil {
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
	// read settings before taking the write lock (Settings() locks too)
	wantPublic := r.FormValue("public") == "1" && a.store.Settings().RoadmapEnabled
	err = a.store.Update(func(db *dbFile) error {
		tt := findTicket(db, t.ID)
		if tt == nil {
			return errors.New("gone")
		}
		mid := tt.nextMessageID()
		public := wantPublic && tt.HasPublicPage()
		tt.Messages = append(tt.Messages, Message{ID: mid, Body: body, Public: public, CreatedAt: now})
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
	type rmCol struct {
		Key, Label string
		Tickets    []*Ticket
		Matches    int
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	words := strings.Fields(strings.ToLower(q))
	showAll := r.URL.Query().Get("all") == "1" || len(words) > 0
	tickets := slices.DeleteFunc(a.store.Tickets(), func(t *Ticket) bool { return !t.VisibleOnRoadmap() })
	// Everything stays on the page so the live search can widen again;
	// non-matching items are only hidden.
	miss := map[int]bool{}
	for _, t := range tickets {
		text := t.PublicSearchText()
		for _, w := range words {
			if !strings.Contains(text, w) {
				miss[t.ID] = true
				break
			}
		}
	}
	cols := []rmCol{{Key: "review", Label: "In review"}, {Key: "planned", Label: "Planned"}}
	if st.DevStage || slices.ContainsFunc(tickets, func(t *Ticket) bool { return t.Status == DevStatus }) {
		cols = append(cols, rmCol{Key: DevStatus, Label: "In development"})
	}
	cols = append(cols, rmCol{Key: "done", Label: "Done"})
	for _, t := range tickets {
		for i := range cols {
			if cols[i].Key == t.Status {
				cols[i].Tickets = append(cols[i].Tickets, t)
				if !miss[t.ID] {
					cols[i].Matches++
				}
			}
		}
	}
	// Done: most recently finished first. Older ones are on the page (so the
	// live search finds them) but folded away behind "Show older".
	const recentDone = 25
	last := len(cols) - 1
	done := cols[last].Tickets
	sort.SliceStable(done, func(i, j int) bool { return done[i].UpdatedAt.After(done[j].UpdatedAt) })
	limit := recentDone
	if showAll {
		limit = len(done)
	}
	total := 0
	for _, c := range cols {
		total += c.Matches
	}
	a.render(w, r, 200, "roadmap", pageData{"Cols": cols, "Q": q, "Limit": limit, "Miss": miss,
		"Older": max(0, len(done)-limit), "Total": total})
}

// handleRoadmapItem is the public detail page of a roadmap card. It exists
// only when an admin switched it on for that card; media only with consent.
func (a *App) handleRoadmapItem(w http.ResponseWriter, r *http.Request) {
	t := a.store.Ticket(pathInt(r, "id"))
	if !a.store.Settings().RoadmapEnabled || t == nil || !t.HasPublicPage() {
		a.notFound(w, r)
		return
	}
	a.render(w, r, 200, "roadmapitem", pageData{"T": t, "StatusLabel": publicStatusLabel(t.Status),
		"Stamp": a.formStamp(), "Comments": !a.store.Settings().NoPublicComments})
}

// handleComment: a visitor comments on a roadmap item. Same protection as
// the feedback form: honeypot, minimum fill time, proof of work, rate limit.
func (a *App) handleComment(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.fail(w, r, 403, "Request from another site rejected.")
		return
	}
	st := a.store.Settings()
	t := a.store.Ticket(pathInt(r, "id"))
	if !st.RoadmapEnabled || st.NoPublicComments || t == nil || !t.HasPublicPage() {
		a.notFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var perr error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		perr = r.ParseMultipartForm(64 << 10)
	} else {
		perr = r.ParseForm()
	}
	if perr != nil {
		a.fail(w, r, 400, "The comment is too long.")
		return
	}
	if r.FormValue("website") != "" || !a.checkFormStamp(r.FormValue("stamp")) {
		a.fail(w, r, 400, "Please wait a moment and send the comment again.")
		return
	}
	if !a.checkPoW(r) {
		a.fail(w, r, 400, "The spam check failed or expired. Please reload the page and try again (JavaScript is required).")
		return
	}
	if !a.submitLimit.AllowN(a.clientKey(r), max(1, st.RateLimitPerHour)) {
		a.fail(w, r, 429, "Too many comments in a short time. Please try again later.")
		return
	}
	body := cleanText(r.FormValue("body"), 5000)
	name := oneLine(cleanText(r.FormValue("name"), 40))
	if strings.TrimSpace(body) == "" {
		a.fail(w, r, 400, "The comment is empty.")
		return
	}
	now := time.Now()
	err := a.store.Update(func(db *dbFile) error {
		tt := findTicket(db, t.ID)
		if tt == nil || !tt.HasPublicPage() {
			return errNotFound
		}
		tt.Messages = append(tt.Messages, Message{ID: tt.nextMessageID(), Visitor: true, Public: true, Author: name, Body: body, CreatedAt: now})
		tt.UnreadReply = true
		return nil
	})
	if err != nil {
		a.notFound(w, r)
		return
	}
	a.notify(Event{Kind: "comment", TicketID: t.ID, Title: t.Title, Category: t.Category, Body: body, At: now})
	if wantsJSON(r) {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/roadmap/%d#comments", t.ID), http.StatusSeeOther)
}

// handleShareMedia lets the submitter give or withdraw consent to show
// their screenshots and videos on the public detail page.
func (a *App) handleShareMedia(w http.ResponseWriter, r *http.Request) {
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
	share := r.FormValue("share_media") == "1"
	a.store.Update(func(db *dbFile) error {
		if x := findTicket(db, t.ID); x != nil {
			x.ShareMedia = share
		}
		return nil
	})
	http.Redirect(w, r, "/s/"+url.PathEscape(token)+"#sharing", http.StatusSeeOther)
}

func (a *App) handleMedia(w http.ResponseWriter, r *http.Request) {
	t := a.store.Ticket(pathInt(r, "tid"))
	if t == nil {
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
	allowed := a.isAdmin(r)
	if !allowed {
		tok := r.URL.Query().Get("t")
		allowed = tok != "" && t.TokenHash != "" && hashToken(tok) == t.TokenHash
	}
	public := false
	if !allowed && a.store.Settings().RoadmapEnabled && t.HasPublicPage() {
		public = slices.ContainsFunc(t.PublicMedia(), func(x Attachment) bool { return x.ID == att.ID })
		allowed = public
	}
	if !allowed {
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
	if public {
		// short, so withdrawn consent takes effect soon
		h.Set("Cache-Control", "public, max-age=300")
	}
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
