package telegram

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestEnrichReplyTrim(t *testing.T) {
	long := strings.Repeat("长", 300)
	bot := &models.User{Username: "my_bot", IsBot: true}
	other := &models.User{Username: "someone"}

	got := enrichReplyContent(&models.Message{ReplyToMessage: &models.Message{From: bot, Text: long}}, "My_Bot")
	if !strings.HasSuffix(got, "…") || len([]rune(got)) > 230 {
		t.Fatalf("own message not trimmed: %d runes", len([]rune(got)))
	}
	got = enrichReplyContent(&models.Message{ReplyToMessage: &models.Message{From: other, Text: long}}, "my_bot")
	if !strings.Contains(got, long) {
		t.Fatal("other user's message should be kept in full")
	}
	got = enrichReplyContent(&models.Message{
		ReplyToMessage: &models.Message{From: bot, Text: long},
		Quote:          &models.TextQuote{Text: "选中的一段", IsManual: true},
	}, "my_bot")
	if got != "[Reply to @my_bot]: 「选中的一段」" {
		t.Fatalf("quote not used: %q", got)
	}
	got = enrichReplyContent(&models.Message{ReplyToMessage: &models.Message{From: bot, Text: "短消息"}}, "my_bot")
	if got != "[Reply to @my_bot]: 短消息" {
		t.Fatalf("short own message changed: %q", got)
	}
}
