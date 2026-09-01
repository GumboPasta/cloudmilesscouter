// Package mailotp reads a numeric one-time passcode out of a Gmail inbox over
// IMAP, so an unattended login flow (United's bootstrap, here) can clear an
// email-delivered 2FA step without a human watching the browser or their
// inbox. It's a thin, single-purpose IMAP client: poll, match, extract —
// nothing more.
package mailotp

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
)

// codePattern matches a standalone 4-8 digit code — covers United and most
// providers' OTP length without hard-coding to exactly 6.
var codePattern = regexp.MustCompile(`\b(\d{4,8})\b`)

// Config holds the IMAP credentials for the inbox to poll. AppPassword must
// be a Gmail App Password (myaccount.google.com/apppasswords), not the
// account password — Gmail rejects plain IMAP logins otherwise.
type Config struct {
	Address     string
	AppPassword string
}

// WaitForCode polls the inbox every 3s until timeout for a message received
// after since whose sender address, sender name, or subject contains hint
// (case-insensitive substring — e.g. "united"), and returns the first 4-8
// digit code found in its subject or text body.
func WaitForCode(cfg Config, hint string, since time.Time, timeout time.Duration) (string, error) {
	if cfg.Address == "" || cfg.AppPassword == "" {
		return "", errors.New("mailotp: GMAIL_ADDRESS / GMAIL_APP_PASSWORD not set")
	}

	deadline := time.Now().Add(timeout)
	for {
		code, err := pollOnce(cfg, hint, since)
		if err != nil {
			return "", err
		}
		if code != "" {
			return code, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("mailotp: no matching message (hint %q) arrived within %s", hint, timeout)
		}
		time.Sleep(3 * time.Second)
	}
}

// pollOnce opens a fresh IMAP connection, searches, and closes — simplest
// thing that works for a handful of polls over ~a minute; not worth holding a
// connection open across the poll loop.
func pollOnce(cfg Config, hint string, since time.Time) (string, error) {
	c, err := client.DialTLS("imap.gmail.com:993", nil)
	if err != nil {
		return "", fmt.Errorf("mailotp: dial: %w", err)
	}
	defer c.Logout()

	if err := c.Login(cfg.Address, cfg.AppPassword); err != nil {
		return "", fmt.Errorf("mailotp: login (use a Gmail App Password, not your account password): %w", err)
	}
	if _, err := c.Select("INBOX", false); err != nil {
		return "", fmt.Errorf("mailotp: select inbox: %w", err)
	}

	// IMAP SINCE has day granularity, so this is a coarse pre-filter; the
	// envelope-date check below narrows to the actual window.
	criteria := imap.NewSearchCriteria()
	criteria.Since = since.Add(-24 * time.Hour)

	ids, err := c.Search(criteria)
	if err != nil {
		return "", fmt.Errorf("mailotp: search: %w", err)
	}
	if len(ids) == 0 {
		return "", nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)

	section := &imap.BodySectionName{}
	messages := make(chan *imap.Message, len(ids))
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}, messages)
	}()

	cutoff := since.Add(-1 * time.Minute) // small clock-skew slack
	var latest *imap.Message
	for msg := range messages {
		if msg.Envelope == nil || msg.Envelope.Date.Before(cutoff) {
			continue
		}
		if !matches(msg, hint) {
			continue
		}
		if latest == nil || msg.Envelope.Date.After(latest.Envelope.Date) {
			latest = msg
		}
	}
	if err := <-fetchErr; err != nil {
		return "", fmt.Errorf("mailotp: fetch: %w", err)
	}
	if latest == nil {
		return "", nil
	}
	return extractCode(latest, section), nil
}

// matches reports whether hint appears in the sender's address/name or the
// subject, case-insensitively. An empty hint matches everything.
func matches(msg *imap.Message, hint string) bool {
	if hint == "" {
		return true
	}
	hint = strings.ToLower(hint)
	if strings.Contains(strings.ToLower(msg.Envelope.Subject), hint) {
		return true
	}
	for _, addr := range msg.Envelope.From {
		if strings.Contains(strings.ToLower(addr.HostName), hint) ||
			strings.Contains(strings.ToLower(addr.PersonalName), hint) {
			return true
		}
	}
	return false
}

// extractCode looks in the subject first (some providers put the code right
// there), then walks the MIME text parts of the body.
func extractCode(msg *imap.Message, section *imap.BodySectionName) string {
	if m := codePattern.FindStringSubmatch(msg.Envelope.Subject); m != nil {
		return m[1]
	}

	r := msg.GetBody(section)
	if r == nil {
		return ""
	}
	mr, err := mail.CreateReader(r)
	if err != nil {
		return "" // not parseable as MIME — skip rather than fail the whole poll
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF || err != nil {
			break
		}
		if _, ok := part.Header.(*mail.InlineHeader); !ok {
			continue // attachment, not a body part
		}
		body, err := io.ReadAll(part.Body)
		if err != nil {
			continue
		}
		if m := codePattern.FindStringSubmatch(string(body)); m != nil {
			return m[1]
		}
	}
	return ""
}
