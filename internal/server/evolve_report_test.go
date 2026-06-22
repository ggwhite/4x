package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

type evolveReportResp struct {
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// TestGetEvolveReport_WithData 驗證 AC-12：route 回傳 .4x/evolve-report.md 內容。
func TestGetEvolveReport_WithData(t *testing.T) {
	ws := setupServerWorkspace(t)
	body := "# Evolve Report — Round 1\n\n## Accepted\n- Cand A\n"
	if err := os.WriteFile(filepath.Join(ws.DotDir(), protocol.EvolveReportFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil)), http.MethodGet, "/api/evolve-report", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp evolveReportResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Exists || resp.Content != body {
		t.Errorf("resp = %+v, want exists=true content=%q", resp, body)
	}
}

// TestGetEvolveReport_Missing 驗證檔不存在時回 not-found 慣例（exists:false、空 content）。
func TestGetEvolveReport_Missing(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil)), http.MethodGet, "/api/evolve-report", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp evolveReportResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Exists || resp.Content != "" {
		t.Errorf("resp = %+v, want exists=false empty content", resp)
	}
}
