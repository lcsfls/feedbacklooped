package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// PublicBoardID is the board that receives submissions from the public form
// and feeds the roadmap. Every other board is private: admins only.
const PublicBoardID = 1

type Board struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (b Board) Public() bool { return b.ID == PublicBoardID }

// migrateBoards creates the public board and assigns old tickets to it.
// Caller holds the lock.
func migrateBoards(db *dbFile) {
	if findBoard(db, PublicBoardID) == nil {
		db.Boards = append([]*Board{{ID: PublicBoardID, Name: "Feedback", CreatedAt: time.Now()}}, db.Boards...)
	}
	for _, b := range db.Boards {
		if b.ID >= db.NextBoardID {
			db.NextBoardID = b.ID + 1
		}
	}
	for _, t := range db.Tickets {
		if t.BoardID == 0 || findBoard(db, t.BoardID) == nil {
			t.BoardID = PublicBoardID
		}
	}
}

func findBoard(db *dbFile, id int) *Board {
	for _, b := range db.Boards {
		if b.ID == id {
			return b
		}
	}
	return nil
}

func (s *Store) Boards() []Board {
	var out []Board
	s.View(func(db *dbFile) {
		for _, b := range db.Boards {
			out = append(out, *b)
		}
	})
	return out
}

func (s *Store) Board(id int) *Board {
	var out *Board
	s.View(func(db *dbFile) {
		if b := findBoard(db, id); b != nil {
			c := *b
			out = &c
		}
	})
	return out
}

// boardTab is one entry of the board switcher.
type boardTab struct {
	Board
	Count, Unread int
}

func (a *App) boardTabs() []boardTab {
	var tabs []boardTab
	tickets := a.store.Tickets()
	for _, b := range a.store.Boards() {
		tab := boardTab{Board: b}
		for _, t := range tickets {
			if t.BoardID == b.ID {
				tab.Count++
				if t.UnreadReply {
					tab.Unread++
				}
			}
		}
		tabs = append(tabs, tab)
	}
	return tabs
}

func boardParam(r *http.Request) int {
	id, err := strconv.Atoi(r.URL.Query().Get("b"))
	if err != nil || id < 1 {
		return PublicBoardID
	}
	return id
}

func boardURL(id int) string {
	if id == PublicBoardID {
		return "/admin"
	}
	return fmt.Sprintf("/admin?b=%d", id)
}

// --- board management -------------------------------------------------------

func (a *App) handleBoards(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, 200, "boards", pageData{"Tabs": a.boardTabs()})
}

func (a *App) handleBoardCreate(w http.ResponseWriter, r *http.Request) {
	name := oneLine(cleanText(r.FormValue("name"), 60))
	if name == "" {
		a.render(w, r, 400, "boards", pageData{"Tabs": a.boardTabs(), "Error": "Please enter a name."})
		return
	}
	var id int
	a.store.Update(func(db *dbFile) error {
		id = db.NextBoardID
		db.NextBoardID++
		db.Boards = append(db.Boards, &Board{ID: id, Name: name, CreatedAt: time.Now()})
		return nil
	})
	http.Redirect(w, r, boardURL(id), http.StatusSeeOther)
}

func (a *App) handleBoardRename(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	name := oneLine(cleanText(r.FormValue("name"), 60))
	if name != "" {
		a.store.Update(func(db *dbFile) error {
			if b := findBoard(db, id); b != nil {
				b.Name = name
			}
			return nil
		})
	}
	http.Redirect(w, r, "/admin/boards?msg=saved", http.StatusSeeOther)
}

func (a *App) handleBoardDelete(w http.ResponseWriter, r *http.Request) {
	id := pathInt(r, "id")
	err := a.store.Update(func(db *dbFile) error {
		if id == PublicBoardID {
			return errors.New("The public board can't be deleted.")
		}
		for _, t := range db.Tickets {
			if t.BoardID == id {
				return errors.New("This board still has cards. Move or delete them first.")
			}
		}
		db.Boards = slices.DeleteFunc(db.Boards, func(b *Board) bool { return b.ID == id })
		return nil
	})
	if err != nil {
		a.render(w, r, 400, "boards", pageData{"Tabs": a.boardTabs(), "Error": err.Error()})
		return
	}
	http.Redirect(w, r, "/admin/boards?msg=deleted", http.StatusSeeOther)
}
