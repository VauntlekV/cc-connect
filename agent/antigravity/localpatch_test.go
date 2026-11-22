package antigravity

import (
	"slices"
	"testing"
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
