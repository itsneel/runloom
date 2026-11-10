package mailnode

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/runloom/workflows/internal/model"
	"github.com/google/uuid"
)

type Sender struct{ Config Config }
type Result struct {
	Accepted       bool   `json:"accepted"`
	MessageID      string `json:"message_id"`
	RecipientCount int    `json:"recipient_count"`
}

func (s *Sender) Send(ctx context.Context, message model.EmailMessage) (Result, error) {
	var result Result
	if err := Validate(&message, false); err != nil {
		return result, err
	}
	if s == nil || s.Config.Host == "" {
		return result, fmt.Errorf("Email delivery is not configured on the worker")
	}
	c := s.Config
	if err := c.Validate(); err != nil {
		return result, err
	}
	recipients := []string{}
	seen := map[string]bool{}
	for _, list := range [][]string{message.To, message.CC, message.BCC} {
		for _, address := range list {
			domain := strings.ToLower(strings.SplitN(address, "@", 2)[1])
			if len(c.AllowedDomains) > 0 {
				allowed := false
				for _, item := range c.AllowedDomains {
					if domain == item {
						allowed = true
						break
					}
				}
				if !allowed {
					return result, fmt.Errorf("recipient domain is not allowed by the SMTP configuration")
				}
			}
			if !seen[address] {
				recipients = append(recipients, address)
				seen[address] = true
			}
		}
	}
	password := ""
	if c.PasswordFile != "" {
		file, err := os.Open(c.PasswordFile)
		if err != nil {
			return result, fmt.Errorf("SMTP password file could not be read")
		}
		raw, err := io.ReadAll(io.LimitReader(file, 8193))
		file.Close()
		if err != nil || len(raw) > 8192 {
			return result, fmt.Errorf("SMTP password file is invalid")
		}
		password = strings.TrimRight(string(raw), "\r\n")
		if password == "" {
			return result, fmt.Errorf("SMTP password file is empty")
		}
	}
	// Every SMTP phase shares a hard deadline, including greeting, TLS and DATA.
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	address := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return result, fmt.Errorf("SMTP connection failed")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var transport net.Conn = conn
	if c.TLS == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return result, fmt.Errorf("SMTP TLS handshake failed")
		}
		transport = secure
	}
	client, err := smtp.NewClient(transport, c.Host)
	if err != nil {
		return result, fmt.Errorf("SMTP greeting failed")
	}
	defer client.Close()
	if c.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return result, fmt.Errorf("SMTP server must support STARTTLS; plaintext fallback is disabled")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return result, fmt.Errorf("SMTP TLS handshake failed")
		}
	}
	if c.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", c.Username, password, c.Host)); err != nil {
			return result, fmt.Errorf("SMTP authentication failed")
		}
	}
	// Do not reveal server replies: they can echo recipients or credentials.
	if err := client.Mail(c.From); err != nil {
		return result, fmt.Errorf("SMTP sender was rejected")
	}
	for _, address := range recipients {
		if err := client.Rcpt(address); err != nil {
			return result, fmt.Errorf("SMTP recipient was rejected; no message body sent")
		}
	}
	id := "<" + uuid.NewString() + "@" + strings.SplitN(c.From, "@", 2)[1] + ">"
	body, err := messageBytes(c.From, id, message)
	if err != nil {
		return result, fmt.Errorf("could not encode email message")
	}
	writer, err := client.Data()
	if err != nil {
		return result, fmt.Errorf("SMTP message was rejected before sending")
	}
	if _, err = writer.Write(body); err != nil {
		return result, fmt.Errorf("SMTP send interrupted; delivery may have occurred, check the provider before retrying")
	}
	if err = writer.Close(); err != nil {
		return result, fmt.Errorf("SMTP acknowledgement missing; delivery may have occurred, check the provider before retrying")
	}
	// DATA's final 250 is the acceptance boundary. A failed QUIT must not turn a
	// successful send into an error that encourages duplicate delivery.
	return Result{Accepted: true, MessageID: id, RecipientCount: len(recipients)}, nil
}

func messageBytes(from, id string, message model.EmailMessage) ([]byte, error) {
	var body bytes.Buffer
	fmt.Fprintf(&body, "From: %s\r\nTo: %s\r\n", from, strings.Join(message.To, ",\r\n "))
	if len(message.CC) > 0 {
		fmt.Fprintf(&body, "Cc: %s\r\n", strings.Join(message.CC, ",\r\n "))
	}
	if message.ReplyTo != "" {
		fmt.Fprintf(&body, "Reply-To: %s\r\n", message.ReplyTo)
	}
	// Bcc is deliberately envelope-only. Bodies are base64 with bounded lines.
	fmt.Fprintf(&body, "Subject: %s\r\nMessage-ID: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n", mime.QEncoding.Encode("utf-8", message.Subject), id, time.Now().UTC().Format(time.RFC1123Z))
	if message.HTML == "" {
		body.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		writeBody(&body, message.Text)
	} else {
		multipartWriter := multipart.NewWriter(&body)
		fmt.Fprintf(&body, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", multipartWriter.Boundary())
		for _, part := range []struct{ kind, text string }{{"text/plain", message.Text}, {"text/html", message.HTML}} {
			writer, err := multipartWriter.CreatePart(textproto.MIMEHeader{"Content-Type": {part.kind + "; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"}})
			if err != nil {
				return nil, err
			}
			writeBody(writer, part.text)
		}
		if err := multipartWriter.Close(); err != nil {
			return nil, err
		}
	}
	return body.Bytes(), nil
}

func writeBody(writer io.Writer, body string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		fmt.Fprint(writer, encoded[:76], "\r\n")
		encoded = encoded[76:]
	}
	fmt.Fprint(writer, encoded, "\r\n")
}
