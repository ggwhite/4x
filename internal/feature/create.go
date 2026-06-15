package feature

// CreateOpts 是建立 feature 的輸入參數。
// Repos 為 []string（與 Feature.Repos 一致），對應 CLI 的 --repo flag；
// Server 端目前不傳入 Repos。
type CreateOpts struct {
	Name        string
	Description string
	CustomID    string
	Subtasks    []Subtask
	Rules       []string
	Depends     []string
	Priority    *int
	Repos       []string
}

// Create 統一建立 feature 的邏輯，由 CLI（cmd/4x new）與 Server（POST /api/new）共用。
// 流程：取下一個流水號 → 依 CustomID 是否為空選擇 ID 產生方式 → 組 Feature
// （Description 為空時預設為 Name）→ 持久化 → 建立運行時目錄 → 回傳 Feature。
func Create(store Store, opts CreateOpts) (Feature, error) {
	next, err := NextNumber(store)
	if err != nil {
		return Feature{}, err
	}

	var id string
	if opts.CustomID != "" {
		id = GenerateFeatureIDFromSlug(next, opts.CustomID)
	} else {
		id = GenerateFeatureID(next, opts.Name)
	}

	description := opts.Description
	if description == "" {
		description = opts.Name
	}

	f := Feature{
		ID:          id,
		Name:        opts.Name,
		Description: description,
		Status:      StatusNotStarted,
		Repos:       opts.Repos,
		Subtasks:    opts.Subtasks,
		Rules:       opts.Rules,
		Depends:     opts.Depends,
		Priority:    opts.Priority,
	}

	if err := store.SaveFeature(f); err != nil {
		return Feature{}, err
	}
	if err := store.InitFeatureDir(f.ID); err != nil {
		return Feature{}, err
	}

	return f, nil
}
