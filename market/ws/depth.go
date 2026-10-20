package ws

import (
	"encoding/json"
	"fmt"

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
// silently zeroed level.
func ParseDepthDelta(raw []byte) (types.DepthUpdate, error) {
	var msg DepthMessage
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
	for _, side := range []struct {
		name   string
		levels []types.Level
	}{{"bid", msg.Payload.Bids}, {"ask", msg.Payload.Asks}} {
		for i, l := range side.levels {
			if !l.Price.IsPositive() {
				return types.DepthUpdate{}, &FeedError{
					Event:  msg.Type,
					Reason: fmt.Sprintf("%s[%d] price must be greater than zero, got %s", side.name, i, l.Price.String()),
				}
			}
			if l.Quantity.IsNegative() {
				return types.DepthUpdate{}, &FeedError{
					Event:  msg.Type,
					Reason: fmt.Sprintf("%s[%d] quantity must not be negative, got %s", side.name, i, l.Quantity.String()),
				}
			}
		}
	}
	return *msg.Payload, nil
}

// ToDelta converts a decoded depth frame into a single-level orderbook.Delta.
//
// An empty side means "snapshot" and is reported via isSnapshot.
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
