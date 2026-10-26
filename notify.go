package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Event struct {
	Kind     string    `json:"event"` // "new_ticket" | "reply"
	TicketID int       `json:"id"`
	Title    string    `json:"title"`
	Category string    `json:"category"`
	Body     string    `json:"body"`
	Files    int       `json:"attachments"`
	URL      string    `json:"url"`
	At       time.Time `json:"created_at"`
}

func (e Event) headline() string {
	if e.Kind == "reply" {
		return fmt.Sprintf("Reply on #%d: %s", e.TicketID, e.Title)
	}
	return fmt.Sprintf("New feedback #%d (%s): %s", e.TicketID, e.Category, e.Title)
}

func (e Event) text() string {
	body := e.Body
	if len([]rune(body)) > 400 {
		body = string([]rune(body)[:400]) + " …"
	}
	s := body
	if e.Files > 0 {
		s += fmt.Sprintf("\n\n%d attachment(s)", e.Files)
	}
	if e.URL != "" {
		s += "\n\n" + e.URL
	}
	return s
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// withAdminRecipients adds the admins who opted in to the email recipients.
func (a *App) withAdminRecipients(st Settings) Settings {
	seen := map[string]bool{}
	var to []string
	for _, t := range append(strings.Split(st.SMTPTo, ","), a.store.notifyEmails()...) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			to = append(to, t)
		}
	}
	st.SMTPTo = strings.Join(to, ", ")
	return st
}

func (a *App) notify(e Event) {
	st := a.withAdminRecipients(a.store.Settings())
	if st.BaseURL != "" {
		e.URL = strings.TrimRight(st.BaseURL, "/") + fmt.Sprintf("/admin/t/%d", e.TicketID)
	}
	go func() {
		for _, err := range a.sendAll(st, e) {
			log.Printf("notification failed: %v", err)
		}
	}()
}

func (a *App) sendAll(st Settings, e Event) []error {
	var errs []error
	if st.NtfyURL != "" {
		if err := sendNtfy(st, e); err != nil {
			errs = append(errs, fmt.Errorf("ntfy: %w", err))
		}
	}
	if st.WebhookURL != "" {
		if err := sendWebhook(st, e); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}
	if st.SMTPHost != "" && st.SMTPTo != "" {
		if err := sendMail(st, e); err != nil {
			errs = append(errs, fmt.Errorf("email: %w", err))
		}
	}
	return errs
}

func sendNtfy(st Settings, e Event) error {
	req, err := http.NewRequest("POST", st.NtfyURL, strings.NewReader(e.text()))
	if err != nil {
		return err
	}
	// ntfy expects headers in ASCII or RFC 2047
	req.Header.Set("Title", mime.QEncoding.Encode("utf-8", e.headline()))
	req.Header.Set("Tags", "speech_balloon")
	if e.URL != "" {
		req.Header.Set("Click", e.URL)
	}
	if st.NtfyToken != "" {
		req.Header.Set("Authorization", "Bearer "+st.NtfyToken)
	}
	return doRequest(req)
}

func sendWebhook(st Settings, e Event) error {
	payload := map[string]any{
		"event": e.Kind, "id": e.TicketID, "title": e.Title, "category": e.Category,
		"body": e.Body, "attachments": e.Files, "url": e.URL, "created_at": e.At,
		// "text" makes the webhook work out of the box with Slack, Mattermost, Rocket.Chat & co.
		"text": e.headline() + "\n" + e.text(),
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", st.WebhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func doRequest(req *http.Request) error {
	req.Header.Set("User-Agent", "feedbacklooped")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func sendMail(st Settings, e Event) error {
	from := st.SMTPFrom
	if from == "" {
		from = st.SMTPUser
	}
	if from == "" {
		return errors.New("no sender configured")
	}
	var to []string
	for _, t := range strings.Split(st.SMTPTo, ",") {
		if t = strings.TrimSpace(t); t != "" {
			to = append(to, t)
		}
	}
	msg := "From: " + from + "\r\n" +
		"To: " + strings.Join(to, ", ") + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", e.headline()) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(e.text(), "\n", "\r\n") + "\r\n"

	port := st.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(st.SMTPHost, strconv.Itoa(port))
	var c *smtp.Client
	var err error
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if port == 465 {
		conn, derr := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: st.SMTPHost})
		if derr != nil {
			return derr
		}
		c, err = smtp.NewClient(conn, st.SMTPHost)
	} else {
		conn, derr := dialer.Dial("tcp", addr)
		if derr != nil {
			return derr
		}
		c, err = smtp.NewClient(conn, st.SMTPHost)
		if err == nil {
			if ok, _ := c.Extension("STARTTLS"); ok {
				err = c.StartTLS(&tls.Config{ServerName: st.SMTPHost})
			} else if st.SMTPUser != "" {
				err = errors.New("server does not offer STARTTLS; refusing to send the password unencrypted")
			}
		}
	}
	if err != nil {
		return err
	}
	defer c.Close()
	if st.SMTPUser != "" {
		if err := c.Auth(smtp.PlainAuth("", st.SMTPUser, st.SMTPPass, st.SMTPHost)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, t := range to {
		if err := c.Rcpt(t); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
