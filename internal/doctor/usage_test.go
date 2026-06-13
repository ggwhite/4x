package doctor

import "testing"

func TestParseCcusageOutput(t *testing.T) {
	validJSON := `{
		"daily": [
			{
				"agent": "all",
				"period": "2026-06-12",
				"inputTokens": 221810,
				"outputTokens": 8137,
				"cacheReadTokens": 426739,
				"cacheCreationTokens": 0,
				"totalTokens": 663361,
				"totalCost": 0.176,
				"modelsUsed": ["claude-opus-4-6"],
				"metadata": {"agents": ["claude"]},
				"modelBreakdowns": [
					{
						"modelName": "claude-opus-4-6",
						"inputTokens": 221810,
						"outputTokens": 8137,
						"cacheReadTokens": 426739,
						"cacheCreationTokens": 0,
						"cost": 0.176
					}
				]
			}
		]
	}`

	entries, err := parseCcusageOutput([]byte(validJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Period != "2026-06-12" {
		t.Errorf("period = %q, want %q", entries[0].Period, "2026-06-12")
	}
	if entries[0].TotalTokens != 663361 {
		t.Errorf("totalTokens = %d, want %d", entries[0].TotalTokens, 663361)
	}
	if entries[0].TotalCost != 0.176 {
		t.Errorf("totalCost = %f, want %f", entries[0].TotalCost, 0.176)
	}
	if len(entries[0].ModelBreakdowns) != 1 {
		t.Fatalf("expected 1 model breakdown, got %d", len(entries[0].ModelBreakdowns))
	}
	if entries[0].ModelBreakdowns[0].ModelName != "claude-opus-4-6" {
		t.Errorf("modelName = %q, want %q", entries[0].ModelBreakdowns[0].ModelName, "claude-opus-4-6")
	}
}

func TestParseCcusageOutput_Invalid(t *testing.T) {
	_, err := parseCcusageOutput([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseCcusageOutput_Empty(t *testing.T) {
	entries, err := parseCcusageOutput([]byte(`{"daily": []}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
