package ws

import (
	"errors"
	"testing"
)

func TestParseOrderBookDeltaMessageRejectsMalformedPayload(t *testing.T) {
	_, err := ParseOrderBookDeltaMessage([]byte(`{"type":"orderbook_delta","delta":{"symbol":"BTC-USDC","sequence":1,"bids":[{"price":]}}`))
	if !errors.Is(err, ErrInvalidDeltaMessage) {
		t.Fatalf("expected invalid delta message error, got %v", err)
	}
}

func TestParseOrderBookDeltaMessageRejectsWrongEventType(t *testing.T) {
	_, err := ParseOrderBookDeltaMessage([]byte(`{"type":"trade","delta":{"symbol":"BTC-USDC","sequence":1}}`))
	if !errors.Is(err, ErrInvalidDeltaMessage) {
		t.Fatalf("expected invalid delta message error, got %v", err)
	}
}

func TestParseOrderBookDeltaMessageAcceptsValidDelta(t *testing.T) {
	delta, err := ParseOrderBookDeltaMessage([]byte(`{
		"type":"orderbook_delta",
		"delta":{
			"symbol":"BTC-USDC",
			"sequence":2,
			"bids":[{"price":"100","quantity":"1","order_count":1}]
		}
	}`))
	if err != nil {
		t.Fatalf("parse valid delta: %v", err)
	}
	if delta.Symbol != "BTC-USDC" || delta.Sequence != 2 || len(delta.Bids) != 1 {
		t.Fatalf("unexpected delta: %+v", delta)
	}
}
