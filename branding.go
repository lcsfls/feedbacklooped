package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Brand holds the white-label settings. Empty colors mean "use the default
// from style.css"; without a logo the built-in mark is shown.
type Brand struct {
	Name          string `json:"name"`            // wordmark, bold part
	Tag           string `json:"tag"`             // wordmark, mono part (optional)
	HideText      bool   `json:"hide_text"`       // the logo already contains the name
	PrimaryLight  string `json:"primary_light"`   // buttons, links, focus ring (light mode)
	PrimaryDark   string `json:"primary_dark"`    // same for dark mode; empty = derived from light
	MarkHold      string `json:"mark_hold"`       // built-in mark: outline (light mode)
	MarkBeam      string `json:"mark_beam"`       // built-in mark: accent bar
	LogoFile      string `json:"logo_file"`       // file name in <data>/branding
	LogoMime      string `json:"logo_mime"`       //
	LogoVersion   int64  `json:"logo_version"`    // cache buster
	HidePoweredBy bool   `json:"hide_powered_by"` // footer credit
	OgFile        string `json:"og_file"`         // social preview image in <data>/branding
	OgMime        string `json:"og_mime"`
	OgVersion     int64  `json:"og_version"`
}

func defaultBrand() Brand {
	return Brand{Name: "feedback"}
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// foregroundFor picks white or Nile text for a background color by luminance.
func foregroundFor(hex string) string {
	r, _ := strconv.ParseUint(hex[1:3], 16, 8)
	g, _ := strconv.ParseUint(hex[3:5], 16, 8)
	b, _ := strconv.ParseUint(hex[5:7], 16, 8)
	lum := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
	if lum > 0.6 {
		return "oklch(0.205 0 0)"
	}
	return "#ffffff"
}

// brandCSS overrides the design tokens for the configured colors only.
func brandCSS(b Brand) template.CSS {
	var light, dark strings.Builder
	primary := func(w *strings.Builder, c, fg string) {
		fmt.Fprintf(w, "--primary:%s;--primary-foreground:%s;--ring:%s;--info:%s;--info-text:%s;", c, fg, c, c, c)
	}
	if hexColor.MatchString(b.PrimaryLight) {
		primary(&light, b.PrimaryLight, foregroundFor(b.PrimaryLight))
		if !hexColor.MatchString(b.PrimaryDark) {
			// Lighten for dark backgrounds.
			primary(&dark, "color-mix(in srgb, "+b.PrimaryLight+" 55%, white)", "oklch(0.205 0 0)")
		}
	}
	if hexColor.MatchString(b.PrimaryDark) {
		primary(&dark, b.PrimaryDark, foregroundFor(b.PrimaryDark))
	}
	if hexColor.MatchString(b.MarkHold) {
		fmt.Fprintf(&light, "--mark-hold:%s;", b.MarkHold) // dark mode keeps the light hold
	}
	if hexColor.MatchString(b.MarkBeam) {
		fmt.Fprintf(&light, "--mark-beam:%s;", b.MarkBeam)
		fmt.Fprintf(&dark, "--mark-beam:%s;", b.MarkBeam)
	}
	var out strings.Builder
	if light.Len() > 0 {
		out.WriteString(":root{" + light.String() + "}")
	}
	if dark.Len() > 0 {
		out.WriteString("@media (prefers-color-scheme: dark){:root:not([data-theme=light]){" + dark.String() + "}}")
		out.WriteString(":root[data-theme=dark]{" + dark.String() + "}")
	}
	return template.CSS(out.String())
}

func (a *App) brandDir() string { return filepath.Join(filepath.Dir(a.store.Uploads), "branding") }

// sniffLogo accepts PNG, JPEG, WebP and SVG.
func sniffLogo(b []byte) (mime, ext string, err error) {
	if m, e, err := sniff(b, ""); err == nil && strings.HasPrefix(m, "image/") && m != "image/gif" {
		return m, e, nil
	}
	head := bytes.ToLower(b[:min(len(b), 2048)])
	if bytes.Contains(head, []byte("<svg")) {
		return "image/svg+xml", ".svg", nil
	}
	return "", "", errors.New("logo must be PNG, JPEG, WebP or SVG")
}

func (a *App) handleLogoUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	f, _, err := r.FormFile("logo")
	if err != nil {
		a.fail(w, r, 400, "Please choose a logo file (max. 2 MB).")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		a.fail(w, r, 400, "The logo is larger than 2 MB.")
		return
	}
	mime, ext, err := sniffLogo(data)
	if err != nil {
		a.fail(w, r, 400, err.Error())
		return
	}
	if mime != "image/svg+xml" {
		data = stripMetadata(mime, data)
	}
	dir := a.brandDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		a.fail(w, r, 500, err.Error())
		return
	}
	name := "logo" + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		a.fail(w, r, 500, err.Error())
		return
	}
	var old string
	a.store.Update(func(db *dbFile) error {
		old = db.Settings.Brand.LogoFile
		db.Settings.Brand.LogoFile, db.Settings.Brand.LogoMime = name, mime
		db.Settings.Brand.LogoVersion = time.Now().Unix()
		return nil
	})
	if old != "" && old != name {
		os.Remove(filepath.Join(dir, old))
	}
	http.Redirect(w, r, "/admin/settings?msg=saved#branding", http.StatusSeeOther)
}

