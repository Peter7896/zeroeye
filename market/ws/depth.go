package ws

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

// DepthMessage is the on-the-wire shape of a depth feed frame.
type DepthMessage struct {
	Type    string             `json:"type"`
	Symbol  types.Symbol       `json:"symbol"`
	Payload *types.DepthUpdate `json:"payload"`
}

// ParseDepthDelta decodes a depth frame from the WebSocket feed.
//
// decimal.Decimal marshals from JSON strings or numbers; the feed is allowed to
// send either, and an unparseable value must surface as an error rather than a
// silently zeroed level. Quantity must be present and non-null; only an explicit
// zero is a deletion command. Sequence and checksum are supplied separately by
// the feed adapter after ToDelta; they are not fields of types.DepthUpdate.
func ParseDepthDelta(raw []byte) (types.DepthUpdate, error) {
	// Preserve quantity presence at the wire boundary, as in the stricter helper
	// in PR #29. Decoding directly into types.Level loses missing/null versus 0.
	type wireLevel struct {
		Price    decimal.Decimal  `json:"price"`
		Quantity *decimal.Decimal `json:"quantity"`
		Count    int64            `json:"order_count"`
	}
	var msg struct {
		Type    string `json:"type"`
		Payload *struct {
			Symbol    types.Symbol `json:"symbol"`
			Bids      []wireLevel  `json:"bids"`
			Asks      []wireLevel  `json:"asks"`
			Timestamp int64        `json:"timestamp"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return types.DepthUpdate{}, err
	}
	switch msg.Type {
	case "depth_snapshot", "depth_delta":
	default:
		return types.DepthUpdate{}, &FeedError{Event: msg.Type, Reason: "unsupported event type"}
	}
	if msg.Payload == nil {
		return types.DepthUpdate{}, &FeedError{Event: msg.Type, Reason: "missing payload"}
	}
	// Validate levels at the feed boundary so a bad frame is rejected before it
	// can reach the book. The book re-validates independently.
	u := types.DepthUpdate{Symbol: msg.Payload.Symbol, Timestamp: msg.Payload.Timestamp}
	for _, side := range []struct {
		name   string
		levels []wireLevel
		output *[]types.Level
	}{{"bid", msg.Payload.Bids, &u.Bids}, {"ask", msg.Payload.Asks, &u.Asks}} {
		if side.levels != nil {
			*side.output = make([]types.Level, 0, len(side.levels))
		}
		for i, l := range side.levels {
			if !l.Price.IsPositive() {
				return types.DepthUpdate{}, &FeedError{
					Event:  msg.Type,
					Reason: fmt.Sprintf("%s[%d] price must be greater than zero, got %s", side.name, i, l.Price.String()),
				}
			}
			if l.Quantity == nil {
				return types.DepthUpdate{}, &FeedError{
					Event:  msg.Type,
					Reason: fmt.Sprintf("%s[%d] quantity is required and must not be null", side.name, i),
				}
			}
			if l.Quantity.IsNegative() {
				return types.DepthUpdate{}, &FeedError{
					Event:  msg.Type,
					Reason: fmt.Sprintf("%s[%d] quantity must not be negative, got %s", side.name, i, l.Quantity.String()),
				}
			}
			*side.output = append(*side.output, types.Level{Price: l.Price, Quantity: *l.Quantity, Count: l.Count})
		}
	}
	return u, nil
}

// ToDelta converts a decoded depth frame into a single-level orderbook.Delta.
//
// A frame with no levels is reported via the existing isSnapshot flag.
// Sequence and Checksum remain unset: the feed adapter must supply its sequence
// and optional post-update checksum before ApplyDelta. Do not invent a sequence
// from arrival order; ApplyDelta rejects missing/stale/gapped sequence values.
func ToDelta(u types.DepthUpdate) (orderbook.Delta, bool, error) {
	bids, asks := u.Bids, u.Asks
	switch {
	case len(bids) > 0 && len(asks) > 0:
		return orderbook.Delta{}, false, &FeedError{Reason: "frame carries both bids and asks"}
	case len(bids) > 1 || len(asks) > 1:
		return orderbook.Delta{}, false, &FeedError{Reason: "delta frame carries multiple levels"}
	}

	var level *types.Level
	side := types.Buy
	if len(asks) == 1 {
		level, side = &u.Asks[0], types.Sell
	} else if len(bids) == 1 {
		level = &u.Bids[0]
	} else {
		return orderbook.Delta{}, true, nil
	}
	return orderbook.Delta{
		Symbol:   u.Symbol,
		Side:     side,
		Price:    level.Price,
		Quantity: level.Quantity,
	}, false, nil
}

type FeedError struct {
	Event  string
	Reason string
}

func (e *FeedError) Error() string {
	if e.Event != "" {
		return "depth feed: unsupported event " + e.Event + ": " + e.Reason
	}
	return "depth feed: " + e.Reason
}
