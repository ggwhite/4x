package doctor

import "testing"

func TestParseBlocksOutput(t *testing.T) {
	validJSON := `{
		"blocks": [{
			"id": "2026-06-13T10:00:00.000Z",
			"startTime": "2026-06-13T10:00:00.000Z",
			"endTime": "2026-06-13T15:00:00.000Z",
			"actualEndTime": "2026-06-13T11:22:57.945Z",
			"isActive": true, "isGap": false,
			"costUSD": 25.61, "totalTokens": 28827055, "entries": 267,
			"models": ["claude-opus-4-6"],
			"tokenCounts": {"inputTokens": 376, "outputTokens": 80949, "cacheReadInputTokens": 27673929, "cacheCreationInputTokens": 1071801},
			"burnRate": {"costPerHour": 51.67, "tokensPerMinute": 969298.65},
			"projection": {"remainingMinutes": 217, "totalCost": 212.50, "totalTokens": 239164863}
		}]
	}`

	block, err := parseBlocksOutput([]byte(validJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block == nil {
		t.Fatal("expected active block, got nil")
	}
	if block.CostUSD != 25.61 {
		t.Errorf("costUSD = %f, want %f", block.CostUSD, 25.61)
	}
	if block.TotalTokens != 28827055 {
		t.Errorf("totalTokens = %d, want %d", block.TotalTokens, 28827055)
	}
	if block.Projection.RemainingMinutes != 217 {
		t.Errorf("remainingMinutes = %d, want %d", block.Projection.RemainingMinutes, 217)
	}
}

func TestParseBlocksOutput_NoActive(t *testing.T) {
	block, err := parseBlocksOutput([]byte(`{"blocks": []}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block != nil {
		t.Error("expected nil block for empty blocks")
	}
}

func TestParseBlocksOutput_Invalid(t *testing.T) {
	_, err := parseBlocksOutput([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseDailySummary(t *testing.T) {
	data := `{"daily": [
		{"totalTokens": 100000, "totalCost": 1.50},
		{"totalTokens": 200000, "costUSD": 3.00}
	]}`
	s, err := parseDailySummary([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.TotalTokens != 300000 {
		t.Errorf("totalTokens = %d, want 300000", s.TotalTokens)
	}
	if s.TotalCost != 4.50 {
		t.Errorf("totalCost = %f, want 4.50", s.TotalCost)
	}
	if s.Days != 2 {
		t.Errorf("days = %d, want 2", s.Days)
	}
}

func TestParseBlocksOutput_7dBlock(t *testing.T) {
	data := `{"blocks": [{
		"id": "2026-06-11T06:00:00.000Z",
		"startTime": "2026-06-11T06:00:00.000Z",
		"endTime": "2026-06-18T06:00:00.000Z",
		"actualEndTime": "2026-06-13T11:38:28.164Z",
		"isActive": true, "isGap": false,
		"costUSD": 1345.40, "totalTokens": 2031399011, "entries": 23166,
		"models": [],
		"tokenCounts": {"inputTokens": 0, "outputTokens": 0, "cacheReadInputTokens": 0, "cacheCreationInputTokens": 0},
		"burnRate": {"costPerHour": 0, "tokensPerMinute": 0},
		"projection": {"remainingMinutes": 6862, "totalCost": 4213.91, "totalTokens": 6362519992}
	}]}`

	block, err := parseBlocksOutput([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block == nil {
		t.Fatal("expected 7d block")
	}
	if block.Projection.RemainingMinutes != 6862 {
		t.Errorf("remainingMinutes = %d, want 6862", block.Projection.RemainingMinutes)
	}
	if block.CostUSD != 1345.40 {
		t.Errorf("costUSD = %f, want 1345.40", block.CostUSD)
	}
}
