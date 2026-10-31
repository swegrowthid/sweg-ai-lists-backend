package config

import (
	"reflect"
	"testing"
)

// TestLoadSMTPConfigTrimsAndKeepsPasswordVerbatim proves SMTP values arrive
// from env ready for the mailer: host, port, username, and MAIL_FROM are
// trimmed, but the password is used exactly as set.
func TestLoadSMTPConfigTrimsAndKeepsPasswordVerbatim(t *testing.T) {
	t.Setenv("SMTP_HOST", "  smtp.sumopod.com  ")
	t.Setenv("SMTP_PORT", " 587 ")
	t.Setenv("SMTP_USERNAME", " user@smtp.test ")
	t.Setenv("SMTP_PASSWORD", "  raw password, keep spaces  ")
	t.Setenv("MAIL_FROM", " swegai <noreply@ai-sweg.my.id> ")

	cfg := Load()

	if cfg.SMTPHost != "smtp.sumopod.com" {
		t.Fatalf("SMTPHost = %q, want smtp.sumopod.com", cfg.SMTPHost)
	}
	if cfg.SMTPPort != "587" {
		t.Fatalf("SMTPPort = %q, want 587", cfg.SMTPPort)
	}
	if cfg.SMTPUsername != "user@smtp.test" {
		t.Fatalf("SMTPUsername = %q, want user@smtp.test", cfg.SMTPUsername)
	}
	if cfg.SMTPPassword != "  raw password, keep spaces  " {
		t.Fatalf("SMTPPassword = %q, want the raw value with spaces", cfg.SMTPPassword)
	}
	if cfg.MailFrom != "swegai <noreply@ai-sweg.my.id>" {
		t.Fatalf("MailFrom = %q, want the trimmed sender", cfg.MailFrom)
	}
}

// TestLoadSMTPPortDefaultsTo465 proves an empty or blank SMTP_PORT falls back
// to the implicit TLS port.
func TestLoadSMTPPortDefaultsTo465(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv("SMTP_PORT", raw)
			if got := Load().SMTPPort; got != "465" {
				t.Fatalf("SMTPPort = %q, want 465", got)
			}
		})
	}
}

// TestLoadHasNoJetEmailAPIKey guards the provider switch: the JetEmail setting
// must not come back.
func TestLoadHasNoJetEmailAPIKey(t *testing.T) {
	if _, ok := reflect.TypeOf(Config{}).FieldByName("JetEmailAPIKey"); ok {
		t.Fatal("Config still has a JetEmailAPIKey field")
	}
}
