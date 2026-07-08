package ws

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

// TestDecodeDepthDelta_Happy verifies a well-formed wire payload decodes and validates.
func TestDecodeDepthDelta_Happy(t *testing.T) {
	raw := []byte(`{
		"symbol":"BTCUSDT",
		"side":"buy",
		"price":"100",
		"quantity":"5",
		"sequence":1,
		"timestamp":1700000000000
	}`)
	d, err := DecodeDepthDelta(raw)
	if err != nil {
		t.Fatalf("expected decode success, got: %v", err)
	}
	if d.Symbol != "BTCUSDT" {
		t.Errorf("symbol: got %s", d.Symbol)
	}
	if d.Side != types.Buy {
		t.Errorf("side: got %v", d.Side)
	}
	if !d.Price.Equal(decimal.NewFromInt(100)) {
		t.Errorf("price: got %s", d.Price)
	}
	if d.Sequence != 1 {
		t.Errorf("sequence: got %d", d.Sequence)
	}
}

// TestDecodeDepthDelta_MalformedJSON returns a parse error for broken JSON.
func TestDecodeDepthDelta_MalformedJSON(t *testing.T) {
	_, err := DecodeDepthDelta([]byte(`{"symbol": "BTCUSDT", "price":`))
	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected 'decode' in error, got: %v", err)
	}
}

// TestDecodeDepthDelta_EmptyPayload rejects empty bytes.
func TestDecodeDepthDelta_EmptyPayload(t *testing.T) {
	_, err := DecodeDepthDelta([]byte{})
	if err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}

// TestDecodeDepthDelta_RejectsValidationFailure ensures schema errors are surfaced.
func TestDecodeDepthDelta_RejectsValidationFailure(t *testing.T) {
	// price = 0 -> invalid
	raw := []byte(`{"symbol":"BTCUSDT","side":"buy","price":"0","quantity":"5","sequence":1,"timestamp":1}`)
	_, err := DecodeDepthDelta(raw)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "price") {
		t.Errorf("expected price in error, got: %v", err)
	}
}

// TestDecodeDepthDelta_RejectsBadSide ensures side parsing handles unknown strings.
func TestDecodeDepthDelta_RejectsBadSide(t *testing.T) {
	raw := []byte(`{"symbol":"BTCUSDT","side":"moon","price":"100","quantity":"5","sequence":1,"timestamp":1}`)
	_, err := DecodeDepthDelta(raw)
	if err == nil {
		t.Fatal("expected side error, got nil")
	}
}

// TestDecodeDepthDelta_AcceptsNumericStringDecimals verifies decimal-as-string is supported
// (Go's shopspring/decimal decodes JSON numbers/strings interchangeably).
func TestDecodeDepthDelta_AcceptsNumericStringDecimals(t *testing.T) {
	raw := []byte(`{"symbol":"BTCUSDT","side":"sell","price":101.5,"quantity":"3","sequence":2,"timestamp":1700000000002}`)
	d, err := DecodeDepthDelta(raw)
	if err != nil {
		t.Fatalf("expected decode success, got: %v", err)
	}
	if d.Side != types.Sell {
		t.Errorf("expected sell side, got %v", d.Side)
	}
	if !d.Price.Equal(decimal.RequireFromString("101.5")) {
		t.Errorf("expected price 101.5, got %s", d.Price)
	}
}

// TestDecodeDepthDelta_RejectsZeroSequence catches missing sequence at the wire boundary.
func TestDecodeDepthDelta_RejectsZeroSequence(t *testing.T) {
	raw := []byte(`{"symbol":"BTCUSDT","side":"buy","price":"100","quantity":"5","sequence":0,"timestamp":1700000000000}`)
	_, err := DecodeDepthDelta(raw)
	if err == nil {
		t.Fatal("expected sequence validation error, got nil")
	}
	if !strings.Contains(err.Error(), "sequence") {
		t.Errorf("expected 'sequence' in error, got: %v", err)
	}
}

// TestDecodeDepthDelta_RejectsEmptySymbol catches bad symbol at the wire boundary.
func TestDecodeDepthDelta_RejectsEmptySymbol(t *testing.T) {
	raw := []byte(`{"symbol":"","side":"buy","price":"100","quantity":"5","sequence":1,"timestamp":1}`)
	_, err := DecodeDepthDelta(raw)
	if err == nil {
		t.Fatal("expected symbol validation error, got nil")
	}
}

// TestDecodeDepthDelta_RejectsNegativeQuantity catches bad qty at the wire boundary.
func TestDecodeDepthDelta_RejectsNegativeQuantity(t *testing.T) {
	raw := []byte(`{"symbol":"BTCUSDT","side":"buy","price":"100","quantity":"-1","sequence":1,"timestamp":1}`)
	_, err := DecodeDepthDelta(raw)
	if err == nil {
		t.Fatal("expected quantity validation error, got nil")
	}
	if !strings.Contains(err.Error(), "quantity") {
		t.Errorf("expected 'quantity' in error, got: %v", err)
	}
}

// TestParseSide_RoundTrip checks known string/int mappings.
func TestParseSide_RoundTrip(t *testing.T) {
	for _, in := range []string{"buy", "sell"} {
		s, err := ParseSide(in)
		if err != nil {
			t.Errorf("ParseSide(%q) error: %v", in, err)
			continue
		}
		if in != s.String() {
			t.Errorf("round-trip mismatch: %q -> %v -> %q", in, s, s.String())
		}
	}
	if _, err := ParseSide("moon"); err == nil {
		t.Error("expected error for unknown side string")
	}
}
