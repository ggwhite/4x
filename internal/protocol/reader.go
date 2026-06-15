package protocol

// WorkspaceReader 定義 Workspace 的唯讀操作集合，用於抽象出可被 cache 層包裝的讀取介面。
// long-running server 透過此介面操作，便於以 CachedWorkspace 替換而不影響呼叫端；
// CLI 短命程序則直接用 *Workspace。寫入方法刻意不納入介面，避免 cache 層被誤用於寫入。
type WorkspaceReader interface {
	// ListFeatures 列出所有 feature。
	ListFeatures() ([]Feature, error)
	// LoadFeature 讀取單一 feature YAML。
	LoadFeature(id string) (Feature, error)
	// ReadState 讀取 feature 的 state.json。
	ReadState(featureID string) (State, error)
	// ReadConfig 讀取 .4x/settings.json。
	ReadConfig() (Config, error)
}

// 編譯期確認 *Workspace 滿足 WorkspaceReader（現有方法已具備，無需額外實作）。
var _ WorkspaceReader = (*Workspace)(nil)
