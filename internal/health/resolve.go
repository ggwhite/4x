package health

import "github.com/ggwhite/4x/internal/protocol"

// ResolveHealthCheck 合併全域（settings.json）與 per-feature（test-strategy.yaml）設定。
// per-feature 非 nil → 整組覆蓋（不做欄位級 merge）；否則用全域；兩者皆 nil 回傳 nil。
func ResolveHealthCheck(global, feature *protocol.HealthCheck) *protocol.HealthCheck {
	if feature != nil {
		return feature
	}
	return global
}
