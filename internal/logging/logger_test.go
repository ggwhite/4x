package logging

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"  info  ", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestInitFallback(t *testing.T) {
	// 使用不可能建立的路徑測試 fallback
	cfg := Config{
		Level:      "info",
		LogDir:     "/dev/null/nonexistent/path",
		RetainDays: 7,
	}

	err := Init(cfg)
	if err == nil {
		t.Fatal("Init with invalid dir should return error")
	}

	// 驗證 fallback 後仍可正常使用 slog（不 panic）
	slog.Info("test after fallback", "key", "value")
}

func TestCleanOldLogs(t *testing.T) {
	dir := t.TempDir()

	// 建立幾個假 log 檔：舊的應被刪、新的應保留
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	recentDate := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	todayDate := time.Now().Format("2006-01-02")

	oldFile := filepath.Join(dir, "4x-"+oldDate+".log")
	recentFile := filepath.Join(dir, "4x-"+recentDate+".log")
	todayFile := filepath.Join(dir, "4x-"+todayDate+".log")
	otherFile := filepath.Join(dir, "other.txt")

	for _, f := range []string{oldFile, recentFile, todayFile, otherFile} {
		if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cleanOldLogs(dir, 7)

	// 舊的（10 天前）應被刪
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old log file should be deleted: %s", oldFile)
	}

	// 近期的（2 天前）應保留
	if _, err := os.Stat(recentFile); err != nil {
		t.Errorf("recent log file should be kept: %s", recentFile)
	}

	// 今天的應保留
	if _, err := os.Stat(todayFile); err != nil {
		t.Errorf("today's log file should be kept: %s", todayFile)
	}

	// 非 log 格式的檔案不受影響
	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("non-log file should be kept: %s", otherFile)
	}
}

func TestMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Errorf("status code = %d, want %d", rec.Code, tt.statusCode)
			}
		})
	}
}

func TestStatusRecorderDefaultStatus(t *testing.T) {
	// 測試不呼叫 WriteHeader 時預設 status 為 200
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("default status code = %d, want %d", rec.Code, http.StatusOK)
	}
}
