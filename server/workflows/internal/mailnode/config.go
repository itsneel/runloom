package mailnode

import (
	"fmt"
	"github.com/example/runloom/workflows/internal/config"
	"net"
	"strconv"
	"strings"
)

type Config struct {
	Host           string
	Port           int
	From           string
	TLS            string
	Username       string
	PasswordFile   string
	AllowedDomains []string
}

func FromEnv() (Config, error) {
	value := Config{Host: config.Value("RUNLOOM_SMTP_HOST"), From: config.Value("RUNLOOM_SMTP_FROM"), TLS: config.Value("RUNLOOM_SMTP_TLS"), Username: config.Value("RUNLOOM_SMTP_USERNAME"), PasswordFile: config.Value("RUNLOOM_SMTP_PASSWORD_FILE")}
	if value.Host == "" {
		return Config{}, nil
	}
	if value.TLS == "" {
		value.TLS = "starttls"
	}
	value.Port = 587
	if value.TLS == "tls" {
		value.Port = 465
	}
	if raw := config.Value("RUNLOOM_SMTP_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("RUNLOOM_SMTP_PORT must be a number")
		}
		value.Port = port
	}
	if raw := config.Value("RUNLOOM_SMTP_ALLOWED_DOMAINS"); raw != "" {
		value.AllowedDomains = strings.Split(raw, ",")
	}
	for i, domain := range value.AllowedDomains {
		value.AllowedDomains[i] = strings.ToLower(strings.TrimSpace(domain))
	}
	return value, value.Validate()
}

func (c Config) Validate() error {
	if c.Host == "" || strings.ContainsAny(c.Host, "/\r\n\x00 ") || c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("SMTP host and port are invalid")
	}
	if strings.Contains(c.Host, ":") && net.ParseIP(c.Host) == nil {
		return fmt.Errorf("SMTP host must not contain a port or URL")
	}
	if err := Address(c.From); err != nil {
		return fmt.Errorf("SMTP from must be a plain email address")
	}
	if c.TLS != "starttls" && c.TLS != "tls" && c.TLS != "none" {
		return fmt.Errorf("SMTP TLS mode must be starttls, tls, or none (local testing only)")
	}
	if (c.Username == "") != (c.PasswordFile == "") {
		return fmt.Errorf("SMTP username and password file must be configured together")
	}
	if c.TLS == "none" && c.Username != "" {
		return fmt.Errorf("SMTP authentication requires TLS")
	}
	for _, domain := range c.AllowedDomains {
		if domain == "" || strings.ContainsAny(domain, "@/*\r\n ") || Address("test@"+domain) != nil {
			return fmt.Errorf("SMTP allowed domains must be exact email domains, without wildcards")
		}
	}
	return nil
}
