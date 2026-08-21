package main

import (
	"strings"
	"testing"
)

func TestDisplayNameColorsIconsBySource(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{kind: "herdr", want: "\x1b[34m●\x1b[39m session"},
		{kind: "config", want: "\x1b[90m⚙\x1b[39m session"},
		{kind: "zoxide", want: "\x1b[36m\x1b[39m session"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := (Candidate{Kind: tt.kind, Name: "session"}).displayName(true)
			if got != tt.want {
				t.Fatalf("displayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayNameColorsCustomActiveSessionIcon(t *testing.T) {
	got := (Candidate{Kind: "herdr", Name: "session", Icon: "󰆍"}).displayName(true)
	want := "\x1b[34m󰆍\x1b[39m session"
	if got != want {
		t.Fatalf("displayName() = %q, want %q", got, want)
	}
}

func TestDisplayNameWithoutIconsHasNoANSIColor(t *testing.T) {
	got := (Candidate{Kind: "herdr", Name: "session"}).displayName(false)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("displayName() unexpectedly contains ANSI color: %q", got)
	}
	if got != "session" {
		t.Fatalf("displayName() = %q, want %q", got, "session")
	}
}
