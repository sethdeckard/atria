package iterm

import (
	"os"
	"testing"
)

func TestCaptureAuthFromEnv(t *testing.T) {
	t.Setenv("ITERM2_COOKIE", "cookie-123")
	t.Setenv("ITERM2_KEY", "key-456")

	c := &conn{}
	c.captureAuthFromEnv()

	if c.cookie != "cookie-123" {
		t.Fatalf("expected cookie captured, got %q", c.cookie)
	}
	if c.key != "key-456" {
		t.Fatalf("expected key captured, got %q", c.key)
	}
	if got := c.buildHeaders().Get("x-iterm2-cookie"); got != "cookie-123" {
		t.Fatalf("expected header cookie, got %q", got)
	}
	if got := c.buildHeaders().Get("x-iterm2-key"); got != "key-456" {
		t.Fatalf("expected header key, got %q", got)
	}
	if got := os.Getenv("ITERM2_COOKIE"); got != "" {
		t.Fatalf("expected cookie removed from env, got %q", got)
	}
	if got := os.Getenv("ITERM2_KEY"); got != "" {
		t.Fatalf("expected key removed from env, got %q", got)
	}
}
