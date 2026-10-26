package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

var errNotFound = errors.New("not found")

func (a *App) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.store.HasUsers() {
		a.render(w, r, 200, "setup", nil)
		return
	}
	if !a.isAdmin(r) {
		a.render(w, r, 200, "login", nil)
		return
	}
	board := a.store.Board(boardParam(r))
	if board == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	type col struct {
		Key, Label string
		Tickets    []*Ticket
	}
	var cols []col
	for _, s := range Statuses {
		cols = append(cols, col{Key: s.Key, Label: s.Label})
	}
	public := 0
	unread := 0
	for _, t := range a.store.Tickets() {
		if t.BoardID != board.ID {
			continue
		}
		if t.UnreadReply {
			unread++
		}
		if t.VisibleOnRoadmap() {
			public++
		}
		for i := range cols {
			if cols[i].Key == t.Status {
				cols[i].Tickets = append(cols[i].Tickets, t)
			}
		}
	}
	if !a.store.Settings().DevStage {
		cols = slices.DeleteFunc(cols, func(c col) bool { return c.Key == DevStatus && len(c.Tickets) == 0 })
	}
	a.render(w, r, 200, "board", pageData{"Cols": cols, "Unread": unread, "Public": public, "Board": board, "Tabs": a.boardTabs()})
}

func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.fail(w, r, 403, "Request from another site rejected.")
		return
	}
	again := func(msg string) {
		a.render(w, r, 400, "setup", pageData{"Error": msg, "NewUsername": r.FormValue("username"), "NewDisplay": r.FormValue("display_name")})
	}
	if a.setupCode != "" && subtle.ConstantTimeCompare([]byte(strings.ToUpper(strings.TrimSpace(r.FormValue("setup_code")))), []byte(a.setupCode)) != 1 {
		time.Sleep(700 * time.Millisecond)
		again("Wrong setup code. You find it in the server log (docker logs feedbacklooped).")
		return
	}
	if err := validPassword(r.FormValue("password"), r.FormValue("password2")); err != nil {
		again(err.Error())
		return
	}
	h, err := hashPassword(r.FormValue("password"))
	if err != nil {
		a.fail(w, r, 500, err.Error())
		return
	}
	base := a.baseURL(r)
	var u *User
	err = a.store.Update(func(db *dbFile) error {
		if len(db.Users) > 0 {
			return errors.New("already set up")
		}
		var err error
		if u, err = addUser(db, r.FormValue("username"), r.FormValue("display_name"), h); err != nil {
			return err
		}
		if db.Settings.BaseURL == "" {
			db.Settings.BaseURL = base
		}
		if t := strings.TrimSpace(r.FormValue("title")); t != "" {
			db.Settings.Title = cleanText(t, 80)
		}
		return nil
	})
	if err != nil {
		if a.store.HasUsers() {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
		again(err.Error())
		return
	}
	a.setupCode = "" // only needed once
	a.setSession(w, r, u)
	http.Redirect(w, r, "/admin/settings?msg=saved", http.StatusSeeOther)
}

