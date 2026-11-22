package telegram

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

// stickerWindow is how long a sticker waits for a follow-up message from the
// same user so both reach the agent as one turn. Only stickers are delayed.
const stickerWindow = 2 * time.Second

type pendingSticker struct {
	msg   *core.Message
	timer *time.Timer
}

func stickerKey(msg *core.Message) string { return msg.SessionKey + "|" + msg.UserID }

// holdSticker parks a sticker; a previous sticker still waiting is sent first.
func (p *Platform) holdSticker(msg *core.Message, handler core.MessageHandler) {
	key := stickerKey(msg)
	if prev := p.takeSticker(key); prev != nil {
		handler(p, prev)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stickers == nil {
		p.stickers = make(map[string]*pendingSticker)
	}
	p.stickers[key] = &pendingSticker{msg: msg, timer: time.AfterFunc(stickerWindow, func() {
		if m := p.takeSticker(key); m != nil && !p.isStopping() {
			handler(p, m)
		}
	})}
}

func (p *Platform) takeSticker(key string) *core.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	ps := p.stickers[key]
	if ps == nil {
		return nil
	}
	ps.timer.Stop()
	delete(p.stickers, key)
	return ps.msg
}

// mergeSticker folds a waiting sticker into msg, sticker first.
func (p *Platform) mergeSticker(msg *core.Message) {
	st := p.takeSticker(stickerKey(msg))
	if st == nil {
		return
	}
	msg.Images = append(st.Images, msg.Images...)
	msg.Content = strings.TrimSpace(st.Content + "\n" + msg.Content)
	if st.ExtraContent != "" {
		msg.ExtraContent = strings.TrimSpace(st.ExtraContent + "\n" + msg.ExtraContent)
	}
}

// logSticker appends a received sticker to ~/.cc-connect/stickers.jsonl so
// agents can reuse it (see tg-sticker). Duplicates are deduped by the reader.
func logSticker(st *models.Sticker) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	line, _ := json.Marshal(map[string]any{
		"file_id": st.FileID, "file_unique_id": st.FileUniqueID, "emoji": st.Emoji,
		"set_name": st.SetName, "is_animated": st.IsAnimated, "is_video": st.IsVideo,
	})
	f, err := os.OpenFile(filepath.Join(home, ".cc-connect", "stickers.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Warn("telegram: log sticker failed", "error", err)
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}
