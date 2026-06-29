package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config 定義 logging 初始化所需的設定參數。
// Level 接受 debug/info/warn/error（預設 info），LogDir 為 log 檔目錄（預設 ~/.4x/logs/），
// RetainDays 為保留天數（預設 7）。
type Config struct {
	Level      string
	LogDir     string
	RetainDays int
}

var (
	logFile   *os.File
	logFileMu sync.Mutex
)

// ParseLevel 將字串轉為對應的 slog.Level。
// 支援 debug、info、warn、error（不分大小寫），無法辨識時回傳 slog.LevelInfo。
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func applyDefaults(cfg *Config) {
	if cfg.LogDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		cfg.LogDir = filepath.Join(home, ".4x", "logs")
	}
	if cfg.RetainDays <= 0 {
		cfg.RetainDays = 7
	}
	if cfg.Level == "" {
		cfg.Level = "info"
	}
}

// Init 初始化全域 slog.Default logger，同時輸出 JSON 格式到檔案與 text 格式到 stderr。
// 環境變數 FOURX_LOG_LEVEL 會覆蓋 cfg.Level。若無法建立 log 目錄或檔案，
// 不阻擋程式啟動，改為僅輸出到 stderr。
func Init(cfg Config) error {
	applyDefaults(&cfg)

	if envLevel := os.Getenv("FOURX_LOG_LEVEL"); envLevel != "" {
		cfg.Level = envLevel
	}

	fileLevel := ParseLevel(cfg.Level)

	// stderr handler: warn 以上，text 格式
	stderrLevel := new(slog.LevelVar)
	stderrLevel.Set(slog.LevelWarn)
	stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: stderrLevel,
	})

	// 嘗試建立 log 目錄與檔案
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		// 無法建立目錄，fallback 到 stderr only
		slog.SetDefault(slog.New(stderrHandler))
		return fmt.Errorf("無法建立 log 目錄 %s: %w（已 fallback 到 stderr）", cfg.LogDir, err)
	}

	// 清理過期 log 檔
	cleanOldLogs(cfg.LogDir, cfg.RetainDays)

	filename := fmt.Sprintf("4x-%s.log", time.Now().Format("2006-01-02"))
	logPath := filepath.Join(cfg.LogDir, filename)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// 無法開啟檔案，fallback 到 stderr only
		slog.SetDefault(slog.New(stderrHandler))
		return fmt.Errorf("無法開啟 log 檔 %s: %w（已 fallback 到 stderr）", logPath, err)
	}

	logFileMu.Lock()
	logFile = f
	logFileMu.Unlock()

	// file handler: 設定 level，JSON 格式
	fileHandler := slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: fileLevel,
	})

	// 合併兩個 handler
	multiHandler := &multiHandler{
		handlers: []slog.Handler{fileHandler, stderrHandler},
	}
	slog.SetDefault(slog.New(multiHandler))

	return nil
}

// Shutdown 關閉 log 檔案，釋放資源。應在程式結束前呼叫。
func Shutdown() {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}

// multiHandler 將 log record 同時送到多個 slog.Handler。
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}
