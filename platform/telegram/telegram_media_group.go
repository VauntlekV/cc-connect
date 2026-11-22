package telegram

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

// defaultMediaGroupWindow is how long we wait after the last album item before
// flushing. Telegram delivers each album item as a separate update, usually
// within a few hundred milliseconds of each other.
const defaultMediaGroupWindow = 600 * time.Millisecond

// mediaGroupBuffer collects the messages of one Telegram album (media group).
type mediaGroupBuffer struct {
	msgs  []*models.Message
	timer *time.Timer
}

func mediaGroupKey(msg *models.Message) string {
	return strconv.FormatInt(msg.Chat.ID, 10) + ":" + msg.MediaGroupID
}

// isAlbumItem reports whether msg belongs to an album we know how to merge.
func isAlbumItem(msg *models.Message) bool {
	return msg.MediaGroupID != "" && (len(msg.Photo) > 0 || msg.Document != nil)
}

func (p *Platform) mediaGroupDelay() time.Duration {
	if p.mediaGroupWindow > 0 {
		return p.mediaGroupWindow
	}
	return defaultMediaGroupWindow
}

// bufferMediaGroup stores an album item and (re)arms the flush timer. Group
// filtering is deferred to flush time because usually only the first item
// carries the caption / @mention / reply.
func (p *Platform) bufferMediaGroup(ctx context.Context, msg *models.Message) {
	key := mediaGroupKey(msg)
	delay := p.mediaGroupDelay()

	p.albumMu.Lock()
	defer p.albumMu.Unlock()
	if p.isStopping() {
		return
	}
	if p.albums == nil {
		p.albums = make(map[string]*mediaGroupBuffer)
	}
	buf, ok := p.albums[key]
	if !ok {
		buf = &mediaGroupBuffer{}
		p.albums[key] = buf
		buf.timer = time.AfterFunc(delay, func() { p.flushMediaGroup(ctx, key) })
	} else {
		buf.timer.Reset(delay)
	}
	buf.msgs = append(buf.msgs, msg)
}

// takeMediaGroup removes and returns the buffered album for key.
func (p *Platform) takeMediaGroup(key string) []*models.Message {
	p.albumMu.Lock()
	defer p.albumMu.Unlock()
	buf, ok := p.albums[key]
	if !ok {
		return nil
	}
	delete(p.albums, key)
	buf.timer.Stop()
	return buf.msgs
}

// stopMediaGroups drops all pending albums and stops their timers.
func (p *Platform) stopMediaGroups() {
	p.albumMu.Lock()
	defer p.albumMu.Unlock()
	for key, buf := range p.albums {
		buf.timer.Stop()
		delete(p.albums, key)
	}
}

func (p *Platform) flushMediaGroup(ctx context.Context, key string) {
	msgs := p.takeMediaGroup(key)
	if len(msgs) == 0 || p.isStopping() {
		return
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID })

	// Lead = first item with a caption (it carries mention/reply), else first.
	lead := msgs[0]
	for _, m := range msgs {
		if m.Caption != "" {
			lead = m
			break
		}
	}

	isGroup := lead.Chat.Type == models.ChatTypeGroup || lead.Chat.Type == models.ChatTypeSupergroup
	if isGroup && !p.groupReplyAll {
		directed := false
		for _, m := range msgs {
			if p.isDirectedAtBot(m) {
				directed = true
				break
			}
		}
		if !directed {
			slog.Debug("telegram: album not directed at bot", "media_group", key, "items", len(msgs))
			return
		}
	}

	userName := lead.From.Username
	if userName == "" {
		userName = strings.TrimSpace(lead.From.FirstName + " " + lead.From.LastName)
	}
	threadID := 0
	if lead.Chat.IsForum || !isGroup {
		threadID = lead.MessageThreadID
	}
	chatName := ""
	if isGroup {
		chatName = lead.Chat.Title
	}
	botName := p.botUsername()

	var images []core.ImageAttachment
	var files []core.FileAttachment
	var captions []string
	for _, m := range msgs {
		switch {
		case len(m.Photo) > 0:
			best := m.Photo[len(m.Photo)-1]
			data, err := p.downloadFile(best.FileID)
			if err != nil {
				slog.Error("telegram: download album photo failed", "error", err, "message_id", m.ID)
				continue
			}
			images = append(images, core.ImageAttachment{MimeType: "image/jpeg", Data: data})
		case m.Document != nil:
			data, err := p.downloadFile(m.Document.FileID)
			if err != nil {
				slog.Error("telegram: download album document failed", "error", err, "message_id", m.ID)
				continue
			}
			files = append(files, core.FileAttachment{MimeType: m.Document.MimeType, Data: data, FileName: m.Document.FileName})
		}
		if c := strings.TrimSpace(stripBotMention(m.Caption, botName)); c != "" {
			captions = append(captions, c)
		}
	}
	if len(images) == 0 && len(files) == 0 {
		return
	}

	slog.Info("telegram: album received", "user", userName, "items", len(msgs), "images", len(images), "files", len(files))
	if p.enableReactions {
		go p.reactToMessage(ctx, lead.Chat.ID, lead.ID, "⚡")
	}

	// Caption entity offsets only match a single caption; drop them otherwise.
	tgMsg := lead
	if len(captions) > 1 {
		cp := *lead
		cp.CaptionEntities = nil
		tgMsg = &cp
	}

	p.dispatchMessage(&core.Message{
		SessionKey: p.buildSessionKey(lead.Chat.ID, threadID, lead.From.ID), Platform: "telegram",
		UserID: strconv.FormatInt(lead.From.ID, 10), UserName: userName, ChatName: chatName,
		Content:    strings.Join(captions, "\n"),
		MessageID:  strconv.Itoa(lead.ID),
		ChannelKey: buildChannelKey(lead.Chat.ID, threadID),
		Images:     images,
		Files:      files,
		ReplyCtx:   replyContext{chatID: lead.Chat.ID, threadID: threadID, messageID: lead.ID},
	}, tgMsg)
}
