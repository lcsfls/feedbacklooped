package main

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

// User is an admin account. All admins have the same rights.
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	PassHash     string    `json:"pass_hash"`
	Email        string    `json:"email,omitempty"`         // optional, for notifications only
	NotifyEmail  bool      `json:"notify_email,omitempty"`  // email me about new feedback and replies
	SessionEpoch int       `json:"session_epoch,omitempty"` // bump to end all sessions
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitempty"`
}

// Name is what other people see, e.g. next to a reply.
func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

var emailRe = regexp.MustCompile(`^[^\s@<>,;]+@[^\s@<>,;]+\.[^\s@<>,;]+$`)

// notifyEmails returns the addresses of admins who opted in to email notifications.
func (s *Store) notifyEmails() []string {
	var out []string
	s.View(func(db *dbFile) {
		for _, u := range db.Users {
			if u.NotifyEmail && u.Email != "" {
				out = append(out, u.Email)
			}
		}
	})
	return out
}

func normalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// migrateUsers turns the single admin password of older versions into a user.
// Caller holds the lock.
func migrateUsers(db *dbFile) {
	if db.NextUserID < 1 {
		db.NextUserID = 1
		for _, u := range db.Users {
			if u.ID >= db.NextUserID {
				db.NextUserID = u.ID + 1
			}
		}
	}
	if len(db.Users) == 0 && db.Settings.AdminHash != "" {
		db.Users = append(db.Users, &User{ID: db.NextUserID, Username: "admin", DisplayName: "Admin",
			PassHash: db.Settings.AdminHash, CreatedAt: time.Now()})
		db.NextUserID++
	}
	db.Settings.AdminHash = ""
}

func (s *Store) HasUsers() bool {
	var ok bool
	s.View(func(db *dbFile) { ok = len(db.Users) > 0 })
	return ok
}

func (s *Store) User(id int) *User {
	var out *User
	s.View(func(db *dbFile) {
		for _, u := range db.Users {
			if u.ID == id {
				c := *u
				out = &c
			}
		}
	})
	return out
}

func (s *Store) UserByName(name string) *User {
	name = normalizeUsername(name)
	var out *User
	s.View(func(db *dbFile) {
		for _, u := range db.Users {
			if u.Username == name {
				c := *u
				out = &c
			}
		}
	})
	return out
}

func (s *Store) Users() []User {
	var out []User
	s.View(func(db *dbFile) {
		for _, u := range db.Users {
			out = append(out, *u)
		}
	})
	return out
}

// addUser validates and creates a user. Caller holds the lock.
func addUser(db *dbFile, username, display, hash string) (*User, error) {
	username = normalizeUsername(username)
	if !usernameRe.MatchString(username) {
		return nil, errors.New("Username: 2–32 characters, lowercase letters, digits, dot, dash or underscore.")
	}
	for _, u := range db.Users {
		if u.Username == username {
			return nil, errors.New("This username is already taken.")
		}
	}
	u := &User{ID: db.NextUserID, Username: username, DisplayName: cleanText(display, 60), PassHash: hash, CreatedAt: time.Now()}
	db.NextUserID++
	db.Users = append(db.Users, u)
	return u, nil
}

func findUser(db *dbFile, id int) *User {
	for _, u := range db.Users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

func validPassword(pw, pw2 string) error {
	if utf8Len(pw) < 10 {
		return errors.New("The password needs at least 10 characters.")
	}
	if pw != pw2 {
		return errors.New("The passwords do not match.")
	}
	return nil
}

// --- handlers ---------------------------------------------------------------

func (a *App) handleUsers(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 200, "users", pageData{"Users": a.store.Users()})
}

func (a *App) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		a.render(w, r, 400, "users", pageData{"Users": a.store.Users(), "Error": msg,
			"NewUsername": r.FormValue("username"), "NewDisplay": r.FormValue("display_name")})
	}
	if err := validPassword(r.FormValue("password"), r.FormValue("password2")); err != nil {
		fail(err.Error())
		return
	}
	h, err := hashPassword(r.FormValue("password"))
	if err != nil {
		fail(err.Error())
		return
	}
	err = a.store.Update(func(db *dbFile) error {
		_, err := addUser(db, r.FormValue("username"), r.FormValue("display_name"), h)
		return err
	})
	if err != nil {
		fail(err.Error())
		return
	}
	http.Redirect(w, r, "/admin/users?msg=saved", http.StatusSeeOther)
}

func (a *App) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	me := a.currentUser(r)
	id := pathInt(r, "id")
	err := a.store.Update(func(db *dbFile) error {
		if me != nil && id == me.ID {
			return errors.New("You can't delete yourself.")
		}
		if len(db.Users) <= 1 {
			return errors.New("The last admin can't be deleted.")
		}
		db.Users = slices.DeleteFunc(db.Users, func(u *User) bool { return u.ID == id })
		return nil
	})
	if err != nil {
		a.render(w, r, 400, "users", pageData{"Users": a.store.Users(), "Error": err.Error()})
		return
	}
	http.Redirect(w, r, "/admin/users?msg=deleted", http.StatusSeeOther)
}

func (a *App) handleAccount(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 200, "account", nil)
}

func (a *App) handleAccountSave(w http.ResponseWriter, r *http.Request) {
	me := a.currentUser(r)
	display := cleanText(r.FormValue("display_name"), 60)
	email := strings.TrimSpace(r.FormValue("email"))
	if email != "" && !emailRe.MatchString(email) {
		a.render(w, r, 400, "account", pageData{"Error": "Please enter a valid email address."})
		return
	}
	notify := r.FormValue("notify_email") == "1" && email != ""
	a.store.Update(func(db *dbFile) error {
		if u := findUser(db, me.ID); u != nil {
			u.DisplayName, u.Email, u.NotifyEmail = display, email, notify
		}
		return nil
	})
	http.Redirect(w, r, "/admin/account?msg=saved", http.StatusSeeOther)
}

func (a *App) handlePassword(w http.ResponseWriter, r *http.Request) {
	me := a.currentUser(r)
	if !checkPassword(me.PassHash, r.FormValue("current")) {
		a.render(w, r, 400, "account", pageData{"PwError": "The current password is wrong."})
		return
	}
	if err := validPassword(r.FormValue("password"), r.FormValue("password2")); err != nil {
		a.render(w, r, 400, "account", pageData{"PwError": err.Error()})
		return
	}
	h, err := hashPassword(r.FormValue("password"))
	if err != nil {
		a.fail(w, r, 500, err.Error())
		return
	}
	var updated User
	a.store.Update(func(db *dbFile) error {
		if u := findUser(db, me.ID); u != nil {
			u.PassHash = h
			updated = *u
		}
		return nil
	})
	a.setSession(w, r, &updated) // new fingerprint: this session stays, all others end
	http.Redirect(w, r, "/admin/account?msg=pw", http.StatusSeeOther)
}

// dummyHash keeps login timing equal for unknown usernames.
var dummyHash, _ = hashPassword("timing-equaliser")

// handleLogoutAll ends every session of the current user, then logs in again here.
func (a *App) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	me := a.currentUser(r)
	var updated User
	a.store.Update(func(db *dbFile) error {
		if u := findUser(db, me.ID); u != nil {
			u.SessionEpoch++
			updated = *u
		}
		return nil
	})
	a.setSession(w, r, &updated)
	http.Redirect(w, r, "/admin/account?msg=loggedout", http.StatusSeeOther)
}
