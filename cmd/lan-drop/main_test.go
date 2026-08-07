package main

import "testing"

func TestValidAccessCode(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"482901":      true,
		"room_7-code": true,
		"abc":         false,
		"with space":  false,
		"bad;cookie":  false,
		"中文连接码":       false,
	}
	for code, want := range tests {
		if got := validAccessCode(code); got != want {
			t.Errorf("validAccessCode(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestRandomCode(t *testing.T) {
	t.Parallel()
	code, err := randomCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || !validAccessCode(code) {
		t.Fatalf("random code = %q", code)
	}
}

func TestPreferredShareURL(t *testing.T) {
	t.Parallel()
	urls := []string{
		"http://127.0.0.1:8080/?token=482901",
		"http://192.168.1.20:8080/?token=482901",
	}
	if got := preferredShareURL(urls); got != urls[1] {
		t.Fatalf("preferredShareURL() = %q", got)
	}
	if got := preferredShareURL(urls[:1]); got != urls[0] {
		t.Fatalf("loopback fallback = %q", got)
	}
}