func utf8Len(s string) int { return len([]rune(s)) }

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		a.fail(w, r, 403, "Request from another site rejected.")
		return
	}
	if !a.loginLimit.Allow(a.clientKey(r)) {
		a.render(w, r, 429, "login", pageData{"Error": "Too many attempts. Please try again in 15 minutes."})
		return
	}
	uname := normalizeUsername(r.FormValue("username"))
	if !a.userLimit.Check(uname) {
		a.render(w, r, 429, "login", pageData{"Error": "Too many failed attempts for this account. Please try again later.", "NewUsername": uname})
		return
	}
	u := a.store.UserByName(uname)
	if u == nil || !checkPassword(u.PassHash, r.FormValue("password")) {
		a.userLimit.Allow(uname) // count the failure
		if u == nil {
			checkPassword(dummyHash, r.FormValue("password")) // same timing for unknown users
		}
		time.Sleep(700 * time.Millisecond)
		a.render(w, r, 401, "login", pageData{"Error": "Wrong username or password.", "NewUsername": r.FormValue("username")})
		return
	}
	a.store.Update(func(db *dbFile) error {
		if x := findUser(db, u.ID); x != nil {
			x.LastLoginAt = time.Now()
		}
		return nil
	})
	a.setSession(w, r, u)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.clearSession(w, r)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) handleNewCard(w http.ResponseWriter, r *http.Request) {
	title := oneLine(cleanText(r.FormValue("title"), 200))
	boardID, _ := strconv.Atoi(r.FormValue("board"))
	if a.store.Board(boardID) == nil {
		boardID = PublicBoardID
	}
	if title == "" {
		http.Redirect(w, r, boardURL(boardID), http.StatusSeeOther)
		return
	}
	st := a.store.Settings()
	cat := r.FormValue("category")
	if !slices.Contains(st.Categories, cat) && len(st.Categories) > 0 {
		cat = st.Categories[0]
	}
	now := time.Now()
	var id int
	a.store.Update(func(db *dbFile) error {
		id = db.NextID
		db.NextID++
		db.Tickets = append(db.Tickets, &Ticket{ID: id, BoardID: boardID, Title: title, Category: cat, Status: "inbox",
			Pos: minPos(db, "inbox"), Internal: true, CreatedAt: now, UpdatedAt: now})
		return nil
	})
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d", id), http.StatusSeeOther)
}

