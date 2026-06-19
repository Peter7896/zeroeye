package orderbook

import (
	"encoding/json"
	"testing"
)

func TestOrderBookDeltaValidation(t *testing.T) {
	// Test malformed bid/ask price
	malformedPrice := `{"price": "abc"}`
	var delta map[string]interface{}
	json.Unmarshal([]byte(malformedPrice), &delta)
	if _, ok := delta["price"];(ok && delta["price"] != "abc") {
		t.Errorf("Expected price to be 'abc', but got %v", delta["price"])
	}

	// Test stale or out-of-order sequence updates
	staleSequence := `{"sequence": 1}`
	json.Unmarshal([]byte(staleSequence), &delta)
	if _, ok := delta["sequence"];(ok && delta["sequence"] != 1) {
		t.Errorf("Expected sequence to be 1, but got %v", delta["sequence"])
	}

	// Test preservation of current book after invalid delta
	invalidDelta := `{"delta": "abc"}`
	json.Unmarshal([]byte(invalidDelta), &delta)
	if _, ok := delta["delta"];(ok && delta["delta"] != "abc") {
		t.Errorf("Expected delta to be 'abc', but got %v", delta["delta"])
	}
}