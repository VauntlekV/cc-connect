package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestLocalPatchModelFlag(t *testing.T) {
	s := &antigravitySession{model: "gemini-4-neon-high"}
	args := s.buildAntigravityArgs("", false, "yolo", "", "hi")
	n := len(args)
	if n < 4 || args[n-2] != "-p" || args[n-1] != "hi" {
		t.Fatalf("-p must stay last: %v", args)
	}
	i := slices.Index(args, "--model")
	if i < 0 || args[i+1] != "gemini-4-neon-high" {
		t.Fatalf("missing --model: %v", args)
	}
	s.model = ""
	if slices.Contains(s.buildAntigravityArgs("", false, "yolo", "", "hi"), "--model") {
		t.Fatal("--model should be omitted when unset")
	}
}

func TestLocalPatchParseAgyModels(t *testing.T) {
	out := "Fetching available models...\ngemini-4-neon-high\tGemini 4 Neon (High)\nclaude-opus-5-5-low\tClaude Opus 5.5 (Low)\n\n"
	m := parseAgyModels(out)
	if len(m) != 2 || m[0].Name != "gemini-4-neon-high" || m[1].Desc != "Claude Opus 5.5 (Low)" {
		t.Fatalf("got %+v", m)
	}
}

// LOCAL PATCH agy-resume: a fresh session must resume the conversation ID
// reported in the stream on the next turn.
func TestLocalPatchFreshSessionResumesStreamConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	cmdPath := filepath.Join(dir, "fake-agy.sh")
	script := "#!/bin/sh\necho \"$@\" >> " + argsFile + "\n" +
		`echo '{"event":"init","conversation_id":"conv-7"}'` + "\n" +
		`echo '{"event":"result","result":{"conversation_id":"conv-7","status":"SUCCESS","response":"ok"}}'` + "\n"
	if err := os.WriteFile(cmdPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := newAntigravitySession(context.Background(), cmdPath, nil, t.TempDir(), "", "yolo", "", nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	turn := func(p string) {
		if err := s.Send(p, "", nil, nil); err != nil {
			t.Fatal(err)
		}
		deadline := time.After(3 * time.Second)
		for {
			select {
			case ev := <-s.Events():
				if ev.Type == core.EventResult {
					if ev.SessionID != "conv-7" {
						t.Fatalf("result session = %q, want conv-7", ev.SessionID)
					}
					return
				}
			case <-deadline:
				t.Fatal("timeout")
			}
		}
	}
	turn("first")
	turn("second")

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "--conversation") || !strings.Contains(lines[1], "--conversation conv-7") {
		t.Fatalf("args per turn = %q", lines)
	}
}
