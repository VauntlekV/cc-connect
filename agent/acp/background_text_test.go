package acp

import (
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// Out-of-turn text (background work, e.g. kimi acp's deferred
// "Compaction completed" chunk) must be closed with a synthetic
// EventResult so the unsolicited reader relays it to the platform
// instead of dropping it as stale at the next turn.
func TestSession_OutOfTurnTextGetsSyntheticResult(t *testing.T) {
	s, wResp, _ := newTestSession(t, nil)

	go func() {
		_, _ = wResp.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"test-session-id","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Compaction completed."}}}}` + "\n"))
	}()

	var got []core.Event
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case ev := <-s.Events():
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("want EventText + synthetic EventResult, got %+v", got)
		}
	}
	if got[0].Type != core.EventText || got[0].Content != "Compaction completed." {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Type != core.EventResult || !got[1].Done {
		t.Fatalf("second event = %+v, want synthetic EventResult", got[1])
	}
}

// Text chunks of a foreground turn (prompt RPC in flight) must NOT get a
// synthetic EventResult — the turn's own result follows when the RPC
// returns.
func TestSession_InFlightTextGetsNoSyntheticResult(t *testing.T) {
	s, wResp, _ := newTestSession(t, nil)
	s.promptInFlight.Store(1)
	defer s.promptInFlight.Store(0)

	go func() {
		_, _ = wResp.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"test-session-id","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"streamed chunk"}}}}` + "\n"))
	}()

	select {
	case ev := <-s.Events():
		if ev.Type != core.EventText {
			t.Fatalf("first event = %+v, want EventText", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventText")
	}

	select {
	case ev := <-s.Events():
		t.Fatalf("unexpected extra event while prompt in flight: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

// Non-text out-of-turn updates (e.g. tool_call progress with no visible
// text) must not produce a synthetic result either.
func TestSession_OutOfTurnNonTextGetsNoSyntheticResult(t *testing.T) {
	s, wResp, _ := newTestSession(t, nil)

	go func() {
		_, _ = wResp.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"test-session-id","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc1","status":"in_progress"}}}` + "\n"))
	}()

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == core.EventResult {
				t.Fatalf("unexpected synthetic EventResult: %+v", ev)
			}
			// tool_call_update legitimately maps to a progress event; keep
			// watching for a wrongly-appended EventResult.
		case <-deadline:
			return
		}
	}
}

// A late reply streamed as several out-of-turn chunks must be closed by a
// single synthetic EventResult, so the platform posts it as one message.
func TestSession_OutOfTurnChunksShareOneResult(t *testing.T) {
	old := backgroundResultDelay
	backgroundResultDelay = 200 * time.Millisecond
	defer func() { backgroundResultDelay = old }()
	s, wResp, _ := newTestSession(t, nil)

	go func() {
		for _, txt := range []string{"part one, ", "part two, ", "part three"} {
			_, _ = wResp.Write([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"test-session-id","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + txt + `"}}}}` + "\n"))
			time.Sleep(50 * time.Millisecond)
		}
	}()

	var texts, results int
	deadline := time.After(2 * time.Second)
	for results == 0 {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case core.EventText:
				texts++
			case core.EventResult:
				results++
			}
		case <-deadline:
			t.Fatalf("no synthetic EventResult (texts=%d)", texts)
		}
	}
	if texts != 3 {
		t.Fatalf("result arrived after %d text chunks, want all 3", texts)
	}
	select {
	case ev := <-s.Events():
		t.Fatalf("unexpected extra event %+v", ev)
	case <-time.After(400 * time.Millisecond):
	}
}
