package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
	"github.com/spf13/cobra"
)

// newCostCmd 建立 `4x cost` 指令，彙整 run 產出的成本資料。
//
// 資料來源以 logs/*.stream.jsonl 為主（每個 role invocation 一檔，含 total_cost_usd
// 與完整 usage），events.jsonl 為輔（某 feature 完全沒有 stream 檔時才退回）。
// 只讀取既有檔案，不改任何 run 資料。
func newCostCmd() *cobra.Command {
	var featureFlag string
	var byRound bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Aggregate per-role / per-round run cost from stream logs",
		Args:  cobra.NoArgs,
		RunE: withJsonError(&jsonOutput, func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureFilter := ""
			if featureFlag != "" {
				featureFilter = featureFlag
				// 允許使用者傳簡短 ID；解析失敗（例如 run 目錄無對應 feature YAML）
				// 時退回原字串，仍能對 run 目錄名直接比對。
				if resolved, rerr := ws.ResolveFeatureID(featureFlag); rerr == nil {
					featureFilter = resolved
				}
			}

			data, err := collectCost(ws, featureFilter)
			if err != nil {
				return err
			}

			switch {
			case byRound:
				return renderByRound(data, featureFilter, jsonOutput)
			case featureFilter != "":
				return renderByFeature(data, featureFilter, jsonOutput)
			default:
				return renderByRole(data, jsonOutput)
			}
		}),
	}

	cmd.Flags().StringVar(&featureFlag, "feature", "", "filter to a single feature; show per-round per-role detail")
	cmd.Flags().BoolVar(&byRound, "by-round", false, "group by round and show retry (round>=2) share")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

// costEntry 是一次 role 執行的成本歸因（單筆 stream.jsonl 或 events run-end）。
type costEntry struct {
	Feature string
	Round   int
	Role    string
	CostUSD float64
}

// costData 是掃描結果：成本明細加上被跳過的 stream 檔數（缺 total_cost_usd 的舊 run）。
type costData struct {
	Entries []costEntry
	Skipped int
}

var (
	streamFileRe     = regexp.MustCompile(`^round-(\d+)-(.+)\.stream\.jsonl$`)
	roleIterSuffixRe = regexp.MustCompile(`-\d+$`)
)

// parseStreamFileName 從 stream log 檔名解析 round 與 role。
// 檔名格式為 round-<N>-<role>.stream.jsonl，其中 role 可能帶末尾的 iteration/index
// 後綴（如 deep-reviewer-3、deep-fix-1），一律剝除以歸併到同一 role。
func parseStreamFileName(name string) (round int, role string, ok bool) {
	m := streamFileRe.FindStringSubmatch(name)
	if m == nil {
		return 0, "", false
	}
	r, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", false
	}
	role = roleIterSuffixRe.ReplaceAllString(m[2], "")
	if role == "" {
		return 0, "", false
	}
	return r, role, true
}

