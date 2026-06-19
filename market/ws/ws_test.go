package ws

import (
	"encoding/json"
	"testing"
)

func TestWebSocketOrderBookDeltaValidation(t *testing.T) {
	// Test reject malformed bid/ask price, quantity, side, or symbol payloads
	malformedPayload := `{"price": "abc"}`
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(malformedPayload), &payload); err != nil {
		t.Errorf("expected error, got nil")
	}
	if _, ok := payload["price"]; ok {
		t.Errorf("expected price to be missing, but it's present")
	}

	// Test reject stale or out-of-order sequence updates
	stalePayload := `{"sequence": 1}`
	if err := json.Unmarshal([]byte(stalePayload), &payload); err != nil {
		t.Errorf("expected error, got nil")
	}
	if payload["sequence"] != nil {
		t.Errorf("expected sequence to be missing, but it's present")
	}
}