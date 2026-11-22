package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const relayTimeout = 120 * time.Second

const (
	RelayVisibilityFull    = "full"
	RelayVisibilitySummary = "summary"
	RelayVisibilityNone    = "none"
)

// RelayBinding represents a bot-to-bot relay binding in a group chat.
type RelayBinding struct {
	Platform string            `json:"platform"`
	ChatID   string            `json:"chat_id"`
	Bots     map[string]string `json:"bots"` // project name → bot display name
}

// RelayManager coordinates bot-to-bot message relay across engines.
type RelayManager struct {
	mu         sync.RWMutex
	engines    map[string]*Engine       // project name → engine (runtime only)
	bindings   map[string]*RelayBinding // chatID → binding
	storePath  string                   // empty = no persistence
	timeout    time.Duration
	visibility string
}

func NewRelayManager(dataDir string) *RelayManager {
	rm := &RelayManager{
		engines:    make(map[string]*Engine),
		bindings:   make(map[string]*RelayBinding),
		timeout:    relayTimeout,
		visibility: RelayVisibilityFull,
	}
	if dataDir != "" {
		rm.storePath = filepath.Join(dataDir, "relay_bindings.json")
		rm.load()
	}
	return rm
}

func (rm *RelayManager) RegisterEngine(name string, e *Engine) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.engines[name] = e
}

// SetTimeout overrides the relay response timeout. Set to 0 to disable it.
func (rm *RelayManager) SetTimeout(d time.Duration) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if d < 0 {
		d = 0
	}
	rm.timeout = d
}

// SetVisibility controls whether relay request/response visibility messages are
// echoed into the source group chat. The relay transport still returns the
// target response to the caller regardless of this setting.
func (rm *RelayManager) SetVisibility(mode string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.visibility = normalizeRelayVisibility(mode)
}

// Bind establishes a relay binding between bots in a group chat.
// If a binding already exists, it will be replaced.
func (rm *RelayManager) Bind(platform, chatID string, bots map[string]string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.bindings[chatID] = &RelayBinding{
		Platform: platform,
		ChatID:   chatID,
		Bots:     bots,
	}
	slog.Info("relay: binding created", "chat_id", chatID, "bots", bots)
	rm.saveLocked()
}

// AddToBind adds a project to an existing binding, or creates a new one.
func (rm *RelayManager) AddToBind(platform, chatID, projectName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	binding := rm.bindings[chatID]
	if binding == nil {
		binding = &RelayBinding{
			Platform: platform,
			ChatID:   chatID,
			Bots:     make(map[string]string),
		}
		rm.bindings[chatID] = binding
	}

	binding.Bots[projectName] = projectName
	slog.Info("relay: project added to binding", "chat_id", chatID, "project", projectName, "bots", binding.Bots)
	rm.saveLocked()
}

// RemoveFromBind removes a project from an existing binding.
// Returns true if the project was removed, false if not found.
func (rm *RelayManager) RemoveFromBind(chatID, projectName string) bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	binding := rm.bindings[chatID]
	if binding == nil {
		return false
	}

	if _, exists := binding.Bots[projectName]; exists {
		delete(binding.Bots, projectName)
		slog.Info("relay: project removed from binding", "chat_id", chatID, "project", projectName, "remaining", binding.Bots)

		if len(binding.Bots) == 0 {
			delete(rm.bindings, chatID)
			slog.Info("relay: binding removed (no bots left)", "chat_id", chatID)
		}
		rm.saveLocked()
		return true
	}
	return false
}

// GetBinding returns the binding for a chat, or nil if none.
func (rm *RelayManager) GetBinding(chatID string) *RelayBinding {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.bindings[chatID]
}

// Unbind removes the relay binding for a chat.
func (rm *RelayManager) Unbind(chatID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.bindings, chatID)
	slog.Info("relay: binding removed", "chat_id", chatID)
	rm.saveLocked()
}

// HasEngine checks if a project engine is registered.
func (rm *RelayManager) HasEngine(name string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	_, ok := rm.engines[name]
	return ok
}

// ListEngineNames returns all registered engine names.
func (rm *RelayManager) ListEngineNames() []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	names := make([]string, 0, len(rm.engines))
	for n := range rm.engines {
		names = append(names, n)
	}
	return names
}

// ListBoundBots returns the other bots bound in the same chat as the given project.
func (rm *RelayManager) ListBoundBots(chatID, selfProject string) map[string]string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	b := rm.bindings[chatID]
	if b == nil {
		return nil
	}
	others := make(map[string]string)
	for proj, name := range b.Bots {
		if proj != selfProject {
			others[proj] = name
		}
	}
	return others
}

// RelayRequest is the payload for a relay send.
type RelayRequest struct {
	From       string `json:"from"`        // source project name
	To         string `json:"to"`          // target project name
	SessionKey string `json:"session_key"` // source session key (contains platform + chatID)
	Message    string `json:"message"`
	// LOCAL PATCH (relay-new-session): start a fresh agent session for this relay turn.
	NewSession bool `json:"new_session,omitempty"`
	// LOCAL PATCH (relay-async): return immediately; deliver the reply to the
	// caller's session as a new message once the target finishes.
	Async bool `json:"async,omitempty"`
}

