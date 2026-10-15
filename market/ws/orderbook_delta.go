package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

type orderBookDeltaMessage struct {
	Type             string             `json:"type"`
	Symbol           string             `json:"symbol"`
	PreviousSequence uint64             `json:"previous_sequence"`
	Sequence         uint64             `json:"sequence"`
	Bids             [][]string         `json:"bids,omitempty"`
	Asks             [][]string         `json:"asks,omitempty"`
	Changes          []bookLevelMessage `json:"changes,omitempty"`
	Checksum         string             `json:"checksum,omitempty"`
}

type bookLevelMessage struct {
	Side     string `json:"side"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

func ParseOrderBookDeltaMessage(payload []byte) (orderbook.DeltaUpdate, error) {
	var msg orderBookDeltaMessage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&msg); err != nil {
		return orderbook.DeltaUpdate{}, fmt.Errorf("json: invalid orderbook delta payload: %w", err)
	}

	if msg.Type != "" && msg.Type != "depth_delta" && msg.Type != "orderbook_delta" {
		return orderbook.DeltaUpdate{}, fmt.Errorf("type: must be depth_delta or orderbook_delta")
	}
	if strings.TrimSpace(msg.Symbol) == "" {
		return orderbook.DeltaUpdate{}, fmt.Errorf("symbol: symbol is required")
	}
	if msg.Sequence == 0 {
		return orderbook.DeltaUpdate{}, fmt.Errorf("sequence: sequence must be greater than zero")
	}

	levels := make([]orderbook.LevelUpdate, 0, len(msg.Bids)+len(msg.Asks)+len(msg.Changes))
	bids, err := parseBookSideLevels(msg.Bids, orderbook.SideBid, "bids")
	if err != nil {
		return orderbook.DeltaUpdate{}, err
	}
	levels = append(levels, bids...)

	asks, err := parseBookSideLevels(msg.Asks, orderbook.SideAsk, "asks")
	if err != nil {
		return orderbook.DeltaUpdate{}, err
	}
	levels = append(levels, asks...)

	changes, err := parseBookChangeLevels(msg.Changes)
	if err != nil {
		return orderbook.DeltaUpdate{}, err
	}
	levels = append(levels, changes...)

	if len(levels) == 0 {
		return orderbook.DeltaUpdate{}, fmt.Errorf("levels: at least one bid, ask, or change is required")
	}

	return orderbook.DeltaUpdate{
		Symbol:           types.Symbol(msg.Symbol),
		PreviousSequence: msg.PreviousSequence,
		Sequence:         msg.Sequence,
		Levels:           levels,
		Checksum:         msg.Checksum,
	}, nil
}

func parseBookSideLevels(raw [][]string, side orderbook.UpdateSide, field string) ([]orderbook.LevelUpdate, error) {
	levels := make([]orderbook.LevelUpdate, 0, len(raw))
	for i, pair := range raw {
		if len(pair) != 2 {
			return nil, fmt.Errorf("%s[%d]: level must contain price and quantity", field, i)
		}
		price, err := parsePositiveDecimal(pair[0], fmt.Sprintf("%s[%d].price", field, i), false)
		if err != nil {
			return nil, err
		}
		quantity, err := parsePositiveDecimal(pair[1], fmt.Sprintf("%s[%d].quantity", field, i), true)
		if err != nil {
			return nil, err
		}
		levels = append(levels, orderbook.LevelUpdate{
			Side:     side,
			Price:    price,
			Quantity: quantity,
		})
	}
	return levels, nil
}

func parseBookChangeLevels(raw []bookLevelMessage) ([]orderbook.LevelUpdate, error) {
	levels := make([]orderbook.LevelUpdate, 0, len(raw))
	for i, item := range raw {
		side, err := parseBookSide(item.Side, fmt.Sprintf("changes[%d].side", i))
		if err != nil {
			return nil, err
		}
		price, err := parsePositiveDecimal(item.Price, fmt.Sprintf("changes[%d].price", i), false)
		if err != nil {
			return nil, err
		}
		quantity, err := parsePositiveDecimal(item.Quantity, fmt.Sprintf("changes[%d].quantity", i), true)
		if err != nil {
			return nil, err
		}
		levels = append(levels, orderbook.LevelUpdate{
			Side:     side,
			Price:    price,
			Quantity: quantity,
		})
	}
	return levels, nil
}

func parseBookSide(value string, field string) (orderbook.UpdateSide, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bid", "buy":
		return orderbook.SideBid, nil
	case "ask", "sell":
		return orderbook.SideAsk, nil
	default:
		return "", fmt.Errorf("%s: side must be bid or ask", field)
	}
}

func parsePositiveDecimal(value string, field string, allowZero bool) (decimal.Decimal, error) {
	if strings.TrimSpace(value) == "" {
		return decimal.Zero, fmt.Errorf("%s: decimal value is required", field)
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%s: invalid decimal value", field)
	}
	if allowZero {
		if parsed.LessThan(decimal.Zero) {
			return decimal.Zero, fmt.Errorf("%s: value must be zero or positive", field)
		}
	} else if !parsed.GreaterThan(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("%s: value must be positive", field)
	}
	return parsed, nil
}
