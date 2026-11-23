package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func stickerMsg(id int, chat models.Chat) *models.Message {
	return &models.Message{
		ID: id, Date: int(time.Now().Unix()),
		From:    &models.User{ID: 7, Username: "alice"},
		Chat:    chat,
		Sticker: &models.Sticker{FileID: "st", Emoji: "😅"},
	}
}

func TestStickerMergedWithFollowingText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, handled := newAlbumTestPlatform(false)
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	p.handleMessage(ctx, stickerMsg(1, chat))
	p.handleMessage(ctx, &models.Message{ID: 2, Date: int(time.Now().Unix()), From: &models.User{ID: 7, Username: "alice"}, Chat: chat, Text: "hi"})

	got := expectOne(t, handled)
	if got.Content != "[贴纸 😅]\nhi" || len(got.Images) != 1 {
		t.Fatalf("content = %q, images = %d", got.Content, len(got.Images))
	}
}

func TestStickerAloneFlushes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, handled := newAlbumTestPlatform(false)
	p.handleMessage(context.Background(), stickerMsg(1, models.Chat{ID: 100, Type: models.ChatTypePrivate}))
	select {
	case got := <-handled:
		t.Fatalf("dispatched before window: %+v", got)
	case <-time.After(stickerWindow - 500*time.Millisecond):
	}
	select {
	case got := <-handled:
		if got.Content != "[贴纸 😅]" {
			t.Fatalf("content = %q", got.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("sticker not flushed")
	}
}

func TestCommandSkipsStickerMerge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, handled := newAlbumTestPlatform(false)
	p.forwardWindow = 500 * time.Millisecond
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	p.handleMessage(ctx, stickerMsg(1, chat))
	p.handleMessage(ctx, &models.Message{ID: 2, Date: int(time.Now().Unix()), From: &models.User{ID: 7, Username: "alice"}, Chat: chat, Text: "/stop"})

	select {
	case got := <-handled:
		if got.Content != "/stop" || len(got.Images) != 0 {
			t.Fatalf("command polluted by sticker: content=%q images=%d", got.Content, len(got.Images))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("/stop delayed or not dispatched")
	}
}
