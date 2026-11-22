package acp

import (
	"context"
	"testing"
)

func TestLocalPatchACPModelOptions(t *testing.T) {
	ag, err := New(map[string]any{"cmd": "sh", "model": "m-low"})
	if err != nil {
		t.Fatal(err)
	}
	a := ag.(*Agent)
	if got := a.AvailableModels(context.Background()); len(got) != 1 || got[0].Name != "m-low" {
		t.Fatalf("before handshake: %+v", got)
	}
	var b acpModelsBlock
	b.AvailableModels = append(b.AvailableModels, struct {
		ModelID string `json:"modelId"`
		Name    string `json:"name"`
	}{"m-high", "M High"})
	s := &acpSession{callbacks: a}
	s.absorbModels(&b)
	if got := a.AvailableModels(context.Background()); len(got) != 1 || got[0].Name != "m-high" || got[0].Desc != "M High" {
		t.Fatalf("after handshake: %+v", got)
	}
	a.SetModel(" m-high ")
	if a.GetModel() != "m-high" {
		t.Fatalf("GetModel = %q", a.GetModel())
	}
}