// RelayResponse is the result of a relay send.
type RelayResponse struct {
	Response string `json:"response"`
}

// Send delivers a message from one bot to another and returns the response.
func (rm *RelayManager) Send(ctx context.Context, req RelayRequest) (*RelayResponse, error) {
	platform, chatID, err := parseSessionKeyParts(req.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("relay: invalid session key: %w", err)
	}

	rm.mu.RLock()
	binding := rm.bindings[chatID]
	targetEngine := rm.engines[req.To]
	sourceEngine := rm.engines[req.From]
	visibility := rm.visibility
	rm.mu.RUnlock()

	if binding == nil {
		return nil, fmt.Errorf("relay: no binding for this chat. Use /bind <project> first")
	}
	if _, ok := binding.Bots[req.To]; !ok {
		var bound []string
		for proj := range binding.Bots {
			if proj != req.From {
				bound = append(bound, proj)
			}
		}
		return nil, fmt.Errorf("relay: project %q is not bound in this chat. Available targets: %s (use the exact name)", req.To, strings.Join(bound, ", "))
	}
	if targetEngine == nil {
		return nil, fmt.Errorf("relay: target engine %q not found (is the project running?)", req.To)
	}

	fromName := req.From
	if binding.Bots[req.From] != "" {
		fromName = binding.Bots[req.From]
	}
	toName := req.To
	if binding.Bots[req.To] != "" {
		toName = binding.Bots[req.To]
	}

	// Post the forwarded message to the group chat for visibility.  The
	// default target is "<platform>:<chatID>:relay"; platforms that
	// understand thread / topic semantics can override this by
	// implementing core.RelayGroupVisibilityTarget on their Platform impl.
	groupSessionKey := rm.resolveGroupVisibilityKey(platform, chatID, req.SessionKey, sourceEngine)
	if sourceEngine != nil && visibility != RelayVisibilityNone {
		label := relayVisibilityRequestLabel(visibility, fromName, toName, req.Message)
		rm.sendToGroup(ctx, sourceEngine, platform, groupSessionKey, label)
	}

	if req.Async {
		if sourceEngine == nil {
			return nil, fmt.Errorf("relay: source engine %q not found, async reply has nowhere to go", req.From)
		}
		go rm.runAsync(req, targetEngine, sourceEngine, platform, groupSessionKey, toName, visibility)
		return &RelayResponse{Response: fmt.Sprintf("已异步转交给 %s。它完成后，回复会作为一条新消息发到你当前的会话；现在不用等，结束本轮即可。", toName)}, nil
	}

	// Execute relay: inject message into target engine and collect response
	relayCtx, cancel := rm.relayContext(ctx)
	defer cancel()

	response, err := targetEngine.HandleRelayOpts(relayCtx, req.From, req.SessionKey, req.Message, req.NewSession)
	if err != nil {
		return nil, fmt.Errorf("relay: %w", err)
	}

	// Post the response to the group chat for visibility.
	if targetEngine != nil && visibility != RelayVisibilityNone {
		label := relayVisibilityResponseLabel(visibility, toName, response)
		rm.sendToGroup(ctx, targetEngine, platform, groupSessionKey, label)
	}

	return &RelayResponse{Response: response}, nil
}

// sendToGroup sends a message to the group chat for visibility.
func (rm *RelayManager) sendToGroup(ctx context.Context, e *Engine, platform, sessionKey, content string) {
	for _, p := range e.platforms {
		if p.Name() != platform {
			continue
		}
		rc, ok := p.(ReplyContextReconstructor)
		if !ok {
			continue
		}
		rctx, err := rc.ReconstructReplyCtx(sessionKey)
		if err != nil {
			slog.Debug("relay: failed to reconstruct reply ctx", "error", err)
			continue
		}
		if err := p.Send(ctx, rctx, content); err != nil {
			slog.Debug("relay: failed to send group message", "error", err)
		}
		return
	}
}

func truncateRelay(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

func normalizeRelayVisibility(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case RelayVisibilityNone:
		return RelayVisibilityNone
	case RelayVisibilitySummary:
		return RelayVisibilitySummary
	case "", RelayVisibilityFull:
		return RelayVisibilityFull
	default:
		slog.Warn("relay: unknown visibility mode, falling back to full", "mode", mode,
			"valid_values", []string{RelayVisibilityNone, RelayVisibilitySummary, RelayVisibilityFull})
		return RelayVisibilityFull
	}
}

func relayVisibilityRequestLabel(mode, fromName, toName, message string) string {
	if normalizeRelayVisibility(mode) == RelayVisibilitySummary {
		return fmt.Sprintf("[%s → %s] relay request sent", fromName, toName)
	}
	return fmt.Sprintf("[%s → %s] %s", fromName, toName, message)
}

func relayVisibilityResponseLabel(mode, toName, response string) string {
	if normalizeRelayVisibility(mode) == RelayVisibilitySummary {
		return fmt.Sprintf("[%s] relay response ready (%d chars)", toName, len([]rune(response)))
	}
	return fmt.Sprintf("[%s] %s", toName, response)
}

