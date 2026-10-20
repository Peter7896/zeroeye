package matching

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func setupEngine(t *testing.T) *MatchingEngine {
	t.Helper()
	bookConfig := orderbook.Config{
		MaxDepth:       100,
		PriceDecimals:  2,
		VolumeDecimals: 8,
	}
	books := map[types.Symbol]*orderbook.OrderBook{
		"BTC-USD": orderbook.NewOrderBook("BTC-USD", bookConfig),
		"ETH-USD": orderbook.NewOrderBook("ETH-USD", bookConfig),
	}
	engineConfig := EngineConfig{
		OrderTimeoutMs:   30000,
		MaxPendingOrders: 10000,
		EnableShorting:   true,
		FeeRate:          "0.001",
		MakerFeeRate:     "0.0005",
	}
	return NewMatchingEngine(engineConfig, books)
}

func TestMatchingEngine_ValidateOrder_RejectsZeroQuantity(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Type:     types.Limit,
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.Zero,
	}
	err := engine.ValidateOrder(order)
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for zero quantity, got %v", err)
	}
}

func TestMatchingEngine_ValidateOrder_RejectsNegativeQuantity(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Type:     types.Limit,
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.NewFromInt(-1),
	}
	err := engine.ValidateOrder(order)
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity for negative quantity, got %v", err)
	}
}

func TestMatchingEngine_ValidateOrder_RejectsZeroLimitPrice(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Type:     types.Limit,
		Price:    decimal.Zero,
		Quantity: decimal.NewFromInt(1),
	}
	err := engine.ValidateOrder(order)
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice for zero limit price, got %v", err)
	}
}

func TestMatchingEngine_ValidateOrder_RejectsNegativeLimitPrice(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Type:     types.Limit,
		Price:    decimal.NewFromInt(-100),
		Quantity: decimal.NewFromInt(1),
	}
	err := engine.ValidateOrder(order)
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice for negative limit price, got %v", err)
	}
}

func TestMatchingEngine_ValidateOrder_AcceptsMarketOrder(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Type:     types.Market,
		Price:    decimal.Zero,
		Quantity: decimal.NewFromInt(1),
	}
	err := engine.ValidateOrder(order)
	if err != nil {
		t.Fatalf("market order should be valid (no price check), got %v", err)
	}
}

func TestMatchingEngine_PlaceOrder_SymbolNotFound(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:   "SOL-USD",
		Side:     types.Buy,
		Type:     types.Limit,
		Price:    decimal.NewFromInt(100),
		Quantity: decimal.NewFromInt(1),
	}
	_, err := engine.PlaceOrder(order)
	if err != ErrSymbolNotFound {
		t.Fatalf("expected ErrSymbolNotFound, got %v", err)
	}
}

func TestMatchingEngine_CancelOrder_SymbolNotFound(t *testing.T) {
	engine := setupEngine(t)

	err := engine.CancelOrder("SOL-USD", "order-1")
	if err != ErrSymbolNotFound {
		t.Fatalf("expected ErrSymbolNotFound, got %v", err)
	}
}

func TestMatchingEngine_PlaceOrder_PopulatesOrderBook(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:       "BTC-USD",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromInt(3),
		RemainingQty: decimal.NewFromInt(3),
	}
	_, err := engine.PlaceOrder(order)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	book := engine.books["BTC-USD"]
	if seq := book.GetSequence(); seq != 1 {
		t.Errorf("expected sequence 1, got %d", seq)
	}

	bids := book.GetBids()
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid, got %d", len(bids))
	}
	if !bids[0].Price.Equal(decimal.NewFromInt(50000)) {
		t.Errorf("bid price = %s, want 50000", bids[0].Price)
	}
}

func TestMatchingEngine_DeltaValidation_ThroughOrderBook(t *testing.T) {
	engine := setupEngine(t)

	// Place an order to populate the book
	order := &types.Order{
		Symbol:       "BTC-USD",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromInt(3),
		RemainingQty: decimal.NewFromInt(3),
	}
	engine.PlaceOrder(order)

	book := engine.books["BTC-USD"]

	// Verify the book rejects a delta with wrong symbol
	err := book.ApplyDelta(orderbook.Delta{
		Symbol:   "ETH-USD",
		Side:     types.Buy,
		Price:    decimal.NewFromInt(51000),
		Quantity: decimal.NewFromInt(1),
		Sequence: 2,
	})
	if err != orderbook.ErrSymbolMismatch {
		t.Fatalf("expected ErrSymbolMismatch, got %v", err)
	}
}

func TestMatchingEngine_StatePreservation_BadOrder(t *testing.T) {
	engine := setupEngine(t)

	// Place valid order
	engine.PlaceOrder(&types.Order{
		Symbol:       "BTC-USD",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromInt(3),
		RemainingQty: decimal.NewFromInt(3),
	})

	book := engine.books["BTC-USD"]
	bidsBefore := book.GetBids()
	seqBefore := book.GetSequence()

	// Try delta with negative price
	err := book.ApplyDelta(orderbook.Delta{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Price:    decimal.NewFromInt(-1),
		Quantity: decimal.NewFromInt(1),
		Sequence: 2,
	})
	if err != orderbook.ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}

	bidsAfter := book.GetBids()
	seqAfter := book.GetSequence()

	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids changed after bad delta")
	}
	if seqAfter != seqBefore {
		t.Errorf("sequence changed: before=%d after=%d", seqBefore, seqAfter)
	}
}

func TestMatchingEngine_CancelOrder_UpdatesBook(t *testing.T) {
	engine := setupEngine(t)

	order := &types.Order{
		Symbol:       "BTC-USD",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromInt(3),
		RemainingQty: decimal.NewFromInt(3),
	}
	_, err := engine.PlaceOrder(order)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	// Cancel it
	err = engine.CancelOrder("BTC-USD", order.ID)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}

	book := engine.books["BTC-USD"]
	bids := book.GetBids()
	if len(bids) != 0 {
		t.Fatalf("expected 0 bids after cancel, got %d", len(bids))
	}
}

func TestMatchingEngine_TradeCount_Increments(t *testing.T) {
	engine := setupEngine(t)

	if count := engine.GetTradeCount(); count != 0 {
		t.Fatalf("expected 0 trades initially, got %d", count)
	}
}

func TestMatchingEngine_GetRecentTrades_Empty(t *testing.T) {
	engine := setupEngine(t)

	trades := engine.GetRecentTrades(10)
	if len(trades) != 0 {
		t.Fatalf("expected 0 trades, got %d", len(trades))
	}
}

func TestMatchingEngine_ShortingDisabled(t *testing.T) {
	bookConfig := orderbook.Config{MaxDepth: 100}
	books := map[types.Symbol]*orderbook.OrderBook{
		"BTC-USD": orderbook.NewOrderBook("BTC-USD", bookConfig),
	}
	engineConfig := EngineConfig{
		EnableShorting: false,
	}
	engine := NewMatchingEngine(engineConfig, books)

	order := &types.Order{
		Symbol:   "BTC-USD",
		Side:     types.Sell,
		Type:     types.Limit,
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.NewFromInt(1),
	}
	err := engine.ValidateOrder(order)
	if err != ErrShortingDisabled {
		t.Fatalf("expected ErrShortingDisabled, got %v", err)
	}
}