// handleMove sets status and column order after drag & drop.
func (a *App) handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Order  []int  `json:"order"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || !validStatus(req.Status) {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	err := a.store.Update(func(db *dbFile) error {
		t := findTicket(db, req.ID)
		if t == nil {
			return errNotFound
		}
		if t.Status != req.Status {
			t.Status = req.Status
			t.UpdatedAt = time.Now()
		}
		if len(req.Order) == 0 { // moved from the card page: put it on top
			t.Pos = minPos(db, req.Status)
		}
		for i, id := range req.Order {
			if tt := findTicket(db, id); tt != nil && tt.Status == req.Status {
				tt.Pos = float64(i)
			}
		}
		return nil
	})
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleTicket(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	t := a.store.Ticket(id)
	if t == nil {
		a.notFound(w, r)
		return
	}
	if t.UnreadReply {
		a.store.Update(func(db *dbFile) error {
			if tt := findTicket(db, id); tt != nil {
				tt.UnreadReply = false
			}
			return nil
		})
	}
	a.render(w, r, 200, "ticket", pageData{"T": t, "Board": a.store.Board(t.BoardID), "Boards": a.store.Boards(),
		"Statuses": a.store.Settings().StatusesFor(t.Status)})
}

func (a *App) handleTicketUpdate(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	st := a.store.Settings()
	err := a.store.Update(func(db *dbFile) error {
		t := findTicket(db, id)
		if t == nil {
			return errNotFound
		}
		if v := oneLine(cleanText(r.FormValue("title"), 200)); v != "" {
			t.Title = v
		}
		if t.Internal {
			t.Body = cleanText(r.FormValue("body"), 20000)
		}
		if c := r.FormValue("category"); slices.Contains(st.Categories, c) {
			t.Category = c
		}
		if s := r.FormValue("status"); validStatus(s) && s != t.Status {
			t.Status = s
			t.Pos = minPos(db, s)
		}
		t.Labels = nil
		for _, l := range strings.Split(r.FormValue("labels"), ",") {
			if l = cleanText(l, 40); l != "" && !slices.Contains(t.Labels, l) {
				t.Labels = append(t.Labels, l)
			}
		}
		if b, err := strconv.Atoi(r.FormValue("board")); err == nil && b != t.BoardID && findBoard(db, b) != nil {
			t.BoardID = b
			t.Pos = minPos(db, t.Status)
		}
		t.OnRoadmap = r.FormValue("on_roadmap") == "1" && t.BoardID == PublicBoardID
		t.RoadmapTitle = cleanText(r.FormValue("roadmap_title"), 200)
		t.PublicDetails = t.OnRoadmap && r.FormValue("public_details") == "1"
		t.PublicBody = cleanText(r.FormValue("public_body"), 20000)
		if t.Internal {
			// the team's own card: the team decides; for submissions only the submitter can
			t.ShareMedia = r.FormValue("share_media") == "1"
		}
		t.UpdatedAt = time.Now()
		return nil
	})
	if err != nil {
		a.notFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d?msg=saved", id), http.StatusSeeOther)
}

func (a *App) handleTeamMessage(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	t := a.store.Ticket(id)
	if t == nil {
		a.notFound(w, r)
		return
	}
	if err := a.parseUpload(w, r); err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	body := cleanText(r.FormValue("body"), 20000)
	files := formFiles(r)
	if body == "" && len(files) == 0 {
		http.Redirect(w, r, fmt.Sprintf("/admin/t/%d", id), http.StatusSeeOther)
		return
	}
	atts, err := a.saveUploads(files, len(t.Attachments))
	if err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	author := a.currentUser(r).Name() // outside the lock: currentUser reads the store
	public := r.FormValue("public") == "1"
	err = a.store.Update(func(db *dbFile) error {
		tt := findTicket(db, id)
		if tt == nil {
			return errNotFound
		}
		mid := tt.nextMessageID()
		tt.Messages = append(tt.Messages, Message{ID: mid, FromTeam: true, Author: author, Body: body, Public: public && tt.HasPublicPage(), CreatedAt: time.Now()})
		for i := range atts {
			atts[i].MessageID = mid
		}
		tt.Attachments = append(tt.Attachments, atts...)
		tt.UpdatedAt = time.Now()
		return nil
	})
	if err != nil {
		a.removeFiles(atts)
		a.notFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d?msg=sent#thread", id), http.StatusSeeOther)
}

func (a *App) handleMessageDelete(w http.ResponseWriter, r *http.Request) {
	id, mid := pathInt(r, "id"), pathInt(r, "mid")
	var removed []Attachment
	a.store.Update(func(db *dbFile) error {
		t := findTicket(db, id)
		if t == nil {
			return errNotFound
		}
		t.Messages = slices.DeleteFunc(t.Messages, func(m Message) bool { return m.ID == mid })
		t.Attachments = slices.DeleteFunc(t.Attachments, func(at Attachment) bool {
			if mid != 0 && at.MessageID == mid {
				removed = append(removed, at)
				return true
			}
			return false
		})
		return nil
	})
	a.removeFiles(removed)
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d?msg=deleted#thread", id), http.StatusSeeOther)
}

func (a *App) handleNote(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	body := cleanText(r.FormValue("body"), 20000)
	author := a.currentUser(r).Name()
	if body != "" {
		a.store.Update(func(db *dbFile) error {
			t := findTicket(db, id)
			if t == nil {
				return errNotFound
			}
			t.Notes = append(t.Notes, Note{ID: t.nextNoteID(), Author: author, Body: body, CreatedAt: time.Now()})
			return nil
		})
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d#notes", id), http.StatusSeeOther)
}

func (a *App) handleNoteDelete(w http.ResponseWriter, r *http.Request) {
	id, nid := pathInt(r, "id"), pathInt(r, "nid")
	a.store.Update(func(db *dbFile) error {
		t := findTicket(db, id)
		if t == nil {
			return errNotFound
		}
		t.Notes = slices.DeleteFunc(t.Notes, func(n Note) bool { return n.ID == nid })
		return nil
	})
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d#notes", id), http.StatusSeeOther)
}

func (a *App) handleAttachmentDelete(w http.ResponseWriter, r *http.Request) {
	id, aid := pathInt(r, "id"), r.PathValue("aid")
	var removed []Attachment
	a.store.Update(func(db *dbFile) error {
		t := findTicket(db, id)
		if t == nil {
			return errNotFound
		}
		t.Attachments = slices.DeleteFunc(t.Attachments, func(at Attachment) bool {
			if at.ID == aid {
				removed = append(removed, at)
				return true
			}
			return false
		})
		return nil
	})
	a.removeFiles(removed)
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d?msg=deleted", id), http.StatusSeeOther)
}

func (a *App) handleTicketDelete(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	back := "/admin"
	if t := a.store.Ticket(id); t != nil {
		back = boardURL(t.BoardID)
	}
	var removed []Attachment
	a.store.Update(func(db *dbFile) error {
		db.Tickets = slices.DeleteFunc(db.Tickets, func(t *Ticket) bool {
			if t.ID == id {
				removed = t.Attachments
				return true
			}
			return false
		})
		return nil
	})
	a.removeFiles(removed)
	sep := "?"
	if back != "/admin" {
		sep = "&"
	}
	http.Redirect(w, r, back+sep+"msg=deleted", http.StatusSeeOther)
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 200, "settings", nil)
}

func formInt(r *http.Request, key string, def, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(key)))
	if err != nil {
		return def
	}
	return max0(min, minI(n, max))
}

func max0(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *App) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	a.store.Update(func(db *dbFile) error {
		s := &db.Settings
		s.Title = cleanText(r.FormValue("title"), 80)
		if s.Title == "" {
			s.Title = "Feedback"
		}
		s.Intro = cleanText(r.FormValue("intro"), 2000)
		a.saveBrandForm(r, &s.Brand)
		s.BaseURL = strings.TrimRight(cleanURL(r.FormValue("base_url")), "/")
		var cats []string
		for _, c := range strings.Split(r.FormValue("categories"), "\n") {
			if c = cleanText(c, 40); c != "" && !slices.Contains(cats, c) {
				cats = append(cats, c)
			}
		}
		if len(cats) > 0 {
			s.Categories = cats
		}
		s.RoadmapEnabled = r.FormValue("roadmap_enabled") == "1"
		s.DevStage = r.FormValue("dev_stage") == "1"
		s.NoPublicComments = r.FormValue("public_comments") != "1"
		s.ShowTeamNames = r.FormValue("show_team_names") == "1"
		s.CaptchaEnabled = r.FormValue("captcha_enabled") == "1"
		s.CaptchaDifficulty = formInt(r, "captcha_difficulty", s.CaptchaDifficulty, 1000, 1000000)
		s.RateLimitPerHour = formInt(r, "rate_limit_per_hour", s.RateLimitPerHour, 1, 1000)
		s.RetentionDays = formInt(r, "retention_days", s.RetentionDays, 0, 3650)
		s.MaxStorageMB = formInt(r, "max_storage_mb", s.MaxStorageMB, 0, 10000000)
		s.AllowSelfDelete = r.FormValue("allow_self_delete") == "1"
		s.SystemFont = r.FormValue("system_font") == "1"
		s.SearchIndexing = r.FormValue("search_indexing") == "1"
		s.MetaDescription = oneLine(cleanText(r.FormValue("meta_description"), 300))
		s.EmbedOrigins = parseOrigins(r.FormValue("embed_origins"))
		s.ImprintURL = cleanURL(r.FormValue("imprint_url"))
		s.ImprintText = cleanText(r.FormValue("imprint_text"), 20000)
		s.PrivacyURL = cleanURL(r.FormValue("privacy_url"))
		s.PrivacyText = cleanText(r.FormValue("privacy_text"), 50000)
		s.RoadmapIntro = cleanText(r.FormValue("roadmap_intro"), 1000)
		s.MaxFileMB = formInt(r, "max_file_mb", s.MaxFileMB, 1, 2048)
		s.MaxFiles = formInt(r, "max_files", s.MaxFiles, 1, 30)
		s.MaxRecordSec = formInt(r, "max_record_sec", s.MaxRecordSec, 10, 1800)

		s.NtfyURL = cleanURL(r.FormValue("ntfy_url"))
		if v := strings.TrimSpace(r.FormValue("ntfy_token")); v != "" {
			s.NtfyToken = v
		}
		if r.FormValue("ntfy_token_clear") == "1" {
			s.NtfyToken = ""
		}
		s.WebhookURL = cleanURL(r.FormValue("webhook_url"))
		s.SMTPHost = strings.TrimSpace(r.FormValue("smtp_host"))
		s.SMTPPort = formInt(r, "smtp_port", 587, 1, 65535)
		s.SMTPUser = strings.TrimSpace(r.FormValue("smtp_user"))
		if v := r.FormValue("smtp_pass"); v != "" {
			s.SMTPPass = v
		}
		if r.FormValue("smtp_pass_clear") == "1" {
			s.SMTPPass = ""
		}
		s.SMTPFrom = oneLine(strings.TrimSpace(r.FormValue("smtp_from")))
		s.SMTPTo = oneLine(strings.TrimSpace(r.FormValue("smtp_to")))
		return nil
	})
	http.Redirect(w, r, "/admin/settings?msg=saved", http.StatusSeeOther)
}

// cleanURL only allows http(s) URLs.
func cleanURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return s
	}
	return ""
}

func (a *App) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	st := a.withAdminRecipients(a.store.Settings())
	if to := strings.TrimSpace(r.FormValue("test_to")); to != "" {
		if !emailRe.MatchString(to) {
			a.render(w, r, 400, "settings", pageData{"TestResult": []string{"Error: “" + to + "” is not a valid email address."}, "TestTo": to})
			return
		}
		st.SMTPTo = to
	}
	var channels []string
	if st.NtfyURL != "" {
		channels = append(channels, "ntfy")
	}
	if st.WebhookURL != "" {
		channels = append(channels, "webhook")
	}
	if st.SMTPHost != "" && st.SMTPTo != "" {
		channels = append(channels, "email to "+st.SMTPTo)
	}
	if len(channels) == 0 {
		msg := "No channel configured. Set up ntfy, a webhook or an SMTP server above and save first."
		if st.SMTPHost == "" && r.FormValue("test_to") != "" {
			msg = "To test email, enter an SMTP server above and save first."
		}
		a.render(w, r, 200, "settings", pageData{"TestResult": []string{msg}, "TestTo": r.FormValue("test_to")})
		return
	}
	e := Event{Kind: "new_ticket", TicketID: 0, Title: "Test notification", Category: "Test",
		Body: "If you can read this, notifications are working.", At: time.Now()}
	if st.BaseURL != "" {
		e.URL = st.BaseURL + "/admin"
	}
	res := []string{"Sent via: " + strings.Join(channels, ", ")}
	errs := a.sendAll(st, e)
	for _, err := range errs {
		res = append(res, "Error: "+err.Error())
	}
	if len(errs) == 0 {
		res = append(res, "All channels were reached successfully.")
	}
	a.render(w, r, 200, "settings", pageData{"TestResult": res, "TestTo": r.FormValue("test_to")})
}

// handleExport returns all tickets as JSON (without credentials), e.g. as a backup.
func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	tickets := a.store.Tickets()
	for _, t := range tickets {
		t.TokenHash = ""
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="feedback-export-`+time.Now().Format("2006-01-02")+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"exported_at": time.Now(), "tickets": tickets})
}

// handleMessageVisibility makes a team message public or private, and lets
// admins hide any public message (moderation). Messages from the submitter
// are never made public by the team: only they can decide that.
func (a *App) handleMessageVisibility(w http.ResponseWriter, r *http.Request) {
	id, mid := pathInt(r, "id"), pathInt(r, "mid")
	public := r.FormValue("public") == "1"
	err := a.store.Update(func(db *dbFile) error {
		t := findTicket(db, id)
		if t == nil {
			return errNotFound
		}
		for i := range t.Messages {
			m := &t.Messages[i]
			if m.ID != mid {
				continue
			}
			if public && (!m.FromTeam || !t.HasPublicPage()) {
				return errNotFound
			}
			m.Public = public
			return nil
		}
		return errNotFound
	})
	if err != nil {
		a.notFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/t/%d#m%d", id, mid), http.StatusSeeOther)
}
