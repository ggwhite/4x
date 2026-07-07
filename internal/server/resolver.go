package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ggwhite/4x/internal/protocol"
)

// WorkspaceResolver 從 HTTP request 解析出該請求對應的 workspace 與 process manager。
// 讓 NewMux 的路由表只定義一次：單一專案模式用 singleResolver 回傳固定實例，多專案模式用
// multiResolver 依 URL prefix（經外層 mux 注入 context）或 compat 邏輯查找對應專案。
type WorkspaceResolver func(r *http.Request) (*protocol.CachedWorkspace, *ProcessManager, error)

// singleResolver 回傳固定 workspace 的 resolver，用於單一專案模式（4x live 的 legacy / 測試路徑）。
func singleResolver(ws *protocol.CachedWorkspace, pm *ProcessManager) WorkspaceResolver {
	return func(r *http.Request) (*protocol.CachedWorkspace, *ProcessManager, error) {
		return ws, pm, nil
	}
}

// staticResolver 回傳固定二元組（ws/pm）的 resolver，供 newMux thin wrapper 與測試使用。
func staticResolver(ws *protocol.CachedWorkspace, pm *ProcessManager) WorkspaceResolver {
	return func(r *http.Request) (*protocol.CachedWorkspace, *ProcessManager, error) {
		return ws, pm, nil
	}
}

// multiResolver 用於多專案模式：優先讀外層 prefix dispatch 注入 context 的已解析專案；
// 無 prefix（向後相容）時依 registry 內專案數量決定回傳唯一專案或錯誤。
func multiResolver(reg *ProjectRegistry) WorkspaceResolver {
	return func(r *http.Request) (*protocol.CachedWorkspace, *ProcessManager, error) {
		if entry := resolvedProjectFromContext(r); entry != nil {
			return entry.ws, entry.pm, nil
		}

		switch reg.Count() {
		case 0:
			return nil, nil, fmt.Errorf("no projects loaded — add a project first")
		case 1:
			items := reg.List()
			if len(items) != 1 {
				return nil, nil, fmt.Errorf("project unavailable")
			}
			entry := reg.getEntry(items[0].ID)
			if entry == nil {
				return nil, nil, fmt.Errorf("project unavailable")
			}
			return entry.ws, entry.pm, nil
		default:
			return nil, nil, fmt.Errorf("multiple projects loaded — use /api/project/{id}%s", r.URL.Path)
		}
	}
}

// resolvedProjectKeyType 是 context key 的私有型別，避免與其他套件的 key 碰撞。
type resolvedProjectKeyType struct{}

var resolvedProjectKey resolvedProjectKeyType

// withResolvedProject 把外層 prefix dispatch 查到的 registry entry 注入 request context，
// 供內層 multiResolver 取用，避免內層重複解析 URL 或重建 mux。
func withResolvedProject(ctx context.Context, entry *registryEntry) context.Context {
	return context.WithValue(ctx, resolvedProjectKey, entry)
}

// resolvedProjectFromContext 從 request context 取回已解析的 registry entry，無則回 nil。
func resolvedProjectFromContext(r *http.Request) *registryEntry {
	entry, _ := r.Context().Value(resolvedProjectKey).(*registryEntry)
	return entry
}
