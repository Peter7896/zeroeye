package orderbook

import (
	"encoding/json"
	"testing"
)

func TestOrderBookDeltaValidation(t *testing.T) {
	// Test malformed bid/ask price
	malformedPriceDelta := `{"price": "abc"}`
	var delta map[string]interface{}
	if err := json.Unmarshal([]byte(malformedPriceDelta), &delta); err != nil {
		t.Errorf("expected error, got nil")
	}
	if _, ok := delta["price"]; ok {
		t.Errorf("expected price to be missing, but it's present")
	}

	// Test stale or out-of-order sequence updates
	staleDelta := `{"sequence": 1}`
	if err := json.Unmarshal([]byte(staleDelta), &delta); err != nil {
		t.Errorf("expected error, got nil")
	}
	if delta["sequence"] != nil {
		t.Errorf("expected sequence to be missing, but it's present")
	}

	// Test valid snapshot followed by valid deltas
	validSnapshot := `{"snapshot": true}`
	if err := json.Unmarshal([]byte(validSnapshot), &delta); err != nil {
		t.Errorf("expected error, got nil")
	}
	if _, ok := delta["snapshot"]; !ok {
		t.Errorf("expected snapshot to be present, but it's missing")
	}
}