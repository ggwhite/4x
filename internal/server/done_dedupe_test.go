package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestPostDone_SyncFailureStillReturnsDone 驗證 AC-4：merge 成功、state 已寫 done，但
// SyncFeatureStatus 失敗的 edge，POST /api/done 不再回 5xx，而是回 {"status":"done"}。
//
// 透過把 .4x/features 目錄設為唯讀，使 FinalizeDone 內 SyncFeatureStatus 的 SaveFeature 寫入失敗；
// state.json 在 run/ 目錄不受影響，故 transition 仍成功落地。root 會繞過唯讀權限，故 root 下跳過。
func TestPostDone_SyncFailureStillReturnsDone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 會繞過唯讀目錄權限，無法在 root 下模擬 sync 失敗")
	}

	ws := setupServerWorkspace(t)
	setupMergeableWorktree(t, ws, "test-feat", func(wtRoot string) {
		if err := os.WriteFile(filepath.Join(wtRoot, "unrelated.txt"), []byte("change\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	featuresDir := filepath.Join(ws.DotDir(), protocol.FeaturesDir)
	if err := os.Chmod(featuresDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(featuresDir, 0o755) })

	rec := serveRequest(t, NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil)), http.MethodPost, "/api/done", `{"id":"test-feat"}`)
	if rec.Code/100 == 5 {
		t.Fatalf("status = %d, want non-5xx (sync 失敗應為 non-fatal): %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"done"`) {
		t.Errorf("body = %s, want status done", rec.Body.String())
	}

	s, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != protocol.PhaseDone {
		t.Errorf("phase = %q, want done (transition 應已落地)", s.Phase)
	}
}
