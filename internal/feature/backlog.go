package feature

import (
	"fmt"
	"sort"
	"strconv"
)

// CompareBacklogMirror 比對 feature YAML 清單與 legacy backlog mirror，並以 feature ID 穩定排序。
// backlogFile 是 legacy mirror 檔名（如 feature_list.json），僅用於組裝 drift 訊息，
// 由呼叫端傳入以避免 feature package 依賴 protocol 的常量。
func CompareBacklogMirror(features []Feature, backlogFile string, mirror BacklogMirror) []BacklogDrift {
	canonical := make(map[string]Feature, len(features))
	for _, f := range features {
		canonical[f.ID] = f
	}

	backlog := make(map[string]BacklogFeature, len(mirror.Features))
	for _, f := range mirror.Features {
		backlog[f.ID] = f
	}

	var drift []BacklogDrift
	for _, f := range features {
		entry, ok := backlog[f.ID]
		if !ok {
			drift = append(drift, BacklogDrift{
				Kind:      BacklogDriftMissing,
				FeatureID: f.ID,
				Message:   fmt.Sprintf("%s missing entry for feature %q", backlogFile, f.ID),
			})
			continue
		}
		drift = appendFieldDrift(drift, backlogFile, f.ID, "name", f.Name, entry.Name)
		drift = appendFieldDrift(drift, backlogFile, f.ID, "description", f.Description, entry.Description)
		drift = appendFieldDrift(drift, backlogFile, f.ID, "status", string(f.Status), entry.Status)
		drift = appendPriorityDrift(drift, backlogFile, f, entry)
	}

	for _, entry := range mirror.Features {
		if _, ok := canonical[entry.ID]; !ok {
			drift = append(drift, BacklogDrift{
				Kind:      BacklogDriftExtra,
				FeatureID: entry.ID,
				Message:   fmt.Sprintf("%s has extra entry %q", backlogFile, entry.ID),
			})
		}
	}

	sort.Slice(drift, func(i, j int) bool {
		if drift[i].FeatureID != drift[j].FeatureID {
			return drift[i].FeatureID < drift[j].FeatureID
		}
		if drift[i].Kind != drift[j].Kind {
			return drift[i].Kind < drift[j].Kind
		}
		return drift[i].Field < drift[j].Field
	})
	return drift
}

func appendFieldDrift(drift []BacklogDrift, backlogFile, featureID, field, canonical, mirror string) []BacklogDrift {
	if canonical == mirror {
		return drift
	}
	return append(drift, BacklogDrift{
		Kind:      BacklogDriftMismatch,
		FeatureID: featureID,
		Field:     field,
		Canonical: canonical,
		Mirror:    mirror,
		Message: fmt.Sprintf(
			"%s mismatch for feature %q field %q: canonical %q, mirror %q",
			backlogFile,
			featureID,
			field,
			canonical,
			mirror,
		),
	})
}

func appendPriorityDrift(drift []BacklogDrift, backlogFile string, f Feature, mirror BacklogFeature) []BacklogDrift {
	if f.Priority == nil && mirror.Priority == nil {
		return drift
	}
	var canonical string
	if f.Priority != nil {
		canonical = strconv.Itoa(*f.Priority)
	}
	if mirror.Priority == nil {
		if f.Priority == nil {
			return drift
		}
		return appendFieldDrift(drift, backlogFile, f.ID, "priority", canonical, "")
	}
	mirrorStr := strconv.Itoa(*mirror.Priority)
	if f.Priority == nil {
		return appendFieldDrift(drift, backlogFile, f.ID, "priority", "", mirrorStr)
	}
	return appendFieldDrift(drift, backlogFile, f.ID, "priority", canonical, mirrorStr)
}
