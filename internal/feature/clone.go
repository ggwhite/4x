package feature

// Clone 回傳 f 的深拷貝，所有 reference 欄位（Priority、Repos、Subtasks、Rules、
// Depends、Hooks、Warnings）皆配置新的底層儲存，與原值完全不共用。
// 供 cache 層在回傳前隔離使用：呼叫端就地修改回傳值不會污染 cache 內存版本。
func (f Feature) Clone() Feature {
	c := f // 先複製純量欄位

	if f.Priority != nil {
		p := *f.Priority
		c.Priority = &p
	}

	c.Repos = cloneStrings(f.Repos)
	c.Rules = cloneStrings(f.Rules)
	c.Depends = cloneStrings(f.Depends)
	c.Warnings = cloneStrings(f.Warnings)

	if f.Subtasks != nil {
		c.Subtasks = make([]Subtask, len(f.Subtasks))
		for i, st := range f.Subtasks {
			st.Depends = cloneStrings(st.Depends)
			c.Subtasks[i] = st
		}
	}

	if f.Hooks != nil {
		c.Hooks = make(map[string][]HookEntry, len(f.Hooks))
		for k, entries := range f.Hooks {
			dup := make([]HookEntry, len(entries))
			copy(dup, entries)
			c.Hooks[k] = dup
		}
	}

	return c
}

// cloneStrings 複製一個 string slice；nil 維持 nil，避免改變欄位的「未設定」語義。
func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	dup := make([]string, len(s))
	copy(dup, s)
	return dup
}
