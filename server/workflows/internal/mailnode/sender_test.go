package mailnode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/model"
)

type smtpCapture struct {
	recipients []string
	body       []byte
}

// This loopback-only SMTP server deliberately has no TLS or authentication.
func fakeSMTP(t *testing.T, reject bool) (Config, <-chan smtpCapture) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan smtpCapture, 1)
	go func() {
		capture := smtpCapture{}
		defer func() { done <- capture }()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader := textproto.NewReader(bufio.NewReader(conn))
		fmt.Fprint(conn, "220 local.test ESMTP\r\n")
		for {
			line, err := reader.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM"):
				fmt.Fprint(conn, "250 local.test\r\n")
			case strings.HasPrefix(line, "RCPT TO"):
				capture.recipients = append(capture.recipients, line)
				if reject {
					fmt.Fprint(conn, "550 private-provider-detail\r\n")
				} else {
					fmt.Fprint(conn, "250 OK\r\n")
				}
			case line == "DATA":
				fmt.Fprint(conn, "354 Continue\r\n")
				capture.body, _ = reader.ReadDotBytes()
				fmt.Fprint(conn, "250 accepted\r\n")
			case line == "QUIT":
				fmt.Fprint(conn, "221 Bye\r\n")
				return
			default:
				fmt.Fprint(conn, "500 unsupported\r\n")
			}
		}
	}()
	return Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, From: "sender@example.test", TLS: "none"}, done
}

func TestSendEnvelopeMIMEAndBCC(t *testing.T) {
	config, done := fakeSMTP(t, false)
	message := model.EmailMessage{To: []string{"to@example.test"}, CC: []string{"cc@example.test"}, BCC: []string{"hidden@example.test", "to@example.test"}, ReplyTo: "reply@example.test", Subject: "Hello 世界", Text: "Plain 世界\n.line", HTML: "<p>Hello 世界</p>"}
	result, err := (&Sender{Config: config}).Send(context.Background(), message)
	if err != nil || !result.Accepted || result.RecipientCount != 3 || result.MessageID == "" {
		t.Fatal(result, err)
	}
	capture := <-done
	if len(capture.recipients) != 3 || !strings.Contains(capture.recipients[2], "hidden@example.test") {
		t.Fatal(capture.recipients)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(capture.body))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("Bcc") != "" || strings.Contains(string(capture.body), "hidden@example.test") {
		t.Fatal("BCC leaked into message headers")
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != message.Subject {
		t.Fatal(subject, err)
	}
	_, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := multipart.NewReader(parsed.Body, params["boundary"])
	for _, want := range []string{message.Text, message.HTML} {
		part, err := parts.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil || string(body) != want {
			t.Fatal(string(body), err)
		}
	}
}

func TestSMTPRejectsBeforeBodyAndRequiresTLS(t *testing.T) {
	message := model.EmailMessage{To: []string{"to@example.test"}, Subject: "test", Text: "private body"}
	for _, kind := range []string{"recipient", "tls"} {
		t.Run(kind, func(t *testing.T) {
			config, done := fakeSMTP(t, kind == "recipient")
			if kind == "tls" {
				config.TLS = "starttls"
			}
			_, err := (&Sender{Config: config}).Send(context.Background(), message)
			if err == nil || strings.Contains(err.Error(), "private-provider-detail") {
				t.Fatal(err)
			}
			capture := <-done
			if len(capture.body) != 0 {
				t.Fatal("body sent after rejected recipient or without required TLS")
			}
		})
	}
}

func TestSMTPValidationAndCancellation(t *testing.T) {
	base := model.EmailMessage{To: []string{"to@example.test"}, Subject: "test", Text: "body"}
	for _, bad := range []string{"test\r\nBcc: stolen@example.test", "bad\x00subject"} {
		message := base
		message.Subject = bad
		if Validate(&message, false) == nil {
			t.Fatal("header injection accepted")
		}
	}
	message := base
	message.To = []string{"${input.email}"}
	if Validate(&message, true) != nil || Validate(&message, false) == nil {
		t.Fatal("template was not revalidated")
	}
	config := Config{Host: "127.0.0.1", Port: 1, From: "sender@example.test", TLS: "none", AllowedDomains: []string{"allowed.test"}}
	if _, err := (&Sender{Config: config}).Send(context.Background(), base); err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatal(err)
	}
	config.Username = "user"
	config.PasswordFile = "unused"
	if config.Validate() == nil {
		t.Fatal("plaintext authentication accepted")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	config = Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, From: "sender@example.test", TLS: "none"}
	start := time.Now()
	if _, err := (&Sender{Config: config}).Send(ctx, base); err == nil {
		t.Fatal("hung greeting accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not close SMTP connection")
	}
	<-done
}
