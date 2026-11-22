package telegram

// LOCAL PATCH telegram-forward-merge: Telegram delivers "forward with comment"
// as separate updates (comment first, then each forwarded message). Hold each
// message briefly so forwards from the same user join it as one agent turn.

import (
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/go-telegram/bot/models"
)

const defaultForwardWindow = 800 * time.Millisecond

type pendingForward struct {
	msg        *core.Message
	timer      *time.Timer
	hasComment bool // a non-forwarded message is already in the batch
}

func forwardLabel(o *models.MessageOrigin) string {
	name := ""
	switch {
	case o.MessageOriginUser != nil:
		u := o.MessageOriginUser.SenderUser
		name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		if u.Username != "" {
			name += " (@" + u.Username + ")"
		}
	case o.MessageOriginHiddenUser != nil:
		name = o.MessageOriginHiddenUser.SenderUserName
	case o.MessageOriginChat != nil:
		name = o.MessageOriginChat.SenderChat.Title
	case o.MessageOriginChannel != nil:
		name = o.MessageOriginChannel.Chat.Title
	}
	if name == "" {
		return "[转发]"
	}
	return "[转发自 " + name + "]"
}

func joinText(a, b string) string { return strings.TrimSpace(a + "\n\n" + b) }

// appendMsg folds src into dst, src after dst.
func appendMsg(dst, src *core.Message) {
	dst.Content = joinText(dst.Content, src.Content)
	dst.ExtraContent = joinText(dst.ExtraContent, src.ExtraContent)
	dst.Images = append(dst.Images, src.Images...)
	dst.Files = append(dst.Files, src.Files...)
}

func (p *Platform) holdForward(msg *core.Message, origin *models.MessageOrigin, handler core.MessageHandler) {
	isFwd := origin != nil
	if isFwd {
		msg.Content = strings.TrimSpace(forwardLabel(origin) + "\n" + msg.Content)
	}
	if p.forwardWindow <= 0 || (!isFwd && strings.HasPrefix(strings.TrimSpace(msg.Content), "/")) {
		if prev := p.takeForward(stickerKey(msg)); prev != nil {
			handler(p, prev)
		}
		handler(p, msg)
		return
	}

	key := stickerKey(msg)
	p.mu.Lock()
	pf := p.forwards[key]
	if pf != nil && (isFwd || !pf.hasComment) {
		if isFwd {
			appendMsg(pf.msg, msg)
		} else { // comment arrived after the forwards: put it first
			appendMsg(msg, pf.msg)
			pf.msg = msg
			pf.hasComment = true
		}
		pf.timer.Reset(p.forwardWindow)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	if prev := p.takeForward(key); prev != nil {
		handler(p, prev)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.forwards == nil {
		p.forwards = make(map[string]*pendingForward)
	}
	p.forwards[key] = &pendingForward{msg: msg, hasComment: !isFwd, timer: time.AfterFunc(p.forwardWindow, func() {
		if m := p.takeForward(key); m != nil && !p.isStopping() {
			handler(p, m)
		}
	})}
}

func (p *Platform) takeForward(key string) *core.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	pf := p.forwards[key]
	if pf == nil {
		return nil
	}
	pf.timer.Stop()
	delete(p.forwards, key)
	return pf.msg
}

func (p *Platform) hasPendingForward(key string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.forwards[key] != nil
}
