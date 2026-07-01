package schemasync

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/schemas"
)

func mustReadSchema(t *testing.T, name string) []byte {
	t.Helper()
	b, err := schemas.FS.ReadFile(name)
	if err != nil {
		t.Fatalf("讀取 %s 失敗: %v", name, err)
	}
	return b
}

func toStrings[T ~string](vals []T) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = string(v)
	}
	return out
}

func assertSetEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	for _, v := range got {
		gotSet[v] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, v := range want {
		wantSet[v] = true
	}

	var missing, extra []string
	for _, v := range want {
		if !gotSet[v] {
			missing = append(missing, v)
		}
	}
	for _, v := range got {
		if !wantSet[v] {
			extra = append(extra, v)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("%s 集合不一致：missing=%v extra=%v", label, missing, extra)
	}
}

func assertSubset(t *testing.T, label string, subset, superset []string) {
	t.Helper()
	superSet := make(map[string]bool, len(superset))
	for _, v := range superset {
		superSet[v] = true
	}
	var missing []string
	for _, v := range subset {
		if !superSet[v] {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%s 缺少 canonical 值：%v", label, missing)
	}
}

func TestStateSchemaMatchesStruct(t *testing.T) {
	schemaBytes := mustReadSchema(t, "state.schema.json")
	fields, enums, err := SchemaProperties(schemaBytes)
	if err != nil {
		t.Fatalf("SchemaProperties: %v", err)
	}

	assertSetEqual(t, "state.schema.json properties", fields, StructJSONFields(protocol.State{}))
	assertSetEqual(t, "state.schema.json phase enum", enums["phase"], toStrings(protocol.AllPhases()))
	assertSetEqual(t, "state.schema.json role enum", enums["role"], toStrings(protocol.AllRoles()))
	assertSetEqual(t, "state.schema.json subPhase enum", enums["subPhase"], toStrings(protocol.AllSubPhases()))
}

func TestEventSchemaMatchesStruct(t *testing.T) {
	schemaBytes := mustReadSchema(t, "event.schema.json")
	fields, enums, err := SchemaProperties(schemaBytes)
	if err != nil {
		t.Fatalf("SchemaProperties: %v", err)
	}

	assertSetEqual(t, "event.schema.json properties", fields, StructJSONFields(protocol.Event{}))
	assertSetEqual(t, "event.schema.json type enum", enums["type"], protocol.AllEventTypes())
	assertSetEqual(t, "event.schema.json notify enum", enums["notify"], protocol.AllNotifyLevels())
}

func TestFeatureSchemaMatchesStruct(t *testing.T) {
	schemaBytes := mustReadSchema(t, "feature.schema.json")
	fields, enums, err := SchemaProperties(schemaBytes)
	if err != nil {
		t.Fatalf("SchemaProperties: %v", err)
	}

	assertSetEqual(t, "feature.schema.json properties", fields, StructJSONFields(feature.Feature{}))
	assertSetEqual(t, "feature.schema.json status enum", enums["status"], toStrings(feature.AllStatuses()))
	assertSetEqual(t, "feature.schema.json subtasks.items.status enum", enums["subtasks.items.status"], feature.AllSubtaskStatuses())

	subtaskFields := subtaskItemsProperties(t, schemaBytes)
	assertSetEqual(t, "feature.schema.json subtasks.items.properties", subtaskFields, StructJSONFields(feature.Subtask{}))
}

// subtaskItemsProperties 抽出 feature.schema.json 內 subtasks.items.properties 的欄位名集合。
func subtaskItemsProperties(t *testing.T, schemaBytes []byte) []string {
	t.Helper()
	var doc struct {
		Properties struct {
			Subtasks struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"subtasks"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaBytes, &doc); err != nil {
		t.Fatalf("解析 subtasks.items.properties 失敗: %v", err)
	}
	fields := make([]string, 0, len(doc.Properties.Subtasks.Items.Properties))
	for name := range doc.Properties.Subtasks.Items.Properties {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}

func TestDashboardMapsCoverCanonical(t *testing.T) {
	src, err := os.ReadFile("../../dashboard/web/core.js")
	if err != nil {
		t.Fatalf("讀取 dashboard/web/core.js 失敗: %v", err)
	}

	phaseIcon := extractObjectKeys(t, src, "PHASE_ICON")
	roles := extractObjectKeys(t, src, "ROLES")

	assertSubset(t, "core.js PHASE_ICON", toStrings(protocol.AllPhases()), phaseIcon)
	assertSubset(t, "core.js ROLES", toStrings(protocol.AllRoles()), roles)
}

// extractObjectKeys 解析 core.js 內 `const <constName> = {...};` 物件字面量的頂層 key
// 集合，正確處理巢狀物件（如 ROLES 內每個角色再包一層 {name,emoji,color,bg}）。
func extractObjectKeys(t *testing.T, src []byte, constName string) []string {
	t.Helper()
	openRe := regexp.MustCompile(`const\s+` + regexp.QuoteMeta(constName) + `\s*=\s*\{`)
	loc := openRe.FindIndex(src)
	if loc == nil {
		t.Fatalf("core.js 找不到 const %s = {...};", constName)
	}

	start := loc[1]
	depth := 1
	i := start
	for i < len(src) && depth > 0 {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	body := src[start : i-1]

	var keys []string
	entryStart := 0
	nestDepth := 0
	for j := 0; j <= len(body); j++ {
		if j == len(body) || (body[j] == ',' && nestDepth == 0) {
			if k := firstEntryKey(body[entryStart:j]); k != "" {
				keys = append(keys, k)
			}
			entryStart = j + 1
			continue
		}
		switch body[j] {
		case '{', '[', '(':
			nestDepth++
		case '}', ']', ')':
			nestDepth--
		}
	}
	if len(keys) == 0 {
		t.Fatalf("core.js const %s 未解析出任何 key", constName)
	}
	return keys
}

func firstEntryKey(entry []byte) string {
	s := strings.TrimSpace(string(entry))
	if s == "" {
		return ""
	}
	idx := strings.Index(s, ":")
	if idx < 0 {
		return ""
	}
	key := strings.TrimSpace(s[:idx])
	key = strings.Trim(key, "'\"")
	return key
}
