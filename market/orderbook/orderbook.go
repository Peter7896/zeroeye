package orderbook

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

type Config struct {
	MaxDepth       int
	PriceDecimals  int32
	VolumeDecimals int32
}

type OrderBook struct {
	mu        sync.RWMutex
	symbol    types.Symbol
	config    Config
	bids      []*types.Level
	asks      []*types.Level
	orders    map[string]*types.Order
	sequence  uint64
	updatedAt time.Time
	closed    bool
}

func NewOrderBook(symbol types.Symbol, config Config) *OrderBook {
	return &OrderBook{
		symbol:   symbol,
		config:   config,
		bids:     make([]*types.Level, 0, config.MaxDepth),
		asks:     make([]*types.Level, 0, config.MaxDepth),
		orders:   make(map[string]*types.Order),
		sequence: 0,
	}
}

func (ob *OrderBook) AddOrder(order *types.Order) ([]*types.Trade, error) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return nil, ErrBookClosed
	}

	if order.ID == "" {
		order.ID = uuid.New().String()
	}

	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()
	order.Status = types.New

	ob.orders[order.ID] = order
	ob.sequence++

	level := &types.Level{
		Price:    order.Price,
		Quantity: order.RemainingQty,
		Count:    1,
	}

	if order.Side == types.Buy {
		ob.bids = insertLevel(ob.bids, level, true)
	} else {
		ob.asks = insertLevel(ob.asks, level, false)
	}

	ob.updatedAt = time.Now()
	return nil, nil
}

func (ob *OrderBook) CancelOrder(orderID string) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	order, exists := ob.orders[orderID]
	if !exists {
		return ErrOrderNotFound
	}

	order.Status = types.Cancelled
	order.UpdatedAt = time.Now()
	delete(ob.orders, orderID)

	if order.Side == types.Buy {
		ob.bids = removeLevel(ob.bids, order.Price)
	} else {
		ob.asks = removeLevel(ob.asks, order.Price)
	}

	ob.updatedAt = time.Now()
	return nil
}

func (ob *OrderBook) GetBids() []*types.Level {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	result := make([]*types.Level, len(ob.bids))
	copy(result, ob.bids)
	return result
}

func (ob *OrderBook) GetAsks() []*types.Level {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	result := make([]*types.Level, len(ob.asks))
	copy(result, ob.asks)
	return result
}

func (ob *OrderBook) GetSnapshot() *types.DepthUpdate {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	bids := make([]types.Level, len(ob.bids))
	for i, l := range ob.bids {
		if l != nil {
			bids[i] = *l
		}
	}

	asks := make([]types.Level, len(ob.asks))
	for i, l := range ob.asks {
		if l != nil {
			asks[i] = *l
		}
	}

	return &types.DepthUpdate{
		Symbol:    ob.symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now().UnixMilli(),
	}
}

func (ob *OrderBook) Close() {
	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.closed = true
	ob.bids = nil
	ob.asks = nil
	ob.orders = nil
}

