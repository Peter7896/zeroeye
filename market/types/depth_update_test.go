package types

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

// TestDepthDelta_Validate_Happy verifies a well-formed delta passes validation.
func TestDepthDelta_Validate_Happy(t *testing.T) {
	d := &DepthDelta{
		Symbol:    "BTCUSDT",
		Side:      Buy,
		Price:     decimal.NewFromInt(100),
		Quantity:  decimal.NewFromInt(5),
		Sequence:  1,
		Timestamp: 1700000000000,
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid delta, got error: %v", err)
	}
}

// TestDepthDelta_Validate_RejectsBadSymbol covers empty / whitespace symbols.
func TestDepthDelta_Validate_RejectsBadSymbol(t *testing.T) {
	cases := []Symbol{"", " ", "\t", "   "}
	for _, sym := range cases {
		d := validDelta()
		d.Symbol = sym
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for symbol %q, got nil", sym)
		} else if !strings.Contains(err.Error(), "symbol") {
			t.Errorf("expected symbol-related error for %q, got: %v", sym, err)
		}
	}
}

// TestDepthDelta_Validate_RejectsZeroPrice covers non-positive price.
func TestDepthDelta_Validate_RejectsZeroPrice(t *testing.T) {
	for _, p := range []string{"0", "-1", "-0.00000001"} {
		d := validDelta()
		d.Price = decimal.RequireFromString(p)
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for price %s, got nil", p)
		} else if !strings.Contains(err.Error(), "price") {
			t.Errorf("expected price-related error for %s, got: %v", p, err)
		}
	}
}

// TestDepthDelta_Validate_RejectsNegativeQuantity covers negative quantity.
// Quantity of exactly zero is allowed (it represents a level removal).
func TestDepthDelta_Validate_RejectsNegativeQuantity(t *testing.T) {
	for _, q := range []string{"-1", "-0.0001"} {
		d := validDelta()
		d.Quantity = decimal.RequireFromString(q)
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for quantity %s, got nil", q)
		} else if !strings.Contains(err.Error(), "quantity") {
			t.Errorf("expected quantity-related error for %s, got: %v", q, err)
		}
	}
}

// TestDepthDelta_Validate_AllowsZeroQuantity signals level removal and is permitted.
func TestDepthDelta_Validate_AllowsZeroQuantity(t *testing.T) {
	d := validDelta()
	d.Quantity = decimal.Zero
	if err := d.Validate(); err != nil {
		t.Fatalf("zero quantity should be allowed (level removal), got: %v", err)
	}
}

// TestDepthDelta_Validate_RejectsUnknownSide covers invalid side values.
func TestDepthDelta_Validate_RejectsUnknownSide(t *testing.T) {
	d := validDelta()
	d.Side = OrderSide(99)
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for unknown side, got nil")
	} else if !strings.Contains(err.Error(), "side") {
		t.Errorf("expected side-related error, got: %v", err)
	}
}

// TestDepthDelta_Validate_RejectsZeroSequence covers missing sequence numbers.
func TestDepthDelta_Validate_RejectsZeroSequence(t *testing.T) {
	d := validDelta()
	d.Sequence = 0
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for sequence 0, got nil")
	} else if !strings.Contains(err.Error(), "sequence") {
		t.Errorf("expected sequence-related error, got: %v", err)
	}
}

// TestDepthDelta_Validate_RejectsZeroTimestamp covers missing timestamps.
func TestDepthDelta_Validate_RejectsZeroTimestamp(t *testing.T) {
	d := validDelta()
	d.Timestamp = 0
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for timestamp 0, got nil")
	} else if !strings.Contains(err.Error(), "timestamp") {
		t.Errorf("expected timestamp-related error, got: %v", err)
	}
}

// TestDepthDelta_JSONDecode_Happy verifies round-trip JSON encoding.
func TestDepthDelta_JSONDecode_Happy(t *testing.T) {
	original := validDelta()
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded DepthDelta
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !decoded.Price.Equal(original.Price) {
		t.Errorf("price mismatch: got %s want %s", decoded.Price, original.Price)
	}
	if !decoded.Quantity.Equal(original.Quantity) {
		t.Errorf("quantity mismatch: got %s want %s", decoded.Quantity, original.Quantity)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("sequence mismatch: got %d want %d", decoded.Sequence, original.Sequence)
	}
	if decoded.Symbol != original.Symbol {
		t.Errorf("symbol mismatch: got %s want %s", decoded.Symbol, original.Symbol)
	}
}

// TestDepthDelta_JSONDecode_Malformed verifies malformed JSON returns an error.
func TestDepthDelta_JSONDecode_Malformed(t *testing.T) {
	var d DepthDelta
	err := json.Unmarshal([]byte(`{"symbol": "BTCUSDT", "price":`), &d)
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

// TestDepthDelta_JSONDecode_MissingFields verifies required fields surface errors.
func TestDepthDelta_JSONDecode_MissingFields(t *testing.T) {
	// Sequence missing -> Validation should catch it post-decode.
	raw := `{"symbol":"BTCUSDT","side":0,"price":"100","quantity":"5","timestamp":1700000000000}`
	var d DepthDelta
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode should succeed, validation should fail: %v", err)
	}
	d.Sequence = 0 // zero-value default
	if err := d.Validate(); err == nil {
		t.Fatal("expected validation error for missing sequence, got nil")
	}
}

// TestSequenceGapError_IsError ensures the typed error satisfies the error interface
// and preserves the gap magnitude for diagnostics.
func TestSequenceGapError_IsError(t *testing.T) {
	e := &SequenceGapError{Expected: 5, Got: 7}
	if e.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	if e.Gap() != 2 {
		t.Errorf("expected gap 2, got %d", e.Gap())
	}
	if e.IsStale() {
		t.Error("expected IsStale() false for forward gap")
	}
	// Stale case
	stale := &SequenceGapError{Expected: 10, Got: 10}
	if !stale.IsStale() {
		t.Error("expected IsStale() true when Got == Expected")
	}
}

func validDelta() *DepthDelta {
	return &DepthDelta{
		Symbol:    "BTCUSDT",
		Side:      Buy,
		Price:     decimal.NewFromInt(100),
		Quantity:  decimal.NewFromInt(5),
		Sequence:  1,
		Timestamp: 1700000000000,
	}
}
