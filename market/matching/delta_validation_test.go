package matching

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func TestInvalidDeltaPreservesEngineOwnedOrder(t *testing.T) {
	book := orderbook.NewOrderBook("BTC-USD", orderbook.Config{MaxDepth: 5})
	engine := NewMatchingEngine(EngineConfig{}, map[types.Symbol]*orderbook.OrderBook{"BTC-USD": book})
	order := &types.Order{ID: "existing-order", Symbol: "BTC-USD", Side: types.Buy, Type: types.Limit, Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1), RemainingQty: decimal.NewFromInt(1)}
	if _, err := engine.PlaceOrder(order); err != nil {
		t.Fatal(err)
	}
	before := book.GetSnapshot()
	checksum := orderbook.DepthChecksum(before.Bids, before.Asks)
	err := book.ApplyDelta(orderbook.DepthDelta{Symbol: "BTC-USD", PreviousSequence: 1, Sequence: 2, Updates: []orderbook.DepthDeltaLevel{{Side: "bid", Price: decimal.NewFromInt(100), Quantity: decimal.Zero}}, Checksum: "invalid"})
	if !errors.Is(err, orderbook.ErrDeltaChecksumMismatch) {
		t.Fatalf("got %v", err)
	}
	after := book.GetSnapshot()
	if orderbook.DepthChecksum(after.Bids, after.Asks) != checksum {
		t.Fatal("rejected delta changed engine-owned book")
	}
	if err := engine.CancelOrder("BTC-USD", "existing-order"); err != nil {
		t.Fatalf("rejected delta lost engine order: %v", err)
	}
	if len(book.GetSnapshot().Bids) != 0 {
		t.Fatal("cancel did not affect the same engine-owned book")
	}
}