// streamCost 讀取單一 stream.jsonl，取最後一筆帶 total_cost_usd 的 result 事件成本。
// found 為 false 代表該檔沒有 result 事件或缺 total_cost_usd 欄位（舊 run），呼叫端據此計入 skipped。
func streamCost(path string) (cost float64, found bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
	marker := []byte(`"type":"result"`)
	for sc.Scan() {
		line := sc.Bytes()
		// 快速預篩，避免對每行做 JSON 解析。
		if !bytes.Contains(line, marker) {
			continue
		}
		var ev struct {
			Type         string   `json:"type"`
			TotalCostUSD *float64 `json:"total_cost_usd"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Type == "result" && ev.TotalCostUSD != nil {
			cost = *ev.TotalCostUSD
			found = true
		}
	}
	return cost, found
}

// collectCost 掃描 .4x/run/ 下所有 feature 的成本；featureFilter 非空時只收該 feature。
func collectCost(ws *protocol.Workspace, featureFilter string) (costData, error) {
	var data costData
	runRoot := filepath.Join(ws.DotDir(), protocol.RunDir)
	dirs, err := os.ReadDir(runRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return data, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		featureID := d.Name()
		if featureFilter != "" && featureID != featureFilter {
			continue
		}
		entries, skipped := collectFeatureCost(ws, featureID)
		data.Entries = append(data.Entries, entries...)
		data.Skipped += skipped
	}
	return data, nil
}

// collectFeatureCost 收單一 feature 的成本：以 stream.jsonl 為主，全無 stream 資料時退回 events.jsonl。
func collectFeatureCost(ws *protocol.Workspace, featureID string) ([]costEntry, int) {
	var entries []costEntry
	skipped := 0

	logsDir := runner.LogDir(ws, featureID)
	files, err := os.ReadDir(logsDir)
	if err == nil {
		for _, fe := range files {
			if fe.IsDir() || !strings.HasSuffix(fe.Name(), ".stream.jsonl") {
				continue
			}
			round, role, ok := parseStreamFileName(fe.Name())
			if !ok {
				continue
			}
			cost, found := streamCost(filepath.Join(logsDir, fe.Name()))
			if !found {
				skipped++
				continue
			}
			entries = append(entries, costEntry{Feature: featureID, Round: round, Role: role, CostUSD: cost})
		}
	}

	if len(entries) > 0 {
		return entries, skipped
	}
	// stream.jsonl 完全無資料時退回 events.jsonl（輔），涵蓋 stream log 出現前的舊 run。
	return eventsCost(ws, featureID), skipped
}

// eventsCost 從 events.jsonl 的 run-end 事件擷取 per-role/per-round 成本。
func eventsCost(ws *protocol.Workspace, featureID string) []costEntry {
	path := filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []costEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var e struct {
			Type    string  `json:"type"`
			Role    string  `json:"role"`
			Round   int     `json:"round"`
			CostUSD float64 `json:"cost_usd"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Type == "run-end" && e.CostUSD > 0 && e.Role != "" {
			entries = append(entries, costEntry{Feature: featureID, Round: e.Round, Role: e.Role, CostUSD: e.CostUSD})
		}
	}
	return entries
}

// --- 彙總與輸出 ---

func grandTotal(entries []costEntry) (total float64, calls int) {
	for _, e := range entries {
		total += e.CostUSD
		calls++
	}
	return total, calls
}

func pct(v, total float64) float64 {
	if total == 0 {
		return 0
	}
	return v / total * 100
}

func avg(total float64, calls int) float64 {
	if calls == 0 {
		return 0
	}
	return total / float64(calls)
}

// costRowJSON 是 --json 輸出的單列；Round 用指標，by-role 視圖時省略。
type costRowJSON struct {
	Role     string  `json:"role,omitempty"`
	Round    *int    `json:"round,omitempty"`
	Calls    int     `json:"calls"`
	TotalUSD float64 `json:"totalUsd"`
	AvgUSD   float64 `json:"avgUsd"`
	Pct      float64 `json:"pct"`
	Retry    bool    `json:"retry,omitempty"`
}

type costJSON struct {
	View     string        `json:"view"`
	Feature  string        `json:"feature,omitempty"`
	Rows     []costRowJSON `json:"rows"`
	TotalUSD float64       `json:"totalUsd"`
	Calls    int           `json:"calls"`
	Skipped  int           `json:"skipped"`
	RetryUSD float64       `json:"retryUsd,omitempty"`
	RetryPct float64       `json:"retryPct,omitempty"`
}

// renderByRole 輸出跨所有 feature 的 per-role 成本表（預設視圖）。
func renderByRole(data costData, jsonOutput bool) error {
	type agg struct {
		role  string
		calls int
		total float64
	}
	byRole := map[string]*agg{}
	for _, e := range data.Entries {
		a := byRole[e.Role]
		if a == nil {
			a = &agg{role: e.Role}
			byRole[e.Role] = a
		}
		a.calls++
		a.total += e.CostUSD
	}
	rows := make([]*agg, 0, len(byRole))
	for _, a := range byRole {
		rows = append(rows, a)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].total != rows[j].total {
			return rows[i].total > rows[j].total
		}
		return rows[i].role < rows[j].role
	})

	total, calls := grandTotal(data.Entries)

	if jsonOutput {
		out := costJSON{View: "by-role", TotalUSD: total, Calls: calls, Skipped: data.Skipped}
		for _, a := range rows {
			out.Rows = append(out.Rows, costRowJSON{
				Role:     a.role,
				Calls:    a.calls,
				TotalUSD: a.total,
				AvgUSD:   avg(a.total, a.calls),
				Pct:      pct(a.total, total),
			})
		}
		return printJSON(out)
	}

	if len(rows) == 0 {
		fmt.Println("No cost data found.")
		printSkipped(data.Skipped)
		return nil
	}

	fmt.Printf("Cost by role (all features) — total $%.4f across %d calls\n\n", total, calls)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "ROLE\tCALLS\tTOTAL($)\tAVG($)\tPCT(%%)\n")
	fmt.Fprintf(w, "────\t─────\t────────\t──────\t──────\n")
	for _, a := range rows {
		fmt.Fprintf(w, "%s\t%d\t%.4f\t%.4f\t%.1f\n", a.role, a.calls, a.total, avg(a.total, a.calls), pct(a.total, total))
	}
	fmt.Fprintf(w, "%s\t%d\t%.4f\t%.4f\t%.1f\n", "TOTAL", calls, total, avg(total, calls), 100.0)
	w.Flush()
	printSkipped(data.Skipped)
	return nil
}