func (a *App) handleLogoDelete(w http.ResponseWriter, r *http.Request) {
	var old string
	a.store.Update(func(db *dbFile) error {
		old = db.Settings.Brand.LogoFile
		db.Settings.Brand.LogoFile, db.Settings.Brand.LogoMime = "", ""
		return nil
	})
	if old != "" {
		os.Remove(filepath.Join(a.brandDir(), old))
	}
	http.Redirect(w, r, "/admin/settings?msg=deleted#branding", http.StatusSeeOther)
}

// handleBrandReset restores the default branding (keeps the logo file on disk
// only until it is replaced; the reference is removed).
func (a *App) handleBrandReset(w http.ResponseWriter, r *http.Request) {
	var old, og string
	a.store.Update(func(db *dbFile) error {
		old, og = db.Settings.Brand.LogoFile, db.Settings.Brand.OgFile
		db.Settings.Brand = defaultBrand()
		return nil
	})
	for _, f := range []string{old, og} {
		if f != "" {
			os.Remove(filepath.Join(a.brandDir(), f))
		}
	}
	http.Redirect(w, r, "/admin/settings?msg=saved#branding", http.StatusSeeOther)
}

func (a *App) saveBrandForm(r *http.Request, b *Brand) {
	b.Name = cleanText(r.FormValue("brand_name"), 40)
	b.Tag = cleanText(r.FormValue("brand_tag"), 40)
	b.HideText = r.FormValue("brand_hide_text") == "1"
	b.HidePoweredBy = r.FormValue("hide_powered_by") == "1"
	color := func(key string) string {
		if r.FormValue(key+"_on") != "1" {
			return ""
		}
		if c := strings.TrimSpace(r.FormValue(key)); hexColor.MatchString(c) {
			return strings.ToLower(c)
		}
		return ""
	}
	b.PrimaryLight = color("primary_light")
	b.PrimaryDark = color("primary_dark")
	b.MarkHold = color("mark_hold")
	b.MarkBeam = color("mark_beam")
}

// handleLogo serves the uploaded logo (also used as favicon).
func (a *App) handleLogo(w http.ResponseWriter, r *http.Request) {
	b := a.store.Settings().Brand
	if b.LogoFile == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.brandDir(), b.LogoFile))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", b.LogoMime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// An SVG opened directly must not run scripts in our origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	http.ServeContent(w, r, "", time.Unix(b.LogoVersion, 0), f)
}

func defaultColor(c, def string) string {
	if hexColor.MatchString(c) {
		return c
	}
	return def
}

// handleOgUpload stores the social preview image (Open Graph / Twitter card).
func (a *App) handleOgUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	f, _, err := r.FormFile("og")
	if err != nil {
		a.fail(w, r, 400, "Please choose an image (max. 5 MB).")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 5<<20+1))
	if err != nil || len(data) > 5<<20 {
		a.fail(w, r, 400, "The image is larger than 5 MB.")
		return
	}
	mime, ext, err := sniff(data[:min(len(data), 4096)], "")
	if err != nil || (mime != "image/png" && mime != "image/jpeg") {
		a.fail(w, r, 400, "Link previews need a PNG or JPEG image (1200 × 630 px recommended).")
		return
	}
	data = stripMetadata(mime, data)
	dir := a.brandDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		a.fail(w, r, 500, err.Error())
		return
	}
	name := "og" + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		a.fail(w, r, 500, err.Error())
		return
	}
	var old string
	a.store.Update(func(db *dbFile) error {
		old = db.Settings.Brand.OgFile
		db.Settings.Brand.OgFile, db.Settings.Brand.OgMime = name, mime
		db.Settings.Brand.OgVersion = time.Now().Unix()
		return nil
	})
	if old != "" && old != name {
		os.Remove(filepath.Join(dir, old))
	}
	http.Redirect(w, r, "/admin/settings?msg=saved#seo", http.StatusSeeOther)
}

func (a *App) handleOgDelete(w http.ResponseWriter, r *http.Request) {
	var old string
	a.store.Update(func(db *dbFile) error {
		old = db.Settings.Brand.OgFile
		db.Settings.Brand.OgFile, db.Settings.Brand.OgMime = "", ""
		return nil
	})
	if old != "" {
		os.Remove(filepath.Join(a.brandDir(), old))
	}
	http.Redirect(w, r, "/admin/settings?msg=deleted#seo", http.StatusSeeOther)
}

func (a *App) handleOg(w http.ResponseWriter, r *http.Request) {
	b := a.store.Settings().Brand
	if b.OgFile == "" {
		http.Redirect(w, r, "/static/og-default.png", http.StatusFound)
		return
	}
	f, err := os.Open(filepath.Join(a.brandDir(), b.OgFile))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", b.OgMime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "", time.Unix(b.OgVersion, 0), f)
}
