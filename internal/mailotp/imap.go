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

// OTP matchers, tried in this order so a year ("2026"), account number, or ZIP
// elsewhere in the message can't be mistaken for the code:
//  1. a 4-8 digit run right after OTP wording ("code: 123456");
//  2. a 4-8 digit run right before "is your" / "as your" ("123456 is your ...");
//  3. fallback: the first standalone 4-8 digit run anywhere.
var otpMatchers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:code|passcode|one[- ]?time password|one[- ]?time code|verification|security code|otp|pin)\b\D{0,24}(\d{4,8})\b`),
	regexp.MustCompile(`(?i)\b(\d{4,8})\D{0,20}(?:is your|as your)\b`),
	regexp.MustCompile(`\b(\d{4,8})\b`),
}

// findOTPCode pulls the OTP out of one or more text blocks (subject first, then
// body parts). Each matcher is run against every block before the next matcher
// is tried, so a keyword-anchored code in the body still beats a stray number
// in the subject.
func findOTPCode(texts ...string) string {
	for _, re := range otpMatchers {
		for _, t := range texts {
			if m := re.FindStringSubmatch(t); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

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
		fetchErr <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}, messages)
	}()

	cutoff := since.Add(-2 * time.Minute) // clock-skew slack
	var latest *imap.Message
	var latestAt time.Time
	for msg := range messages {
		if msg.Envelope == nil {
			continue
		}
		recv := receivedAt(msg)
		if recv.Before(cutoff) {
			continue
		}
		if !matches(msg, hint) {
			continue
		}
		if latest == nil || recv.After(latestAt) {
			latest, latestAt = msg, recv
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

// receivedAt is the server's receipt time (INTERNALDATE) when present, falling
// back to the sender's Date header. INTERNALDATE is set by Gmail, not by a
// possibly-skewed sending client, so it's the reliable basis for "did this
// arrive after we asked for the code".
func receivedAt(msg *imap.Message) time.Time {
	if !msg.InternalDate.IsZero() {
		return msg.InternalDate
	}
	return msg.Envelope.Date
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

// extractCode gathers the subject and every MIME text part of the body, then
// hands them to findOTPCode (keyword-anchored first, "any digit run" as a
// fallback). Collecting all the text before matching means a code in the body
// beats a stray number in the subject.
func extractCode(msg *imap.Message, section *imap.BodySectionName) string {
	texts := []string{msg.Envelope.Subject}

	if r := msg.GetBody(section); r != nil {
		if mr, err := mail.CreateReader(r); err == nil {
			for {
				part, err := mr.NextPart()
				if err == io.EOF || err != nil {
					break
				}
				if _, ok := part.Header.(*mail.InlineHeader); !ok {
					continue // attachment, not a body part
				}
				if body, err := io.ReadAll(part.Body); err == nil {
					texts = append(texts, string(body))
				}
			}
		}
	}

	return findOTPCode(texts...)
}
