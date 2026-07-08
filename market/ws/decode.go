package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

// wireDepthDelta mirrors types.DepthDelta but uses string side for symmetric
// JSON encoding with upstream exchanges (Binance-style payloads use "buy"/"sell").
// Price/Quantity are kept as raw JSON so we can accept either JSON numbers
// (e.g. 101.5) or string-encoded decimals (e.g. "101.5").
type wireDepthDelta struct {
	Symbol    string          `json:"symbol"`
	Side      string          `json:"side"`
	Price     json.RawMessage `json:"price"`
	Quantity  json.RawMessage `json:"quantity"`
	Sequence  uint64          `json:"sequence"`
	Timestamp int64           `json:"timestamp"`
}

// DecodeDepthDelta parses a JSON wire payload into a validated *types.DepthDelta.
// Errors are descriptive enough for upstream logging without leaking internals.
func DecodeDepthDelta(raw []byte) (*types.DepthDelta, error) {
	if len(raw) == 0 {
		return nil, errors.New("decode: empty payload")
	}
	var w wireDepthDelta
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("decode: invalid json: %w", err)
	}

	side, err := ParseSide(w.Side)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	price, err := parseDecimalJSON(w.Price)
	if err != nil {
		return nil, fmt.Errorf("decode: invalid price %q: %w", string(w.Price), err)
	}
	qty, err := parseDecimalJSON(w.Quantity)
	if err != nil {
		return nil, fmt.Errorf("decode: invalid quantity %q: %w", string(w.Quantity), err)
	}

	d := &types.DepthDelta{
		Symbol:    types.Symbol(strings.TrimSpace(w.Symbol)),
		Side:      side,
		Price:     price,
		Quantity:  qty,
		Sequence:  w.Sequence,
		Timestamp: w.Timestamp,
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

// ParseSide maps wire strings to types.OrderSide.
func ParseSide(s string) (types.OrderSide, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "buy", "bid", "bids":
		return types.Buy, nil
	case "sell", "ask", "asks", "offer":
		return types.Sell, nil
	default:
		return 0, fmt.Errorf("invalid side %q (expected buy|sell)", s)
	}
}

// parseDecimalJSON accepts a JSON value that is either a number or a quoted string
// and returns a decimal.Decimal. Empty / missing / non-numeric values are errors.
func parseDecimalJSON(raw json.RawMessage) (decimal.Decimal, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return decimal.Zero, errors.New("empty value")
	}
	// Unwrap JSON string literal so we accept both "101.5" and 101.5.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return decimal.Zero, err
		}
		s = inner
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero, errors.New("empty value")
	}
	return decimal.NewFromString(s)
}
