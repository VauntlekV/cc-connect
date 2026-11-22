package acp

// LOCAL PATCH acp-model: runtime model switching for ACP agents that
// advertise `models` in session/new (e.g. `agy acp`), via session/set_model.

import (
	"context"
	"log/slog"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

type acpModelsBlock struct {
	CurrentModelID  string `json:"currentModelId"`
	AvailableModels []struct {
		ModelID string `json:"modelId"`
		Name    string `json:"name"`
	} `json:"availableModels"`
}

var _ core.ModelSwitcher = (*Agent)(nil)

func (a *Agent) SetModel(model string) {
	a.mu.Lock()
	a.model = strings.TrimSpace(model)
	a.mu.Unlock()
}

func (a *Agent) GetModel() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}

func (a *Agent) AvailableModels(_ context.Context) []core.ModelOption {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.modelsCache) == 0 && a.model != "" {
		return []core.ModelOption{{Name: a.model}}
	}
	return append([]core.ModelOption(nil), a.modelsCache...)
}

func (a *Agent) reportModels(block acpModelsBlock) {
	opts := make([]core.ModelOption, 0, len(block.AvailableModels))
	for _, m := range block.AvailableModels {
		opts = append(opts, core.ModelOption{Name: m.ModelID, Desc: m.Name})
	}
	a.mu.Lock()
	a.modelsCache = opts
	a.mu.Unlock()
}

func (s *acpSession) absorbModels(block *acpModelsBlock) {
	if block == nil || len(block.AvailableModels) == 0 {
		return
	}
	if r, ok := s.callbacks.(interface{ reportModels(acpModelsBlock) }); ok {
		r.reportModels(*block)
	}
}

func (s *acpSession) applyInitialModel(model string) {
	if model == "" {
		return
	}
	if _, err := s.tr.call(s.ctx, "session/set_model", map[string]any{
		"sessionId": s.currentACPSessionID(),
		"modelId":   model,
	}); err != nil {
		slog.Warn("acp: set_model failed", "model", model, "error", err)
	}
}
