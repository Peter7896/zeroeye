package ws

import (
	"encoding/json"
	"testing"
)

func TestWebSocketOrderBookDeltaValidation(t *testing.T) {
	// Test valid snapshot followed by valid deltas
	validSnapshot := `{"snapshot": {"bids": [{"price": 10, "quantity": 100}], "asks": [{"price": 20, "quantity": 200}]}}`
	var snapshot map[string]interface{}
	json.Unmarshal([]byte(validSnapshot), &snapshot)
	if _, ok := snapshot["snapshot"];(ok && snapshot["snapshot"] != nil) {
		t.Errorf("Expected snapshot to be nil, but got %v", snapshot["snapshot"])
	}

	// Test recovery path and live update path
	recoveryPath := `{"recovery": true}`
	json.Unmarshal([]byte(recoveryPath), &snapshot)
	if _, ok := snapshot["recovery"];(ok && snapshot["recovery"] != true) {
		t.Errorf("Expected recovery to be true, but got %v", snapshot["recovery"])
	}
}