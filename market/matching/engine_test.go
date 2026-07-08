package matching

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func TestValidateOrder_AcceptsValidOrder(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if err := e.ValidateOrder(o); err != nil {
		t.Fatalf("expected valid order, got: %v", err)
	}
}

func TestValidateOrder_RejectsZeroQuantity(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.Zero,
		RemainingQty: decimal.Zero,
	}
	if err := e.ValidateOrder(o); err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestValidateOrder_RejectsNegativeQuantity(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.NewFromInt(-3),
		RemainingQty: decimal.NewFromInt(-3),
	}
	if err := e.ValidateOrder(o); err == nil {
		t.Fatal("expected error for negative quantity")
	}
}

func TestValidateOrder_RejectsZeroLimitPrice(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.Zero,
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if err := e.ValidateOrder(o); err == nil {
		t.Fatal("expected error for zero limit price")
	}
}

func TestValidateOrder_RejectsNegativeLimitPrice(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(-1),
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if err := e.ValidateOrder(o); err == nil {
		t.Fatal("expected error for negative limit price")
	}
}

func TestValidateOrder_MarketOrderSkipsPriceCheck(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Buy,
		Type:         types.Market,
		Price:        decimal.Zero, // market orders have no price
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if err := e.ValidateOrder(o); err != nil {
		t.Fatalf("market order with zero price should be allowed, got: %v", err)
	}
}

func TestValidateOrder_RejectsShortingWhenDisabled(t *testing.T) {
	e := newTestEngine() // EnableShorting defaults to false
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Sell,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if err := e.ValidateOrder(o); err == nil {
		t.Fatal("expected error for shorting when disabled")
	}
}

func TestValidateOrder_AllowsShortingWhenEnabled(t *testing.T) {
	e := newTestEngine()
	e.config.EnableShorting = true
	o := &types.Order{
		Symbol:       "BTCUSDT",
		Side:         types.Sell,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if err := e.ValidateOrder(o); err != nil {
		t.Fatalf("sell should be allowed when shorting enabled, got: %v", err)
	}
}

func TestPlaceOrder_UnknownSymbol(t *testing.T) {
	e := newTestEngine()
	o := &types.Order{
		Symbol:       "DOGEUSDT",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(100),
		Quantity:     decimal.NewFromInt(5),
		RemainingQty: decimal.NewFromInt(5),
	}
	if _, err := e.PlaceOrder(o); err == nil {
		t.Fatal("expected unknown-symbol error")
	}
}

// newTestEngine builds an engine with a single BTCUSDT book, shorting disabled.
func newTestEngine() *MatchingEngine {
	cfg := EngineConfig{
		OrderTimeoutMs:   1000,
		MaxPendingOrders: 100,
		EnableShorting:   false,
		FeeRate:          "0.001",
		MakerFeeRate:     "0.001",
	}
	books := map[types.Symbol]*orderbook.OrderBook{
		"BTCUSDT": orderbook.NewOrderBook("BTCUSDT", orderbook.Config{
			MaxDepth:       10,
			PriceDecimals:  2,
			VolumeDecimals: 8,
		}),
	}
	return NewMatchingEngine(cfg, books)
}
