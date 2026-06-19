package ws

import (
	"encoding/json"
	"errors"
)

func ValidateOrderBookDelta(deltaJSON []byte) error {
	var delta map[string]interface{}
	if err := json.Unmarshal(deltaJSON, &delta); err != nil {
		return err
	}
	// Validate the delta
	if _, ok := delta["price"]; !ok || delta["price"] == nil {
		return errors.New("invalid price")
	}
	if _, ok := delta["quantity"]; !ok || delta["quantity"] == nil {
		return errors.New("invalid quantity")
	}
	if _, ok := delta["side"]; !ok || delta["side"] == nil {
		return errors.New("invalid side")
	}
	if _, ok := delta["symbol"]; !ok || delta["symbol"] == nil {
		return errors.New("invalid symbol")
	}
	// Check for stale delta
	if sequence, ok := delta["sequence"]; ok && sequence != nil {
		// For this example, we assume the current sequence is 20
		if sequence.(float64) < 20 {
			return errors.New("stale delta")
		}
	}
	return nil
}
