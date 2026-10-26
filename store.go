package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Board columns, in display order.
type StatusDef struct {
	Key, Label, PublicLabel string
}

// DevStatus is the optional "In development" stage between Planned and Done.
const DevStatus = "development"

var Statuses = []StatusDef{
	{"inbox", "Inbox", "Received"},
	{"review", "In review", "In review"},
	{"planned", "Planned", "Planned"},
	{DevStatus, "In development", "In development"},
	{"done", "Done", "Done"},
	{"rejected", "Declined", "Declined"},
}

func validStatus(s string) bool {
	for _, st := range Statuses {
		if st.Key == s {
			return true
		}
	}
	return false
}

// StatusesFor returns the stages in use. The optional development stage is
// included when it is switched on, or when keep is in it (so cards that are
// already there never vanish after it is switched off).
func (st Settings) StatusesFor(keep string) []StatusDef {
	var out []StatusDef
	for _, s := range Statuses {
		if s.Key == DevStatus && !st.DevStage && keep != DevStatus {
			continue
		}
		out = append(out, s)
	}
	return out
}

func statusLabel(s string) string {
	for _, st := range Statuses {
		if st.Key == s {
			return st.Label
		}
	}
	return s
}

func publicStatusLabel(s string) string {
	for _, st := range Statuses {
		if st.Key == s {
			return st.PublicLabel
		}
	}
	return s
}

type Attachment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	File      string    `json:"file"`
	MessageID int       `json:"message_id,omitempty"` // 0 = belongs to the original submission
	CreatedAt time.Time `json:"created_at"`
}

func (a Attachment) IsImage() bool { return strings.HasPrefix(a.Mime, "image/") }
func (a Attachment) IsVideo() bool { return strings.HasPrefix(a.Mime, "video/") }

