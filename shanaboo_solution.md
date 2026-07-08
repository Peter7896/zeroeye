 ```diff
--- a/market/orderbook/orderbook.go
+++ b/market/orderbook/orderbook.go
@@ -0,0 +1,247 @@
+package orderbook
+
+import (
+	"errors"
+	"fmt"
+	"sort"
+	"sync"
+)
+
+// ErrMalformedPayload indicates a delta with invalid price, quantity, side, or symbol.
+var ErrMalformedPayload = errors.New("malformed order book delta payload")
+
+// ErrStaleSequence indicates a delta with a sequence number that is not greater than the current sequence.
+var ErrStaleSequence = errors.New("stale or out-of-order sequence update")
+
+// ErrChecksumMismatch indicates a checksum validation failure.
+var ErrChecksumMismatch = errors.New("checksum mismatch")
+
+// Side represents bid or ask.
+type Side int
+
+const (
+	SideBid Side = iota
+	SideAsk
+)
+
+// Level represents a single price level in the order book.
+type Level struct {
+	Price    float64
+	Quantity float64
+}
+
+// BookSnapshot represents a full snapshot of the order book.
+type BookSnapshot struct {
+	Symbol   string
+	Sequence int64
+	Bids     []Level
+	Asks     []Level
+}
+
+// Delta represents a single update to the order book.
+type Delta struct {
+	Symbol   string
+	Sequence int64
+	Side     Side
+	Price    float64
+	Quantity float64
+}
+
+// OrderBook maintains the current state of an order book for a symbol.
+type OrderBook struct {
+	mu       sync.RWMutex
+	symbol   string
+	sequence int64
+	bids     map[float64]float64 // price -> quantity
+	asks     map[float64]float64 // price -> quantity
+	ready    bool
+}
+
+// NewOrderBook creates a new empty OrderBook.
+func NewOrderBook() *OrderBook {
+	return &OrderBook{
+		bids: make(map[float64]float64),
+		asks: make(map[float64]float64),
+	}
+}
+
+// Symbol returns the book's symbol.
+func (ob *OrderBook) Symbol() string {
+	ob.mu.RLock()
+	defer ob.mu.RUnlock()
+	return ob.symbol
+}
+
+// Sequence returns the current sequence number.
+func (ob *OrderBook) Sequence() int64 {
+	ob.mu.RLock()
+	defer ob.mu.RUnlock()
+	return ob.sequence
+}
+
+// IsReady returns true if the book has received a snapshot.
+func (ob *OrderBook) IsReady() bool {
+	ob.mu.RLock()
+	defer ob.mu.RUnlock()
+	return ob.ready
+}
+
+// Bids returns a copy of the current bids sorted by price descending.
+func (ob *OrderBook) Bids() []Level {
+	ob.mu.RLock()
+	defer ob.mu.RUnlock()
+	return sortedLevels(ob.bids, true)
+}
+
+// Asks returns a copy of the current asks sorted by price ascending.
+func (ob *OrderBook) Asks() []Level {
+	ob.mu.RLock()
+	defer ob.mu.RUnlock()
+	return sortedLevels(ob.asks, false)
+}
+
+// ApplySnapshot applies a full snapshot to the book.
+func (ob *OrderBook) ApplySnapshot(snap BookSnapshot) error {
+	if snap.Symbol == "" {
+		return fmt.Errorf("%w: empty symbol", ErrMalformedPayload)
+	}
+	if snap.Sequence < 0 {
+		return fmt.Errorf("%w: negative sequence", ErrMalformedPayload)
+	}
+	ob.mu.Lock()
+	defer ob.mu.Unlock()
+	ob.symbol = snap.Symbol
+	ob.sequence = snap.Sequence
+	ob.bids = make(map[float64]float64, len(snap.Bids))
+	ob.asks = make(map[float64]float64, len(snap.Asks))
+	for _, b := range snap.Bids {
+		ob.bids[b.Price] = b.Quantity
+	}
+	for _, a := range snap.Asks {
+		ob.asks[a.Price] = a.Quantity
+	}
+	ob.ready = true
+	return nil
+}
+
+// ApplyDelta applies a delta to the book. Returns an non-nil error if the delta is invalid,
+// in which case the book state is preserved.
+func (ob *OrderBook) ApplyDelta(delta Delta) error {
+	if err := ob.validateDelta(delta); err != nil {
+		return err
+	}
+	ob.mu.Lock()
+	defer ob.mu.Unlock()
+	if !ob.ready {
+		return fmt.Errorf("book not ready")
+	}
+	if delta.Symbol != ob.symbol {
+		return fmt.Errorf("%w: symbol mismatch expected %s got %s", ErrMalformedPayload, ob.symbol, delta.Symbol)
+	}
+	if delta.Sequence <= ob.sequence {
+		return fmt.Errorf("%w: expected > %d got %d", ErrStaleSequence, ob.sequence, delta.Sequence)
+	}
+
+	target := ob.asks
+	if delta.Side == SideBid {
+		target = ob.bids
+	}
+	if delta.Quantity == 0 {
+		delete(target, delta.Price)
+	} else {
+		target[delta.Price] = delta.Quantity
+	}
+	ob.sequence = delta.Sequence
+	return nil
+}
+
+func (ob *OrderBook) validateDelta(delta Delta) error {
+	if delta.Symbol == "" {
+		return fmt.Errorf("%w: empty symbol", ErrMalformedPayload)
+	}
+	if delta.Sequence < 0 {
+		return fmt.Errorf("%w: negative sequence", ErrMalformedPayload)
+	}
+	if delta.Price <= 0 {
+		return fmt.Errorf("%w: invalid price %f", ErrMalformedPayload, delta.Price)
+	}
+	if delta.Quantity < 0 {
+		return fmt.Errorf("%w: negative quantity %f", ErrMalformedPayload, delta.Quantity)
+	}
+	if delta.Side != SideBid && delta.Side != SideAsk {
+		return fmt.Errorf("%w: invalid side %d", ErrMalformedPayload, delta.Side)
+	}
+	return nil
+}
+
+func sortedLevels(m map[float64]float64, desc bool) []Level {
+	levels := make([]Level, 0, len(m))
+	for p, q := range m {
+		levels = append(levels, Level{Price: p, Quantity: q})
+	}
+	sort.Slice(levels, func(i, j int) bool {
+		if desc {
+			return levels[i].Price > levels[j].Price
+		}
+		return levels[i].Price < levels[j].Price
+	})
+	return levels
+}
--- a/market/orderbook/orderbook_test