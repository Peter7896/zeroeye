package orderbook

import (
	"fmt"
	"sort"
	"strings"
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

type DeltaSide string

const (
	DeltaBid DeltaSide = "bid"
	DeltaAsk DeltaSide = "ask"
)

type Delta struct {
	Symbol   types.Symbol
	Sequence uint64
	Side     DeltaSide
	Price    decimal.Decimal
	Quantity decimal.Decimal
	Checksum string
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

func (ob *OrderBook) Sequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.sequence
}

func (ob *OrderBook) ApplySnapshot(snapshot *types.DepthUpdate, sequence uint64) error {
	if snapshot == nil {
		return ErrInvalidSnapshot
	}
	if snapshot.Symbol != ob.symbol {
		return ErrInvalidSymbol
	}

	bids := make([]*types.Level, 0, len(snapshot.Bids))
	for _, level := range snapshot.Bids {
		if err := validateLevel(level.Price, level.Quantity); err != nil {
			return err
		}
		copy := level
		bids = append(bids, &copy)
	}

	asks := make([]*types.Level, 0, len(snapshot.Asks))
	for _, level := range snapshot.Asks {
		if err := validateLevel(level.Price, level.Quantity); err != nil {
			return err
		}
		copy := level
		asks = append(asks, &copy)
	}

	sortLevels(bids, true)
	sortLevels(asks, false)

	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	ob.bids = trimDepth(bids, ob.config.MaxDepth)
	ob.asks = trimDepth(asks, ob.config.MaxDepth)
	ob.sequence = sequence
	ob.updatedAt = time.Now()
	return nil
}

func (ob *OrderBook) ApplyDelta(delta Delta) error {
	if delta.Symbol != ob.symbol {
		return ErrInvalidSymbol
	}
	if delta.Sequence == 0 {
		return ErrInvalidSequence
	}
	if delta.Side != DeltaBid && delta.Side != DeltaAsk {
		return ErrInvalidSide
	}
	if delta.Price.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidPrice
	}
	if delta.Quantity.IsNegative() {
		return ErrInvalidQuantity
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}
	if delta.Sequence <= ob.sequence {
		return ErrStaleDelta
	}

	previousBids := cloneLevels(ob.bids)
	previousAsks := cloneLevels(ob.asks)
	previousSequence := ob.sequence

	if delta.Side == DeltaBid {
		ob.bids = applyLevelDelta(ob.bids, delta.Price, delta.Quantity, true, ob.config.MaxDepth)
	} else {
		ob.asks = applyLevelDelta(ob.asks, delta.Price, delta.Quantity, false, ob.config.MaxDepth)
	}
	ob.sequence = delta.Sequence

	if delta.Checksum != "" && delta.Checksum != checksumForLevels(ob.bids, ob.asks) {
		ob.bids = previousBids
		ob.asks = previousAsks
		ob.sequence = previousSequence
		return ErrChecksumMismatch
	}

	ob.updatedAt = time.Now()
	return nil
}

func (ob *OrderBook) Checksum() string {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return checksumForLevels(ob.bids, ob.asks)
}

func checksumForLevels(bids []*types.Level, asks []*types.Level) string {
	var builder strings.Builder
	writeLevels := func(prefix string, levels []*types.Level) {
		for _, level := range levels {
			if level == nil {
				continue
			}
			fmt.Fprintf(
				&builder,
				"%s:%s:%s;",
				prefix,
				level.Price.String(),
				level.Quantity.String(),
			)
		}
	}
	writeLevels("b", bids)
	writeLevels("a", asks)
	return builder.String()
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
	ErrBookClosed       = &BookError{"order book is closed"}
	ErrOrderNotFound    = &BookError{"order not found"}
	ErrInvalidSnapshot  = &BookError{"invalid snapshot"}
	ErrInvalidSymbol    = &BookError{"invalid symbol"}
	ErrInvalidSequence  = &BookError{"invalid sequence"}
	ErrInvalidSide      = &BookError{"invalid side"}
	ErrInvalidPrice     = &BookError{"invalid price"}
	ErrInvalidQuantity  = &BookError{"invalid quantity"}
	ErrStaleDelta       = &BookError{"stale or out-of-order delta"}
	ErrChecksumMismatch = &BookError{"delta checksum mismatch"}
)

type BookError struct {
	message string
}

func (e *BookError) Error() string {
	return e.message
}

func insertLevel(levels []*types.Level, level *types.Level, desc bool) []*types.Level {
	levels = append(levels, level)
	sortLevels(levels, desc)
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

func validateLevel(price decimal.Decimal, quantity decimal.Decimal) error {
	if price.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidPrice
	}
	if quantity.IsNegative() {
		return ErrInvalidQuantity
	}
	return nil
}

func cloneLevels(levels []*types.Level) []*types.Level {
	result := make([]*types.Level, 0, len(levels))
	for _, level := range levels {
		if level == nil {
			continue
		}
		copy := *level
		result = append(result, &copy)
	}
	return result
}

func applyLevelDelta(levels []*types.Level, price decimal.Decimal, quantity decimal.Decimal, desc bool, maxDepth int) []*types.Level {
	for i, level := range levels {
		if level.Price.Equal(price) {
			if quantity.IsZero() {
				return append(levels[:i], levels[i+1:]...)
			}
			level.Quantity = quantity
			sortLevels(levels, desc)
			return trimDepth(levels, maxDepth)
		}
	}

	if quantity.IsZero() {
		return levels
	}

	levels = append(levels, &types.Level{
		Price:    price,
		Quantity: quantity,
		Count:    1,
	})
	sortLevels(levels, desc)
	return trimDepth(levels, maxDepth)
}

func sortLevels(levels []*types.Level, desc bool) {
	sort.Slice(levels, func(i, j int) bool {
		if desc {
			return levels[i].Price.GreaterThan(levels[j].Price)
		}
		return levels[i].Price.LessThan(levels[j].Price)
	})
}

func trimDepth(levels []*types.Level, maxDepth int) []*types.Level {
	if maxDepth <= 0 || len(levels) <= maxDepth {
		return levels
	}
	return levels[:maxDepth]
}
