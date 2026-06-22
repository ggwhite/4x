package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EvolveState 跨多次 `4x evolve` 呼叫持久化 anti-spin 防空轉計數，存於 .4x/evolve-state.json。
// 每次 evolve 只跑一輪（裁決 4），重複跑由外部驅動；連續未接受的輪數靠本檔跨呼叫累計。
type EvolveState struct {
	Version             int       `json:"version"`
	Round               int       `json:"round"`
	ConsecutiveNoAccept int       `json:"consecutiveNoAccept"`
	LastRunAt           time.Time `json:"lastRunAt"`
}

// LoadEvolveState 讀取 evolve-state.json；檔案不存在時回零值（不視為錯誤，對齊
// protocol.LoadCandidates 慣例），JSON 解析失敗才回 error。
func LoadEvolveState(path string) (EvolveState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return EvolveState{}, nil
		}
		return EvolveState{}, fmt.Errorf("read evolve-state: %w", err)
	}
	var s EvolveState
	if err := json.Unmarshal(data, &s); err != nil {
		return EvolveState{}, fmt.Errorf("parse evolve-state: %w", err)
	}
	return s, nil
}

// Save 以同目錄 temp file + rename 原子寫入 path，避免 dashboard 讀到半寫的 JSON。
func (s EvolveState) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal evolve-state: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".evolve-state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// ShouldHalt 回報是否已達 anti-spin 早退門檻。
// maxIdle <= 0 表示停用 halt（永遠跑）；正數時 ConsecutiveNoAccept >= maxIdle 即應早退。
func (s EvolveState) ShouldHalt(maxIdle int) bool {
	if maxIdle <= 0 {
		return false
	}
	return s.ConsecutiveNoAccept >= maxIdle
}