// Message is visible to both submitter and team (thread on the status page).
// Public messages also show on the item's roadmap page. Visitor messages are
// comments from the public roadmap (always public, no attachments).
type Message struct {
	ID        int       `json:"id"`
	FromTeam  bool      `json:"from_team"`
	Visitor   bool      `json:"visitor,omitempty"`
	Public    bool      `json:"public,omitempty"`
	Author    string    `json:"author,omitempty"` // admin display name, or the visitor's chosen name
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Note is internal only.
type Note struct {
	ID        int       `json:"id"`
	Author    string    `json:"author,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Ticket struct {
	ID           int      `json:"id"`
	BoardID      int      `json:"board_id"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Category     string   `json:"category"`
	Status       string   `json:"status"`
	Pos          float64  `json:"pos"`
	Contact      string   `json:"contact,omitempty"`
	Context      string   `json:"context,omitempty"`  // from a prefilled link or the widget, e.g. "app 1.4"
	PageURL      string   `json:"page_url,omitempty"` // page the feedback was sent from
	Labels       []string `json:"labels,omitempty"`
	OnRoadmap    bool     `json:"on_roadmap"`
	RoadmapTitle string   `json:"roadmap_title,omitempty"`
	// Public detail page at /roadmap/{id}: description (or PublicBody) and,
	// only with the submitter's consent (ShareMedia), their screenshots and videos.
	PublicDetails bool         `json:"public_details,omitempty"`
	PublicBody    string       `json:"public_body,omitempty"`
	ShareMedia    bool         `json:"share_media,omitempty"`
	Internal      bool         `json:"internal"` // created by the admin, no status link
	TokenHash     string       `json:"token_hash,omitempty"`
	Attachments   []Attachment `json:"attachments,omitempty"`
	Messages      []Message    `json:"messages,omitempty"`
	Notes         []Note       `json:"notes,omitempty"`
	UnreadReply   bool         `json:"unread_reply"` // submitter replied, admin hasn't looked yet
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

func (t *Ticket) PublicTitle() string {
	if strings.TrimSpace(t.RoadmapTitle) != "" {
		return t.RoadmapTitle
	}
	return t.Title
}

// roadmapStatuses are the columns the public roadmap shows.
var roadmapStatuses = []string{"review", "planned", DevStatus, "done"}

// VisibleOnRoadmap: marked for the roadmap and in a column the roadmap shows.
func (t *Ticket) VisibleOnRoadmap() bool {
	return t.OnRoadmap && t.BoardID == PublicBoardID && slices.Contains(roadmapStatuses, t.Status)
}

// HasPublicPage: the item page at /roadmap/{id} exists (roadmap switched on is checked by the caller).
// It always shows the public title and public comments; the description only with PublicDetails.
func (t *Ticket) HasPublicPage() bool { return t.VisibleOnRoadmap() }

// PublicMessages: what the roadmap page shows of the conversation.
func (t *Ticket) PublicMessages() []Message {
	var out []Message
	for _, m := range t.Messages {
		if m.Public {
			out = append(out, m)
		}
	}
	return out
}

func (t *Ticket) PublicText() string {
	if strings.TrimSpace(t.PublicBody) != "" {
		return t.PublicBody
	}
	return t.Body
}

// PublicSearchText is what the roadmap search looks at: only public text.
func (t *Ticket) PublicSearchText() string {
	s := t.PublicTitle() + " " + t.Category
	if t.PublicDetails {
		s += " " + t.PublicText()
	}
	return strings.ToLower(s)
}

// SharableMedia: screenshots and videos from the original submission (never
// from the conversation). Whether they may be shown is decided by ShareMedia.
func (t *Ticket) SharableMedia() []Attachment {
	var out []Attachment
	for _, a := range t.Attachments {
		if a.MessageID == 0 && (a.IsImage() || a.IsVideo()) {
			out = append(out, a)
		}
	}
	return out
}

// PublicMedia: what the public detail page shows.
func (t *Ticket) PublicMedia() []Attachment {
	if !t.ShareMedia || !t.PublicDetails {
		return nil
	}
	return t.SharableMedia()
}

func (t *Ticket) AttachmentsFor(messageID int) []Attachment {
	var out []Attachment
	for _, a := range t.Attachments {
		if a.MessageID == messageID {
			out = append(out, a)
		}
	}
	return out
}

func (t *Ticket) nextMessageID() int {
	max := 0
	for _, m := range t.Messages {
		if m.ID > max {
			max = m.ID
		}
	}
	return max + 1
}

func (t *Ticket) nextNoteID() int {
	max := 0
	for _, n := range t.Notes {
		if n.ID > max {
			max = n.ID
		}
	}
	return max + 1
}

type Settings struct {
	Title          string   `json:"title"`
	Intro          string   `json:"intro"`
	BaseURL        string   `json:"base_url"`
	Categories     []string `json:"categories"`
	RoadmapEnabled bool     `json:"roadmap_enabled"`
	RoadmapIntro   string   `json:"roadmap_intro"`
	DevStage       bool     `json:"dev_stage"` // extra "In development" column after Planned
	// Visitors can comment on roadmap items. Stored inverted so it is on by default.
	NoPublicComments bool `json:"no_public_comments,omitempty"`
	MaxFileMB        int  `json:"max_file_mb"`
	MaxFiles         int  `json:"max_files"`
	MaxRecordSec     int  `json:"max_record_sec"`

	NtfyURL    string `json:"ntfy_url"`
	NtfyToken  string `json:"ntfy_token"`
	WebhookURL string `json:"webhook_url"`
	SMTPHost   string `json:"smtp_host"`
	SMTPPort   int    `json:"smtp_port"`
	SMTPUser   string `json:"smtp_user"`
	SMTPPass   string `json:"smtp_pass"`
	SMTPFrom   string `json:"smtp_from"`
	SMTPTo     string `json:"smtp_to"`

	Brand Brand `json:"brand"`

	ShowTeamNames     bool `json:"show_team_names"`    // status page shows "Anna (Team)" instead of "Team"
	CaptchaEnabled    bool `json:"captcha_enabled"`    // proof-of-work check on public forms
	CaptchaDifficulty int  `json:"captcha_difficulty"` // max. number to search; higher = slower
	RateLimitPerHour  int  `json:"rate_limit_per_hour"`
	RetentionDays     int  `json:"retention_days"`    // delete Done/Declined cards after N days, 0 = never
	MaxStorageMB      int  `json:"max_storage_mb"`    // total size of all uploads; new uploads are refused above it
	AllowSelfDelete   bool `json:"allow_self_delete"` // submitters may delete their feedback via the status link
	SystemFont        bool `json:"system_font"`       // use the OS font instead of the bundled one

	SearchIndexing  bool   `json:"search_indexing"`  // let search engines index form, roadmap, imprint, privacy
	MetaDescription string `json:"meta_description"` // search results and link previews; empty = intro text

	EmbedOrigins []string `json:"embed_origins"` // sites allowed to embed the form (widget)

	ImprintURL  string `json:"imprint_url"`
	ImprintText string `json:"imprint_text"`
	PrivacyURL  string `json:"privacy_url"`
	PrivacyText string `json:"privacy_text"`

	AdminHash string `json:"admin_hash,omitempty"` // legacy single-admin password, migrated to Users
	Secret    string `json:"secret"`
}

func defaultSettings() Settings {
	return Settings{
		Title:          "Feedback",
		Intro:          "Found a bug or have an idea? Tell us briefly what happened. Screenshots or a short screen recording help a lot. No account or email address needed.",
		Categories:     []string{"Bug", "Idea", "Question"},
		RoadmapEnabled: true,
		RoadmapIntro:   "What we are working on and what shipped recently.",
		MaxFileMB:      100,
		MaxFiles:       10,
		MaxRecordSec:   180,
		SMTPPort:       587,
		Brand:          defaultBrand(),

		CaptchaEnabled:    true,
		CaptchaDifficulty: 50000,
		RateLimitPerHour:  10,
		MaxStorageMB:      5120,
		AllowSelfDelete:   true,
	}
}

type dbFile struct {
	NextID      int       `json:"next_id"`
	Tickets     []*Ticket `json:"tickets"`
	NextUserID  int       `json:"next_user_id"`
	Users       []*User   `json:"users"`
	NextBoardID int       `json:"next_board_id"`
	Boards      []*Board  `json:"boards"`
	Settings    Settings  `json:"settings"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	Uploads string
	db      dbFile
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "uploads"), 0o750); err != nil {
		return nil, err
	}
	// Large uploads are buffered in temp files while they arrive. Keep those
	// on the data volume (the container's own /tmp may be small or missing)
	// and clear leftovers from a crash.
	if os.Getenv("TMPDIR") == "" {
		tmp := filepath.Join(dataDir, "tmp")
		os.RemoveAll(tmp)
		if err := os.MkdirAll(tmp, 0o700); err != nil {
			return nil, err
		}
		os.Setenv("TMPDIR", tmp)
	}
	s := &Store{path: filepath.Join(dataDir, "feedback.json"), Uploads: filepath.Join(dataDir, "uploads")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.db = dbFile{NextID: 1, Settings: defaultSettings()}
	case err != nil:
		return nil, err
	default:
		s.db.Settings = defaultSettings()
		if err := json.Unmarshal(b, &s.db); err != nil {
			return nil, err
		}
		if s.db.NextID < 1 {
			s.db.NextID = 1
		}
	}
	migrateUsers(&s.db)
	migrateBoards(&s.db)
	if s.db.Settings.Secret == "" {
		s.db.Settings.Secret = randomToken(32)
	}
	return s, s.saveLocked()
}

// saveLocked writes atomically (tmp + rename). Caller holds the lock.
func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Update runs fn under the write lock and persists afterwards.
func (s *Store) Update(fn func(db *dbFile) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.db); err != nil {
		return err
	}
	return s.saveLocked()
}

// View gives fn a read view. Copy results before returning them.
func (s *Store) View(fn func(db *dbFile)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(&s.db)
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.db.Settings
	st.Categories = append([]string(nil), st.Categories...)
	return st
}

func cloneTicket(t *Ticket) *Ticket {
	c := *t
	c.Labels = append([]string(nil), t.Labels...)
	c.Attachments = append([]Attachment(nil), t.Attachments...)
	c.Messages = append([]Message(nil), t.Messages...)
	c.Notes = append([]Note(nil), t.Notes...)
	return &c
}

func (s *Store) Ticket(id int) *Ticket {
	var out *Ticket
	s.View(func(db *dbFile) {
		for _, t := range db.Tickets {
			if t.ID == id {
				out = cloneTicket(t)
				return
			}
		}
	})
	return out
}

func (s *Store) TicketByToken(token string) *Ticket {
	if token == "" {
		return nil
	}
	h := hashToken(token)
	var out *Ticket
	s.View(func(db *dbFile) {
		for _, t := range db.Tickets {
			if t.TokenHash != "" && t.TokenHash == h {
				out = cloneTicket(t)
				return
			}
		}
	})
	return out
}

// Tickets returns copies, sorted by position.
func (s *Store) Tickets() []*Ticket {
	var out []*Ticket
	s.View(func(db *dbFile) {
		for _, t := range db.Tickets {
			out = append(out, cloneTicket(t))
		}
	})
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pos != out[j].Pos {
			return out[i].Pos < out[j].Pos
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func findTicket(db *dbFile, id int) *Ticket {
	for _, t := range db.Tickets {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// minPos returns a position before all cards of a column (new cards go on top).
func minPos(db *dbFile, status string) float64 {
	min := 0.0
	first := true
	for _, t := range db.Tickets {
		if t.Status == status && (first || t.Pos < min) {
			min = t.Pos
			first = false
		}
	}
	if first {
		return 1000
	}
	return min - 1
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}