// renderByFeature 輸出單一 feature 的 per-round per-role 明細。
func renderByFeature(data costData, featureID string, jsonOutput bool) error {
	type key struct {
		round int
		role  string
	}
	type agg struct {
		key   key
		calls int
		total float64
	}
	byKey := map[key]*agg{}
	for _, e := range data.Entries {
		k := key{e.Round, e.Role}
		a := byKey[k]
		if a == nil {
			a = &agg{key: k}
			byKey[k] = a
		}
		a.calls++
		a.total += e.CostUSD
	}
	rows := make([]*agg, 0, len(byKey))
	for _, a := range byKey {
		rows = append(rows, a)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].key.round != rows[j].key.round {
			return rows[i].key.round < rows[j].key.round
		}
		if rows[i].total != rows[j].total {
			return rows[i].total > rows[j].total
		}
		return rows[i].key.role < rows[j].key.role
	})

	total, calls := grandTotal(data.Entries)

	if jsonOutput {
		out := costJSON{View: "by-feature", Feature: featureID, TotalUSD: total, Calls: calls, Skipped: data.Skipped}
		for _, a := range rows {
			round := a.key.round
			out.Rows = append(out.Rows, costRowJSON{
				Round:    &round,
				Role:     a.key.role,
				Calls:    a.calls,
				TotalUSD: a.total,
				AvgUSD:   avg(a.total, a.calls),
				Pct:      pct(a.total, total),
			})
		}
		return printJSON(out)
	}

	if len(rows) == 0 {
		fmt.Printf("No cost data found for %s.\n", featureID)
		printSkipped(data.Skipped)
		return nil
	}

	fmt.Printf("Cost for %s — total $%.4f across %d calls\n\n", featureID, total, calls)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "ROUND\tROLE\tCALLS\tTOTAL($)\tPCT(%%)\n")
	fmt.Fprintf(w, "─────\t────\t─────\t────────\t──────\n")
	for _, a := range rows {
		fmt.Fprintf(w, "%d\t%s\t%d\t%.4f\t%.1f\n", a.key.round, a.key.role, a.calls, a.total, pct(a.total, total))
	}
	fmt.Fprintf(w, "%s\t%s\t%d\t%.4f\t%.1f\n", "TOTAL", "", calls, total, 100.0)
	w.Flush()
	printSkipped(data.Skipped)
	return nil
}

// renderByRound 依 round 彙總成本，並標示 retry（round>=2）佔比。
func renderByRound(data costData, featureID string, jsonOutput bool) error {
	type agg struct {
		round int
		calls int
		total float64
	}
	byRound := map[int]*agg{}
	for _, e := range data.Entries {
		a := byRound[e.Round]
		if a == nil {
			a = &agg{round: e.Round}
			byRound[e.Round] = a
		}
		a.calls++
		a.total += e.CostUSD
	}
	rows := make([]*agg, 0, len(byRound))
	for _, a := range byRound {
		rows = append(rows, a)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].round < rows[j].round })

	total, calls := grandTotal(data.Entries)
	var retryTotal float64
	for _, a := range rows {
		if a.round >= 2 {
			retryTotal += a.total
		}
	}

	if jsonOutput {
		out := costJSON{
			View:     "by-round",
			Feature:  featureID,
			TotalUSD: total,
			Calls:    calls,
			Skipped:  data.Skipped,
			RetryUSD: retryTotal,
			RetryPct: pct(retryTotal, total),
		}
		for _, a := range rows {
			round := a.round
			out.Rows = append(out.Rows, costRowJSON{
				Round:    &round,
				Calls:    a.calls,
				TotalUSD: a.total,
				AvgUSD:   avg(a.total, a.calls),
				Pct:      pct(a.total, total),
				Retry:    a.round >= 2,
			})
		}
		return printJSON(out)
	}

	if len(rows) == 0 {
		fmt.Println("No cost data found.")
		printSkipped(data.Skipped)
		return nil
	}

	scope := "all features"
	if featureID != "" {
		scope = featureID
	}
	fmt.Printf("Cost by round (%s) — total $%.4f, retry(round>=2) $%.4f (%.1f%%)\n\n",
		scope, total, retryTotal, pct(retryTotal, total))
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "ROUND\tTYPE\tCALLS\tTOTAL($)\tPCT(%%)\n")
	fmt.Fprintf(w, "─────\t────\t─────\t────────\t──────\n")
	for _, a := range rows {
		kind := "initial"
		if a.round >= 2 {
			kind = "retry"
		}
		fmt.Fprintf(w, "%d\t%s\t%d\t%.4f\t%.1f\n", a.round, kind, a.calls, a.total, pct(a.total, total))
	}
	fmt.Fprintf(w, "%s\t%s\t%d\t%.4f\t%.1f\n", "TOTAL", "", calls, total, 100.0)
	w.Flush()
	printSkipped(data.Skipped)
	return nil
}

func printSkipped(skipped int) {
	if skipped > 0 {
		fmt.Printf("\nSkipped %d stream log(s) with no cost data.\n", skipped)
	}
}
