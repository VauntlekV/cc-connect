package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func fwdTestMsg(id int, chat models.Chat, text string, fwd bool) *models.Message {
	m := &models.Message{
		ID: id, Date: int(time.Now().Unix()),
		From: &models.User{ID: 7, Username: "alice"},
		Chat: chat, Text: text,
	}
	if fwd {
		m.ForwardOrigin = &models.MessageOrigin{
			Type:              models.MessageOriginTypeUser,
			MessageOriginUser: &models.MessageOriginUser{SenderUser: models.User{FirstName: "agy", Username: "agy_bot"}},
		}
	}
	return m
}

func TestForwardsMergedWithComment(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	p.forwardWindow = 100 * time.Millisecond
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	p.handleMessage(ctx, fwdTestMsg(1, chat, "fix this", false))
	p.handleMessage(ctx, fwdTestMsg(2, chat, "bug report", true))
	p.handleMessage(ctx, fwdTestMsg(3, chat, "more details", true))

	got := expectOne(t, handled)
	want := "fix this\n\n[转发自 agy (@agy_bot)]\nbug report\n\n[转发自 agy (@agy_bot)]\nmore details"
	if got.Content != want {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestCommentAfterForwardGoesFirst(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	p.forwardWindow = 100 * time.Millisecond
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	p.handleMessage(ctx, fwdTestMsg(1, chat, "bug report", true))
	p.handleMessage(ctx, fwdTestMsg(2, chat, "fix this", false))

	got := expectOne(t, handled)
	if !strings.HasPrefix(got.Content, "fix this\n\n[转发自") {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestTwoPlainMessagesStaySeparate(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	p.forwardWindow = 100 * time.Millisecond
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	p.handleMessage(ctx, fwdTestMsg(1, chat, "one", false))
	p.handleMessage(ctx, fwdTestMsg(2, chat, "two", false))

	for _, want := range []string{"one", "two"} {
		select {
		case got := <-handled:
			if got.Content != want {
				t.Fatalf("content = %q, want %q", got.Content, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("%q not dispatched", want)
		}
	}
}

func TestGroupForwardJoinsMention(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	p.forwardWindow = 100 * time.Millisecond
	chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup, Title: "g"}
	ctx := context.Background()
	m := fwdTestMsg(1, chat, "@mybot look", false)
	m.Entities = []models.MessageEntity{{Type: models.MessageEntityTypeMention, Offset: 0, Length: 6}}
	p.handleMessage(ctx, m)
	p.handleMessage(ctx, fwdTestMsg(2, chat, "bug report", true))

	got := expectOne(t, handled)
	if !strings.Contains(got.Content, "look") || !strings.Contains(got.Content, "bug report") {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestGroupStrayForwardIgnored(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	p.forwardWindow = 100 * time.Millisecond
	chat := models.Chat{ID: -100, Type: models.ChatTypeSupergroup, Title: "g"}
	p.handleMessage(context.Background(), fwdTestMsg(1, chat, "bug report", true))
	select {
	case got := <-handled:
		t.Fatalf("unexpected dispatch: %q", got.Content)
	case <-time.After(300 * time.Millisecond):
	}
}
