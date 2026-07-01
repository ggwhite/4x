// Package schemasync 驗證 Go struct（單一事實來源）與 schemas/*.json、
// dashboard/web/core.js 之間的欄位／列舉集合是否一致。
package schemasync

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// StructJSONFields 用 reflect 擷取 struct 的 JSON 欄位名：讀取 exported 欄位的
// json tag 首段（去除 ,omitempty 等選項），json:"-" 視為不輸出並跳過；無 json tag
// 的 exported 欄位以欄位名為準。回傳排序、去重的欄位名 slice。
func StructJSONFields(v any) []string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	seen := make(map[string]bool)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := f.Name
		if tag != "" {
			if first, _, _ := strings.Cut(tag, ","); first != "" {
				name = first
			}
		}
		seen[name] = true
	}

	fields := make([]string, 0, len(seen))
	for name := range seen {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}

// SchemaProperties 解析 JSON Schema 的 properties key 集合，以及各具 enum 欄位
// （含陣列 items.properties.* 巢狀，如 subtasks.items.status）的 enum 值。
// 巢狀 enum 的 key 格式為 "<欄位名>.items.<子欄位名>"。
func SchemaProperties(schemaBytes []byte) (fields []string, enums map[string][]string, err error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(schemaBytes, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse schema: %w", err)
	}

	enums = make(map[string][]string)
	propsRaw, ok := doc["properties"]
	if !ok {
		return nil, enums, nil
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		return nil, nil, fmt.Errorf("parse properties: %w", err)
	}

	fields = make([]string, 0, len(props))
	for name, propBytes := range props {
		fields = append(fields, name)

		var prop map[string]json.RawMessage
		if err := json.Unmarshal(propBytes, &prop); err != nil {
			return nil, nil, fmt.Errorf("parse property %q: %w", name, err)
		}

		if vals, err := extractEnum(prop); err != nil {
			return nil, nil, fmt.Errorf("parse enum for %q: %w", name, err)
		} else if vals != nil {
			enums[name] = vals
		}

		if itemsRaw, ok := prop["items"]; ok {
			var items map[string]json.RawMessage
			if err := json.Unmarshal(itemsRaw, &items); err != nil {
				continue
			}
			itemPropsRaw, ok := items["properties"]
			if !ok {
				continue
			}
			var itemProps map[string]json.RawMessage
			if err := json.Unmarshal(itemPropsRaw, &itemProps); err != nil {
				continue
			}
			for itemName, itemPropBytes := range itemProps {
				var itemProp map[string]json.RawMessage
				if err := json.Unmarshal(itemPropBytes, &itemProp); err != nil {
					continue
				}
				if vals, err := extractEnum(itemProp); err != nil {
					return nil, nil, fmt.Errorf("parse enum for %q.items.%q: %w", name, itemName, err)
				} else if vals != nil {
					enums[name+".items."+itemName] = vals
				}
			}
		}
	}

	sort.Strings(fields)
	return fields, enums, nil
}

func extractEnum(prop map[string]json.RawMessage) ([]string, error) {
	enumRaw, ok := prop["enum"]
	if !ok {
		return nil, nil
	}
	var vals []string
	if err := json.Unmarshal(enumRaw, &vals); err != nil {
		return nil, err
	}
	return vals, nil
}