// Delta represents a single-side order book level change pushed via WebSocket.
type Delta struct {
	Symbol   types.Symbol    `json:"symbol"`
	Side     types.OrderSide `json:"side"`
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"quantity"`
	Sequence uint64          `json:"sequence"`
}

// ValidateDelta checks a delta payload for structural correctness before it is applied.
// It does NOT mutate the order book.
func ValidateDelta(delta Delta, bookSymbol types.Symbol, lastSequence uint64) error {
	if delta.Symbol != bookSymbol {
		return ErrSymbolMismatch
	}
	if delta.Side != types.Buy && delta.Side != types.Sell {
		return ErrInvalidSide
	}
	if !delta.Price.IsPositive() {
		return ErrInvalidPrice
	}
	if delta.Quantity.IsNegative() {
		return ErrInvalidQuantity
	}
	if delta.Sequence <= lastSequence {
		return ErrStaleSequence
	}
	return nil
}

// ApplyDelta validates and then applies a single delta to the order book.
// The order book is NOT modified when an error is returned.
func (ob *OrderBook) ApplyDelta(delta Delta) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	if err := ValidateDelta(delta, ob.symbol, ob.sequence); err != nil {
		return err
	}

	level := &types.Level{
		Price:    delta.Price,
		Quantity: delta.Quantity,
		Count:    1,
	}

	if delta.Side == types.Buy {
		ob.bids = upsertLevel(ob.bids, level, true)
	} else {
		ob.asks = upsertLevel(ob.asks, level, false)
	}

	ob.sequence = delta.Sequence
	ob.updatedAt = time.Now()
	return nil
}

// ApplySnapshot replaces the full order book with the given depth snapshot.
func (ob *OrderBook) ApplySnapshot(snapshot *types.DepthUpdate) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	if snapshot.Symbol != ob.symbol {
		return ErrSymbolMismatch
	}

	for _, bid := range snapshot.Bids {
		if !bid.Price.IsPositive() {
			return ErrInvalidPrice
		}
	}
	for _, ask := range snapshot.Asks {
		if !ask.Price.IsPositive() {
			return ErrInvalidPrice
		}
	}

	ob.bids = make([]*types.Level, 0, len(snapshot.Bids))
	for i := range snapshot.Bids {
		l := snapshot.Bids[i]
		ob.bids = append(ob.bids, &types.Level{
			Price:    l.Price,
			Quantity: l.Quantity,
			Count:    l.Count,
		})
	}

	ob.asks = make([]*types.Level, 0, len(snapshot.Asks))
	for i := range snapshot.Asks {
		l := snapshot.Asks[i]
		ob.asks = append(ob.asks, &types.Level{
			Price:    l.Price,
			Quantity: l.Quantity,
			Count:    l.Count,
		})
	}

	ob.sequence = 0
	ob.updatedAt = time.Now()
	return nil
}

// GetSequence returns the current sequence number (for testing and diagnostics).
func (ob *OrderBook) GetSequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.sequence
}

// GetOrdersCount returns the number of orders in the book (for testing).
func (ob *OrderBook) GetOrdersCount() int {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.orders)
}

// upsertLevel inserts a level at the correct sorted position or updates quantity if the price already exists.
func upsertLevel(levels []*types.Level, level *types.Level, desc bool) []*types.Level {
	for _, existing := range levels {
		if existing.Price.Equal(level.Price) {
			if level.Quantity.IsZero() {
				return removeLevel(levels, level.Price)
			}
			existing.Quantity = level.Quantity
			existing.Count = level.Count
			return levels
		}
	}
	if level.Quantity.IsZero() {
		return levels
	}
	return insertLevel(levels, level, desc)
}

var (
	ErrBookClosed      = &BookError{"order book is closed"}
	ErrOrderNotFound   = &BookError{"order not found"}
	ErrSymbolMismatch  = &BookError{"symbol mismatch"}
	ErrInvalidSide     = &BookError{"invalid order side"}
	ErrStaleSequence   = &BookError{"stale or out-of-order sequence"}
	ErrInvalidPrice    = &BookError{"price must be positive"}
	ErrInvalidQuantity = &BookError{"quantity must not be negative"}
)

type BookError struct {
	message string
}

func (e *BookError) Error() string {
	return e.message
}

func insertLevel(levels []*types.Level, level *types.Level, desc bool) []*types.Level {
	levels = append(levels, level)
	sort.Slice(levels, func(i, j int) bool {
		if desc {
			return levels[i].Price.GreaterThan(levels[j].Price)
		}
		return levels[i].Price.LessThan(levels[j].Price)
	})
	return levels
}

func removeLevel(levels []*types.Level, price decimal.Decimal) []*types.Level {
	for i, l := range levels {
		if l.Price.Equal(price) {
			return append(levels[:i], levels[i+1:]...)
		}
	}
	return levels
}
