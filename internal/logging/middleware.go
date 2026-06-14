package logging

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder 包裝 http.ResponseWriter 以擷取回應的 status code。
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 將 http.Flusher 介面委託給底層 ResponseWriter，讓 SSE handler 能正常運作。
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware 回傳 HTTP request logging 的中介層。
// 每個 request 記錄 method、path、status code 與處理時間（毫秒）。
// 2xx/3xx 用 slog.Info、4xx 用 slog.Warn、5xx 用 slog.Error。
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rec := &statusRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.statusCode,
			"duration_ms", duration.Milliseconds(),
		}

		switch {
		case rec.statusCode >= 500:
			slog.Error("http request", attrs...)
		case rec.statusCode >= 400:
			slog.Warn("http request", attrs...)
		default:
			slog.Info("http request", attrs...)
		}
	})
}
