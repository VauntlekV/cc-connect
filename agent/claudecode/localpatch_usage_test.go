package claudecode

import "testing"

// LOCAL PATCH (claude-usage-trust)
func TestTrustPromptCursorOnNo(t *testing.T) {
	newer := "Quick safety check: Is this a project you created or one you trust?\n❯ No, exit\n  Yes, I trust this folder\nEnter to confirm · Esc to cancel"
	if got := promptActionForScreen(newer); got != "\x1b[B\r" {
		t.Fatalf("cursor on No: got %q", got)
	}
	older := "Quick safety check\n❯ 1. Yes, I trust this folder\n  2. No, exit\nEnter to confirm"
	if got := promptActionForScreen(older); got != "\r" {
		t.Fatalf("cursor on Yes: got %q", got)
	}
}

func TestTerminalCursorHorizontalAbsolute(t *testing.T) {
	term := newClaudeUsageTerminal()
	term.Write([]byte("\x1b[2GQuick\x1b[8Gsafety\x1b[15Gcheck:"))
	if got := term.String(); got != " Quick safety check:" {
		t.Fatalf("got %q", got)
	}
}
