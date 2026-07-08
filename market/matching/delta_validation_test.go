package matching

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func TestMatchingBookPreservesStateAfterInvalidDelta(t *testing.T) {
	book := orderbook.NewOrderBook("BTC-USDC", orderbook.Config{MaxDepth: 100})
	engine := NewMatchingEngine(EngineConfig{EnableShorting: true}, map[types.Symbol]*orderbook.OrderBook{
		"BTC-USDC": book,
	})
	_, err := engine.PlaceOrder(&types.Order{
		Symbol:       "BTC-USDC",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.RequireFromString("100"),
		Quantity:     decimal.RequireFromString("1"),
		RemainingQty: decimal.RequireFromString("1"),
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	before := book.GetSnapshot()

	err = book.ApplyDelta(orderbook.Delta{
		Symbol:   "BTC-USDC",
		Sequence: book.Sequence() + 1,
		Bids: []types.Level{{
			Price:    decimal.RequireFromString("99"),
			Quantity: decimal.RequireFromString("-1"),
			Count:    1,
		}},
	})
	if err == nil {
		t.Fatal("expected invalid quantity error")
	}

	after := book.GetSnapshot()
	if len(after.Bids) != len(before.Bids) || !after.Bids[0].Quantity.Equal(before.Bids[0].Quantity) {
		t.Fatalf("book mutated after invalid delta: before %+v after %+v", before, after)
	}
}
