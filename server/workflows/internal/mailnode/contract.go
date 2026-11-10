package mailnode

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/example/runloom/workflows/internal/model"
)

const MaxBodyBytes = 256 * 1024
const MaxRecipients = 50

func Address(value string) error {
	if len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("use a single plain email address")
	}
	for _, r := range value {
		if r <= 32 || r >= 127 {
			return fmt.Errorf("email addresses must use ASCII without whitespace; use punycode for domains")
		}
	}
	a, err := mail.ParseAddress(value)
	if err != nil || a.Name != "" || a.Address != value || strings.Count(value, "@") != 1 {
		return fmt.Errorf("use a single plain email address")
	}
	return nil
}

// Templates are validated again after resolution, before any SMTP connection.
func Validate(message *model.EmailMessage, templates bool) error {
	if message == nil {
		return fmt.Errorf("email configuration is required")
	}
	if len(message.To) == 0 || len(message.To)+len(message.CC)+len(message.BCC) > MaxRecipients {
		return fmt.Errorf("email requires To and allows at most 50 total recipients")
	}
	check := func(value string) error {
		if value == "" || len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("invalid email recipient or reply-to")
		}
		if templates && strings.Contains(value, "${") {
			return nil
		}
		return Address(value)
	}
	for _, list := range [][]string{message.To, message.CC, message.BCC} {
		for _, value := range list {
			if err := check(value); err != nil {
				return err
			}
		}
	}
	if message.ReplyTo != "" {
		if err := check(message.ReplyTo); err != nil {
			return err
		}
	}
	if strings.TrimSpace(message.Subject) == "" || len(message.Subject) > 300 || strings.ContainsAny(message.Subject, "\r\n\x00") || !utf8.ValidString(message.Subject) {
		return fmt.Errorf("email subject must be 1 to 300 UTF-8 bytes without newlines")
	}
	if strings.TrimSpace(message.Text) == "" || len(message.Text)+len(message.HTML) > MaxBodyBytes || !utf8.ValidString(message.Text) || !utf8.ValidString(message.HTML) {
		return fmt.Errorf("email requires plain text; combined text and HTML must be at most 256 KiB of UTF-8")
	}
	return nil
}
