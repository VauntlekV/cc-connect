package telegram

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

type fixedBodyTransport struct{}

func (fixedBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data")),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func newAlbumTestPlatform(groupReplyAll bool) (*Platform, chan *core.Message) {
	handled := make(chan *core.Message, 4)
	p := &Platform{
		token:            "token",
		httpClient:       &http.Client{Transport: fixedBodyTransport{}},
		groupReplyAll:    groupReplyAll,
		mediaGroupWindow: 50 * time.Millisecond,
	}
	p.handler = func(_ core.Platform, msg *core.Message) { handled <- msg }
	p.bot = newStubTelegramBot()
	p.selfUser = &models.User{ID: 42, Username: "mybot"}
	return p, handled
}

func albumPhoto(id int, chat models.Chat, group, caption string) *models.Message {
	m := &models.Message{
		ID:           id,
		Date:         int(time.Now().Unix()),
		From:         &models.User{ID: 7, Username: "alice"},
		Chat:         chat,
		MediaGroupID: group,
		Caption:      caption,
		Photo:        []models.PhotoSize{{FileID: "small"}, {FileID: "big"}},
	}
	if strings.HasPrefix(caption, "@mybot") {
		m.CaptionEntities = []models.MessageEntity{{Type: models.MessageEntityTypeMention, Offset: 0, Length: 6}}
	}
	return m
}

func expectOne(t *testing.T, handled chan *core.Message) *core.Message {
	t.Helper()
	var got *core.Message
	select {
	case got = <-handled:
	case <-time.After(time.Second):
		t.Fatal("album not dispatched")
	}
	select {
	case extra := <-handled:
		t.Fatalf("unexpected extra dispatch: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}
	return got
}

func TestMediaGroupMergedPrivate(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	// Out of order; caption on the middle item.
	p.handleMessage(ctx, albumPhoto(12, chat, "g1", ""))
	p.handleMessage(ctx, albumPhoto(11, chat, "g1", "look at these"))
	p.handleMessage(ctx, albumPhoto(10, chat, "g1", ""))

	got := expectOne(t, handled)
	if len(got.Images) != 3 {
		t.Fatalf("images = %d, want 3", len(got.Images))
	}
	if got.Content != "look at these" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.MessageID != "11" {
		t.Fatalf("MessageID = %q, want captioned item 11", got.MessageID)
	}
	if got.SessionKey != "telegram:100:7" {
		t.Fatalf("SessionKey = %q", got.SessionKey)
	}
}

func TestMediaGroupGroupMentionOnFirstOnly(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	chat := models.Chat{ID: -5, Type: models.ChatTypeSupergroup, Title: "G"}
	ctx := context.Background()
	p.handleMessage(ctx, albumPhoto(20, chat, "g2", "@mybot what is this"))
	p.handleMessage(ctx, albumPhoto(21, chat, "g2", ""))
	p.handleMessage(ctx, albumPhoto(22, chat, "g2", ""))

	got := expectOne(t, handled)
	if len(got.Images) != 3 {
		t.Fatalf("images = %d, want 3", len(got.Images))
	}
	if got.Content != "what is this" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.ChatName != "G" {
		t.Fatalf("ChatName = %q", got.ChatName)
	}
}

func TestMediaGroupGroupReplyToBot(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	chat := models.Chat{ID: -5, Type: models.ChatTypeSupergroup}
	ctx := context.Background()
	first := albumPhoto(30, chat, "g3", "")
	first.ReplyToMessage = &models.Message{ID: 1, From: &models.User{ID: 42}}
	p.handleMessage(ctx, first)
	p.handleMessage(ctx, albumPhoto(31, chat, "g3", ""))

	got := expectOne(t, handled)
	if len(got.Images) != 2 {
		t.Fatalf("images = %d, want 2", len(got.Images))
	}
}

func TestMediaGroupGroupNotDirectedDropped(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	chat := models.Chat{ID: -5, Type: models.ChatTypeSupergroup}
	ctx := context.Background()
	p.handleMessage(ctx, albumPhoto(40, chat, "g4", "holiday pics"))
	p.handleMessage(ctx, albumPhoto(41, chat, "g4", ""))

	select {
	case got := <-handled:
		t.Fatalf("undirected album dispatched: %+v", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestMediaGroupSeparateAlbums(t *testing.T) {
	p, handled := newAlbumTestPlatform(true)
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	p.handleMessage(ctx, albumPhoto(50, chat, "a", "one"))
	p.handleMessage(ctx, albumPhoto(60, chat, "b", "two"))
	p.handleMessage(ctx, albumPhoto(51, chat, "a", ""))
	p.handleMessage(ctx, albumPhoto(61, chat, "b", ""))

	seen := map[string]int{}
	for i := 0; i < 2; i++ {
		select {
		case got := <-handled:
			seen[got.Content] = len(got.Images)
		case <-time.After(time.Second):
			t.Fatal("album not dispatched")
		}
	}
	if seen["one"] != 2 || seen["two"] != 2 {
		t.Fatalf("seen = %v", seen)
	}
}

func TestMediaGroupDocuments(t *testing.T) {
	p, handled := newAlbumTestPlatform(false)
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	ctx := context.Background()
	doc := func(id int, name, caption string) *models.Message {
		return &models.Message{
			ID: id, Date: int(time.Now().Unix()),
			From: &models.User{ID: 7}, Chat: chat, MediaGroupID: "d",
			Caption:  caption,
			Document: &models.Document{FileID: name, FileName: name, MimeType: "text/plain"},
		}
	}
	p.handleMessage(ctx, doc(70, "a.txt", "first"))
	p.handleMessage(ctx, doc(71, "b.txt", "second"))

	got := expectOne(t, handled)
	if len(got.Files) != 2 || got.Files[0].FileName != "a.txt" || got.Files[1].FileName != "b.txt" {
		t.Fatalf("files = %+v", got.Files)
	}
	if got.Content != "first\nsecond" {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestMediaGroupStopDropsPending(t *testing.T) {
	p, handled := newAlbumTestPlatform(true)
	chat := models.Chat{ID: 100, Type: models.ChatTypePrivate}
	p.handleMessage(context.Background(), albumPhoto(80, chat, "s", "x"))
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	p.albumMu.Lock()
	n := len(p.albums)
	p.albumMu.Unlock()
	if n != 0 {
		t.Fatalf("pending albums after Stop = %d", n)
	}
	select {
	case got := <-handled:
		t.Fatalf("dispatched after Stop: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
	// Items arriving after Stop are ignored.
	p.handleMessage(context.Background(), albumPhoto(81, chat, "s2", "y"))
	p.albumMu.Lock()
	n = len(p.albums)
	p.albumMu.Unlock()
	if n != 0 {
		t.Fatalf("album buffered after Stop")
	}
}
