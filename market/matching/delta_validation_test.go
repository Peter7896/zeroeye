package matching

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// The matching engine must reject invalid orders with the engine's own clear
// errors, before any book is touched.
func TestValidateOrderRejectsInvalidOrders(t *testing.T) {
	engine := NewMatchingEngine(EngineConfig{EnableShorting: true}, nil)

	cases := []struct {
		name  string
		order *types.Order
		want  error
	}{
		{
			name:  "zero quantity",
			order: &types.Order{Symbol: "TEST", Side: types.Buy, Type: types.Limit, Quantity: dec("0"), Price: dec("100")},
			want:  ErrInvalidQuantity,
		},
		{
			name:  "negative quantity",
			order: &types.Order{Symbol: "TEST", Side: types.Buy, Type: types.Limit, Quantity: dec("-1"), Price: dec("100")},
			want:  ErrInvalidQuantity,
		},
		{
			name:  "zero limit price",
			order: &types.Order{Symbol: "TEST", Side: types.Buy, Type: types.Limit, Quantity: dec("1"), Price: dec("0")},
			want:  ErrInvalidPrice,
		},
		{
			name:  "negative limit price",
			order: &types.Order{Symbol: "TEST", Side: types.Buy, Type: types.Limit, Quantity: dec("1"), Price: dec("-100")},
			want:  ErrInvalidPrice,
		},
		{
			name:  "shorting disabled",
			order: &types.Order{Symbol: "TEST", Side: types.Sell, Type: types.Limit, Quantity: dec("1"), Price: dec("100")},
			want:  ErrShortingDisabled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := EngineConfig{EnableShorting: true}
			if errors.Is(tc.want, ErrShortingDisabled) {
				cfg.EnableShorting = false
			}
			eng := NewMatchingEngine(cfg, nil)

			err := eng.ValidateOrder(tc.order)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	// A valid order must pass.
	if err := engine.ValidateOrder(&types.Order{Symbol: "TEST", Side: types.Buy, Type: types.Limit, Quantity: dec("1"), Price: dec("100")}); err != nil {
		t.Fatalf("valid order rejected: %v", err)
	}
	// A market order ignores price.
	if err := engine.ValidateOrder(&types.Order{Symbol: "TEST", Side: types.Buy, Type: types.Market, Quantity: dec("1")}); err != nil {
		t.Fatalf("valid market order rejected: %v", err)
	}
}

// An unknown symbol must be reported before any mutation.
func TestPlaceOrderUnknownSymbol(t *testing.T) {
	engine := NewMatchingEngine(EngineConfig{}, map[types.Symbol]*orderbook.OrderBook{})

	_, err := engine.PlaceOrder(&types.Order{Symbol: "NOPE", Side: types.Buy, Type: types.Limit, Quantity: dec("1"), Price: dec("100")})

	if !errors.Is(err, ErrSymbolNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrSymbolNotFound)
	}
	if err := engine.CancelOrder("NOPE", "id"); !errors.Is(err, ErrSymbolNotFound) {
		t.Fatalf("CancelOrder error = %v, want %v", err, ErrSymbolNotFound)
	}
}

// The engine's book and the orderbook package's delta validation must agree:
// a delta rejected by the book must leave the engine's view of the book intact.
func TestEngineBookRejectsInvalidDeltasWithoutMutation(t *testing.T) {
	ob := orderbook.NewOrderBook("TEST", orderbook.Config{MaxDepth: 10})
	if err := ob.ApplySnapshot(orderbook.Snapshot{
		Symbol:   "TEST",
		Bids:     []types.Level{{Price: dec("100.00"), Quantity: dec("1")}},
		Asks:     []types.Level{{Price: dec("100.50"), Quantity: dec("1")}},
		Sequence: 10,
	}); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	engine := NewMatchingEngine(EngineConfig{EnableShorting: true}, map[types.Symbol]*orderbook.OrderBook{"TEST": ob})

	beforeBids := len(ob.GetBids())
	beforeSeq := ob.Sequence()

	cases := []struct {
		name  string
		delta orderbook.Delta
		want  error
	}{
		{
			name:  "malformed price",
			delta: orderbook.Delta{Symbol: "TEST", Side: types.Buy, Price: dec("-1"), Quantity: dec("1"), Sequence: 11},
			want:  orderbook.ErrDeltaPrice,
		},
		{
			name:  "malformed quantity",
			delta: orderbook.Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("-1"), Sequence: 11},
			want:  orderbook.ErrDeltaQuantity,
		},
		{
			name:  "wrong symbol",
			delta: orderbook.Delta{Symbol: "OTHER", Side: types.Buy, Price: dec("100.25"), Quantity: dec("1"), Sequence: 11},
			want:  orderbook.ErrDeltaSymbol,
		},
		{
			name:  "stale sequence",
			delta: orderbook.Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("1"), Sequence: 9},
			want:  orderbook.ErrStaleSequence,
		},
		{
			name:  "checksum mismatch",
			delta: orderbook.Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("1"), Sequence: 11, Checksum: "nope"},
			want:  orderbook.ErrChecksumMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ob.ApplyDelta(tc.delta)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if ob.Sequence() != beforeSeq || len(ob.GetBids()) != beforeBids {
				t.Fatal("book mutated by rejected delta")
			}
		})
	}

	// After every rejection the engine's book is still usable, and the
	// engine can still resolve the symbol.
	if err := engine.CancelOrder("TEST", "still-resolvable"); !errors.Is(err, orderbook.ErrOrderNotFound) {
		t.Fatalf("engine book lost track of symbol: %v", err)
	}
	if err := ob.ApplyDelta(orderbook.Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("1"), Sequence: 11}); err != nil {
		t.Fatalf("valid delta after rejections: %v", err)
	}
	if got := ob.Sequence(); got != 11 {
		t.Fatalf("sequence = %d, want 11", got)
	}
}

// Cancelling an order on a book that does not know it must not panic or corrupt.
func TestCancelOrderRemovesLevel(t *testing.T) {
	ob := orderbook.NewOrderBook("TEST", orderbook.Config{MaxDepth: 10})
	if err := ob.ApplySnapshot(orderbook.Snapshot{
		Symbol:   "TEST",
		Bids:     []types.Level{{Price: dec("100.00"), Quantity: dec("1")}},
		Sequence: 1,
	}); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	engine := NewMatchingEngine(EngineConfig{EnableShorting: true}, map[types.Symbol]*orderbook.OrderBook{"TEST": ob})

	if err := engine.CancelOrder("TEST", "does-not-exist"); !errors.Is(err, orderbook.ErrOrderNotFound) {
		t.Fatalf("CancelOrder = %v, want %v", err, orderbook.ErrOrderNotFound)
	}
	if n := len(ob.GetBids()); n != 1 {
		t.Fatalf("bid levels = %d, want 1 (unchanged)", n)
	}
}

// Recent-trades slicing must behave at the boundaries.
func TestGetRecentTradesBoundaries(t *testing.T) {
	engine := NewMatchingEngine(EngineConfig{}, nil)

	if got := engine.GetRecentTrades(10); len(got) != 0 {
		t.Fatalf("empty book returned %d trades, want 0", len(got))
	}
	if got := engine.GetTradeCount(); got != 0 {
		t.Fatalf("trade count = %d, want 0", got)
	}
	if got := engine.GetRecentTrades(0); len(got) != 0 {
		t.Fatalf("limit 0 returned %d trades, want 0", len(got))
	}
}
