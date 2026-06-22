package evolution

import (
	"encoding/json"
	"fmt"
	"os"
)

// gate role 對單一 candidate 的兩種判斷結果。
const (
	VerdictAccept = "accept"
	VerdictReject = "reject"
)

// Verdict 為 gate role 對單一 candidate 的價值判斷，對應 gate-verdicts.json 內每筆條目。
type Verdict struct {
	Title      string  `json:"title"`
	Verdict    string  `json:"verdict"`
	ValueScore float64 `json:"value_score"`
	WhyNotHack string  `json:"why_not_hack,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// verdictFile 為 gate-verdicts.json 的外層結構。
type verdictFile struct {
	Verdicts []Verdict `json:"verdicts"`
}

// ParseVerdicts 讀取並解析 gate role 產出的 gate-verdicts.json。
// 檔案讀取或 JSON 解析失敗時回 error。
func ParseVerdicts(path string) ([]Verdict, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read verdicts %s: %w", path, err)
	}
	var f verdictFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse verdicts %s: %w", path, err)
	}
	return f.Verdicts, nil
}