func (rm *RelayManager) relayContext(ctx context.Context) (context.Context, context.CancelFunc) {
	rm.mu.RLock()
	timeout := rm.timeout
	rm.mu.RUnlock()
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func parseSessionKeyParts(sessionKey string) (platform, chatID string, err error) {
	// Format: "platform:chatID:userID"
	// Relay session format: "relay:sourceProject:chatID"
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid session key format: %q", sessionKey)
	}
	if parts[0] == "relay" && len(parts) == 3 {
		// For relay sessions, chatID is the third part: "relay:sourceProject:chatID"
		return parts[0], parts[2], nil
	}
	return parts[0], parts[1], nil
}

// resolveGroupVisibilityKey computes the session key used for relay
// visibility echoes.  It defaults to "<platform>:<chatID>:relay" and
// gives the caller's platform a chance to override via the optional
// core.RelayGroupVisibilityTarget interface.
//
// Looking the platform up via sourceEngine.platforms (not targetEngine)
// matches the existing sendToGroup() resolution path — the visibility
// echo is dispatched as the source bot, so the source engine's
// platform impl is authoritative for the key format.
func (rm *RelayManager) resolveGroupVisibilityKey(platform, chatID, callerSessionKey string, sourceEngine *Engine) string {
	defaultKey := platform + ":" + chatID + ":relay"
	if sourceEngine == nil {
		return defaultKey
	}
	for _, p := range sourceEngine.platforms {
		if p.Name() != platform {
			continue
		}
		d, ok := p.(RelayGroupVisibilityTarget)
		if !ok {
			return defaultKey
		}
		if k, ok := d.RelayGroupVisibilityKey(callerSessionKey); ok && k != "" {
			return k
		}
		return defaultKey
	}
	return defaultKey
}

// ── Persistence ─────────────────────────────────────────────

// saveLocked persists bindings to disk. Caller must hold rm.mu (read or write).
func (rm *RelayManager) saveLocked() {
	if rm.storePath == "" {
		return
	}
	data, err := json.MarshalIndent(rm.bindings, "", "  ")
	if err != nil {
		slog.Error("relay: failed to marshal bindings", "error", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(rm.storePath), 0o755); err != nil {
		slog.Error("relay: failed to create dir", "error", err)
		return
	}
	if err := AtomicWriteFile(rm.storePath, data, 0o644); err != nil {
		slog.Error("relay: failed to write bindings", "path", rm.storePath, "error", err)
	}
}

func (rm *RelayManager) load() {
	if rm.storePath == "" {
		return
	}
	data, err := os.ReadFile(rm.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("relay: failed to read bindings", "path", rm.storePath, "error", err)
		}
		return
	}
	var bindings map[string]*RelayBinding
	if err := json.Unmarshal(data, &bindings); err != nil {
		slog.Error("relay: failed to unmarshal bindings", "path", rm.storePath, "error", err)
		return
	}
	if bindings != nil {
		rm.bindings = bindings
		slog.Info("relay: loaded bindings", "count", len(bindings))
	}
}

// LOCAL PATCH (relay-async): run the relay in the background and hand the
// reply back to the caller as a timer-style prompt in its own session.
func (rm *RelayManager) runAsync(req RelayRequest, targetEngine, sourceEngine *Engine, platform, groupSessionKey, toName, visibility string) {
	relayCtx, cancel := rm.relayContext(context.Background())
	response, err := targetEngine.HandleRelayOpts(relayCtx, req.From, req.SessionKey, req.Message, req.NewSession)
	cancel()

	var prompt string
	if err != nil {
		slog.Warn("relay async: target failed", "to", req.To, "error", err)
		prompt = fmt.Sprintf("[relay 回信] 你之前异步转交给 %s 的任务失败了：%v\n\n原请求：\n%s", toName, err, truncateRelay(req.Message, 500))
	} else {
		if visibility != RelayVisibilityNone {
			rm.sendToGroup(context.Background(), targetEngine, platform, groupSessionKey, relayVisibilityResponseLabel(visibility, toName, response))
		}
		prompt = fmt.Sprintf("[relay 回信] %s 已完成你之前异步转交的任务。请根据它的回复继续，并把结果告诉机主。\n\n原请求：\n%s\n\n%s 的回复：\n%s", toName, truncateRelay(req.Message, 500), toName, response)
	}

	silent := true
	deadline := time.Now().Add(time.Hour)
	for {
		job := &TimerJob{
			ID:          fmt.Sprintf("relay-%d", time.Now().UnixNano()),
			Project:     req.From,
			SessionKey:  req.SessionKey,
			Prompt:      prompt,
			Description: "relay 回信：" + toName,
			Silent:      &silent,
			SessionMode: "reuse", // back into the caller's own conversation, not a side session
		}
		err := sourceEngine.ExecuteTimerJob(job)
		if err == nil {
			return
		}
		if !strings.Contains(err.Error(), "is busy") || time.Now().After(deadline) {
			slog.Error("relay async: deliver reply failed", "from", req.From, "to", req.To, "error", err)
			return
		}
		time.Sleep(5 * time.Second)
	}
}
