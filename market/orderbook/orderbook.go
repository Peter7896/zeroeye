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

var (
	ErrBookClosed    = &BookError{"order book is closed"}
	ErrOrderNotFound = &BookError{"order not found"}
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
package orderbook

import (
	"errors"
	"fmt"
	"sync"
)

// Side represents the side of an order.
type Side int

const (
	Bid Side = iota
	Ask
)

func (s Side) String() string {
	switch s {
	case Bid:
		return "bid"
	case Ask:
		return "ask"
	default:
		return "unknown"
	}
}

// ParseSide parses a side string.
func ParseSide(s string) (Side, error) {
	switch s {
	case "bid", "BID", "buy", "BUY":
		return Bid, nil
	case "ask", "ASK", "sell", "SELL":
		return Ask, nil
	default:
		return -1, fmt.Errorf("invalid side: %s", s)
	}
}

// Level represents a single price level in the order book.
type Level struct {
	Price    float64
	Quantity float64
}

// OrderBook represents the current state of the order book.
type OrderBook struct {
	mu        sync.RWMutex
	Symbol    string
	Bids      map[float64]Level
	Asks      map[float64]Level
	Sequence  uint64
}

// NewOrderBook creates a new OrderBook.
func NewOrderBook(symbol string) *OrderBook {
	return &OrderBook{
		Symbol: symbol,
		Bids:   make(map[float64]Level),
		Asks:   make(map[float64]Level),
	}
}

// Snapshot returns a deep copy of the current book state.
32 func (ob *OrderBook) Snapshot() *OrderBook {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	snap := NewOrderBook(ob.Symbol)
	snap.Sequence = ob.Sequence
	for k, v := range ob.Bids {
		snap.Bids[k] = v
	}
	for k, v := range ob.Asks {
		snap.Asks[k] = v
	}
	return snap
}

// ApplyDelta applies a delta to the order book.
func (ob *OrderBook) ApplyDelta(delta Delta) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if delta.Sequence <= ob.Sequence {
		return fmt.Errorf("stale sequence: got %d, current %d", delta.Sequence, ob.Sequence)
	}

	switch delta.Side {
	case Bid:
		if delta.Quantity == 0 {
			delete(ob.Bids, delta.Price)
		} else {
			ob.Bids[delta.Price] = Level{Price: delta.Price, Quantity: delta.Quantity}
		}
	case Ask:
		if delta.Quantity == 0 {
			delete(ob.Asks, delta.Price)
		} else {
			ob.Asks[delta.Price] = Level{Price: delta.Price, Quantity: delta.Quantity}
		}
	default:
		return fmt.Errorf("invalid side: %v", delta.Side)
	}

	ob.Sequence = delta.Sequence
	return nil
}

// GetSequence returns the current sequence number.
func (ob *OrderBook) GetSequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.Sequence
}

// Delta represents an order book delta update.
type Delta struct {
	Symbol   string
	Side     Side
	Price    float64
	Quantity float64
	Sequence uint64
}

// Validate checks if the delta is well-formed.
func (d Delta) Validate() error {
	if d.Symbol == "" {
		return errors.New("symbol cannot be empty")
	}
	if d.Price < 0 {
		return fmt.Errorf("price cannot be negative: %f", d.Price)
	}
	if d.Quantity < 0 {
		return fmt.Errorf("quantity cannot be negative: %f", d.Quantity)
	}
	if d.Side != Bid && d.Side != Ask {
		return fmt.Errorf("invalid side: %v", d.Side)
	}
	return nil
}
}
