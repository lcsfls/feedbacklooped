package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web/templates/*.html
var templateFS embed.FS

//go:embed web/static
var staticFS embed.FS

var version = "dev"

type Config struct {
	Listen        string
	DataDir       string
	TrustProxy    bool
	AdminPassword string
	BaseURL       string
}

type App struct {
	cfg         Config
	store       *Store
	tpl         map[string]*template.Template
	submitLimit *limiter
	loginLimit  *limiter
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := Config{
		Listen:        env("FL_LISTEN", ":8080"),
		DataDir:       env("FL_DATA", "./data"),
		TrustProxy:    env("FL_TRUST_PROXY", "false") == "true",
		AdminPassword: os.Getenv("FL_ADMIN_PASSWORD"),
		BaseURL:       strings.TrimRight(env("FL_BASE_URL", ""), "/"),
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		_, port, _ := net.SplitHostPort(cfg.Listen)
		resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("data directory %s: %v", cfg.DataDir, err)
	}
	app := &App{
		cfg:         cfg,
		store:       store,
		submitLimit: newLimiter(10, time.Hour),
		loginLimit:  newLimiter(8, 15*time.Minute),
	}
	if err := app.applyEnv(); err != nil {
		log.Fatal(err)
	}
	if err := app.loadTemplates(); err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		log.Printf("feedbacklooped %s listening on %s (data: %s)", version, cfg.Listen, cfg.DataDir)
		if store.Settings().AdminHash == "" {
			log.Printf("No admin password set yet: open /admin to set it up.")
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// applyEnv applies FL_ADMIN_PASSWORD and FL_BASE_URL. The password from the
// environment is only re-hashed when it differs from the stored one, so the
// variable doubles as a password reset.
func (a *App) applyEnv() error {
	return a.store.Update(func(db *dbFile) error {
		if a.cfg.AdminPassword != "" && !checkPassword(db.Settings.AdminHash, a.cfg.AdminPassword) {
			h, err := hashPassword(a.cfg.AdminPassword)
			if err != nil {
				return err
			}
			db.Settings.AdminHash = h
			log.Printf("Admin password taken from FL_ADMIN_PASSWORD.")
		}
		if a.cfg.BaseURL != "" && db.Settings.BaseURL == "" {
			db.Settings.BaseURL = a.cfg.BaseURL
		}
		return nil
	})
}

func (a *App) loadTemplates() error {
	funcs := template.FuncMap{
		"statusLabel":       statusLabel,
		"publicStatusLabel": publicStatusLabel,
		"statuses":          func() any { return Statuses },
		"date": func(t time.Time) string {
			return t.Local().Format("Jan 2, 2006")
		},
		"datetime": func(t time.Time) string {
			return t.Local().Format("Jan 2, 2006, 15:04")
		},
		"ago": ago,
		"size": func(n int64) string {
			switch {
			case n >= 1<<20:
				return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
			case n >= 1<<10:
				return fmt.Sprintf("%d KB", n>>10)
			}
			return fmt.Sprintf("%d B", n)
		},
		"join":         strings.Join,
		"brandCSS":     brandCSS,
		"defaultColor": defaultColor,
		"eq":           func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		"json": func(v any) (template.JS, error) {
			b, err := json.Marshal(v)
			return template.JS(b), err
		},
		"add": func(a, b int) int { return a + b },
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"stepIndex": func(s string) int {
			switch s {
			case "inbox":
				return 0
			case "review":
				return 1
			case "planned":
				return 2
			case "done":
				return 3
			}
			return -1
		},
	}
	pages, err := fs.Glob(templateFS, "web/templates/*.html")
	if err != nil {
		return err
	}
	a.tpl = map[string]*template.Template{}
	for _, p := range pages {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "web/templates/"), ".html")
		if name == "layout" {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "web/templates/layout.html", p)
		if err != nil {
			return fmt.Errorf("template %s: %w", name, err)
		}
		a.tpl[name] = t
	}
	return nil
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
	return t.Local().Format("Jan 2, 2006")
}

type pageData map[string]any

func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name string, data pageData) {
	t, ok := a.tpl[name]
	if !ok {
		http.Error(w, "missing template: "+name, 500)
		return
	}
	if data == nil {
		data = pageData{}
	}
	data["S"] = a.store.Settings()
	data["Admin"] = a.isAdmin(r)
	data["Path"] = r.URL.Path
	data["Version"] = version
	if msg := r.URL.Query().Get("msg"); msg != "" {
		data["Flash"] = flashText(msg)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.Execute(w, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func flashText(code string) string {
	switch code {
	case "saved":
		return "Saved."
	case "pw":
		return "Password changed."
	case "deleted":
		return "Deleted."
	case "sent":
		return "Message is now visible on the status page."
	case "reply":
		return "Thanks, your message has been received."
	}
	return ""
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 404, "error", pageData{"Title": "Not found", "Message": "This page does not exist (anymore)."})
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if wantsJSON(r) {
		writeJSON(w, status, map[string]any{"ok": false, "error": msg})
		return
	}
	a.render(w, r, status, "error", pageData{"Title": "Something went wrong", "Message": msg})
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.Header.Get("Content-Type") == "application/json"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), display-capture=(self)")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; "+
			"style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "web/static")
	fileServer := http.FileServer(http.FS(static))
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheStatic(fileServer)))
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if a.store.Settings().Brand.LogoFile != "" {
			a.handleLogo(w, r)
			return
		}
		r.URL.Path = "/favicon.ico"
		cacheStatic(fileServer).ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /brand/logo", a.handleLogo)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("User-agent: *\nDisallow: /s/\nDisallow: /admin\nDisallow: /media/\n"))
	})

	// public
	mux.HandleFunc("GET /{$}", a.handleForm)
	mux.HandleFunc("POST /submit", a.handleSubmit)
	mux.HandleFunc("GET /s/{token}", a.handleStatus)
	mux.HandleFunc("POST /s/{token}/reply", a.handleSubmitterReply)
	mux.HandleFunc("GET /roadmap", a.handleRoadmap)
	mux.HandleFunc("GET /media/{tid}/{aid}", a.handleMedia)

	// Admin
	mux.HandleFunc("GET /admin", a.handleAdminHome)
	mux.HandleFunc("POST /admin/setup", a.handleSetup)
	mux.HandleFunc("POST /admin/login", a.handleLogin)
	mux.HandleFunc("POST /admin/logout", a.handleLogout)
	mux.HandleFunc("POST /admin/new", a.admin(a.handleNewCard))
	mux.HandleFunc("POST /admin/api/move", a.admin(a.handleMove))
	mux.HandleFunc("GET /admin/t/{id}", a.admin(a.handleTicket))
	mux.HandleFunc("POST /admin/t/{id}", a.admin(a.handleTicketUpdate))
	mux.HandleFunc("POST /admin/t/{id}/message", a.admin(a.handleTeamMessage))
	mux.HandleFunc("POST /admin/t/{id}/message/{mid}/delete", a.admin(a.handleMessageDelete))
	mux.HandleFunc("POST /admin/t/{id}/note", a.admin(a.handleNote))
	mux.HandleFunc("POST /admin/t/{id}/note/{nid}/delete", a.admin(a.handleNoteDelete))
	mux.HandleFunc("POST /admin/t/{id}/attachment/{aid}/delete", a.admin(a.handleAttachmentDelete))
	mux.HandleFunc("POST /admin/t/{id}/delete", a.admin(a.handleTicketDelete))
	mux.HandleFunc("GET /admin/settings", a.admin(a.handleSettings))
	mux.HandleFunc("POST /admin/settings", a.admin(a.handleSettingsSave))
	mux.HandleFunc("POST /admin/settings/test", a.admin(a.handleNotifyTest))
	mux.HandleFunc("POST /admin/password", a.admin(a.handlePassword))
	mux.HandleFunc("GET /admin/export", a.admin(a.handleExport))
	mux.HandleFunc("POST /admin/branding/logo", a.admin(a.handleLogoUpload))
	mux.HandleFunc("POST /admin/branding/logo/delete", a.admin(a.handleLogoDelete))
	mux.HandleFunc("POST /admin/branding/reset", a.admin(a.handleBrandReset))

	mux.HandleFunc("/", a.notFound)
	return securityHeaders(mux)
}

func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// admin guards admin routes: login required, state-changing requests same-origin only.
func (a *App) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/admin", http.StatusSeeOther)
				return
			}
			a.fail(w, r, http.StatusUnauthorized, "Please log in again.")
			return
		}
		if r.Method != http.MethodGet && !sameOrigin(r) {
			a.fail(w, r, http.StatusForbidden, "Request from another site rejected.")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func pathInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.PathValue(name))
	return n
}
