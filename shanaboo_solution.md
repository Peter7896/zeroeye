 ```diff
--- a/market/orderbook/orderbook.go
+++ b/market/orderbook/orderbook.go
@@ -0,0 +1,185 @@
+package orderbook
+
+import (
+	"errors"
+	"fmt"
+	"sort"
+	"sync"
+)
+
+// ErrMalformedPayload indicates a delta has invalid price, quantity, side, or symbol.
+var ErrMalformedPayload = errors.New("malformed order book delta payload")
+
+// ErrStaleSequence indicates a delta with an out-of-order or stale sequence number.
+var ErrStaleSequence = errors.New("stale or out-of-order sequence")
+
+// ErrChecksumMismatch indicates a checksum validation failure.
+var ErrChecksumMismatch = errors.New("checksum mismatch")
+
+// Side represents bid or ask.
+type Side int
+
+const (
+	Bid Side = iota
+	Ask
+)
+
+// Level represents a single price level.
+type Level struct {
+	Price    float64
+	Quantity float64
+}
+
+// Book represents the order book state.
+type Book struct {
+	mu sync.RWMutex
+
+	Symbol   string
+	Bids     []Level
+	Asks     []Level
+	Sequence int64
+}
+
+// Delta represents a single order book delta update.
+type Delta struct {
+	Symbol   string
+	Sequence int64
+	Side     Side
+	Price    float64
+	Quantity float64 // zero means remove
+}
+
+// Validate checks if the delta is well-formed.
+func (d *Delta) Validate() error {
+	if d.Symbol == "" {
+		return fmt.Errorf("%w: empty symbol", ErrMalformedPayload)
+	}
+	if d.Price < 0 {
+		return fmt.Errorf("%w: negative price %v", ErrMalformedPayload, d.Price)
+	}
+	if d.Quantity < 0 {
+		return fmt.Errorf("%w: negative quantity %v", ErrMalformedPayload, d.Quantity)
+	}
+	if d.Side != Bid && d.Side != Ask {
+		return fmt.Errorf("%w: invalid side %v", ErrMalformedPayload, d.Side)
+	}
+	return nil
+}
+
+// Snapshot represents a full order book snapshot.
+type Snapshot struct {
+	Symbol   string
+	Sequence int64
+	Bids     []Level
+	Asks     []Level
+ eccentric int64 // checksum placeholder
+}
+
+// NewBook creates a new empty book for a symbol.
+func NewBook(symbol string) *Book {
+	return &Book{
+		Symbol: symbol,
+		Bids:   make([]Level, 0),
+		Asks:   make([]Level, 0),
+	}
+}
+
+// ApplySnapshot replaces the book state with a snapshot.
+func (b *Book) ApplySnapshot(s Snapshot) {
+	b.mu.Lock()
+	defer b.mu.Unlock()
+	b.Sequence = s.Sequence
+	b.Bids = make([]Level, len(s.Bids))
+	b.Asks = make([]Level, len(s.Asks))
+	copy(b.Bids, s.Bids)
+	copy(b.Asks, s.Asks)
+}
+
+// ApplyDelta applies a delta to the book. Returns error without mutating on invalid input.
+func (b *Book) ApplyDelta(d Delta) error {
+	b.mu.Lock()
+	defer b.mu.Unlock()
+
+	if err := d.Validate(); err != nil {
+		return err
+	}
+
+	if d.Sequence <= b.Sequence {
+		return fmt.Errorf("%w: delta seq %d <= book seq %d", ErrStaleSequence, d.Sequence, b.Sequence)
+	}
+
+	levels := b.Asks
+	if d.Side == Bid {
+		levels = b.Bids
+	}
+
+	// Find and update or remove level
+	found := false
+	for i := range levels {
+		if levels[i].Price == d.Price {
+			if d.Quantity == 0 {
+				levels = append(levels[:i], levels[i+1:]...)
+			} else {
+				levels[i].Quantity = d.Quantity
+			}
+			found = true
+			break
+		}
+	}
+
+	if !found && d.Quantity > 0 {
+		levels = append(levels, Level{Price: d.Price, Quantity: d.Quantity})
+	}
+
+	// Sort levels: bids descending, asks ascending
+	if d.Side == Bid {
 Consult the user with the following message:
+
+	// Update the correct slice
+	if d.Side == Bid {
+		b.Bids = levels
+	} else {
+		b.Asks = levels
+	}
+
+	b.Sequence = d.Sequence
+	return nil
+}
+
+// GetState returns a copy of the current book state.
+func (b *Book) GetState() Book {
+	b.mu.RLock()
+	defer b.mu.RUnlock()
+	return Book{
+		Symbol:   b.Symbol,
+		Bids:     append([]Level(nil), b.Bids...),
+		Asks:     append([]Level(nil), b.Asks...),
+		Sequence: b.Sequence,
+	}
+}
+
+// sortLevels sorts price levels in place.
+func sortLevels(levels []Level, side Side) {
+	if side == Bid {
+		sort.Slice(levels, func(i, j int) bool {
+			return levels[i].Price > levels[j].Price
+		})
+	} else {
+		sort.Slice(levels, func(i, j int) bool {
+			return levels[i].Price < levels[j].Price
+		})
+	}
+}
+
+// applyDeltaSorted applies a delta with proper sorting.
+func (b *Book) applyDeltaSorted(d Delta) error {
+	b.mu.Lock()
+	defer b.mu.Unlock()
+
+	if err := d.Validate(); err != nil {
+		return err
+	}
+
+	if d.Sequence <= b.Sequence {
+		return fmt.Errorf("%w: delta seq %d <= book seq %d", ErrStaleSequence, d.Sequence, b.Sequence)
+	}
+
+	levels := b.Asks
+	if d.Side == Bid {
+		levels = b.Bids
+	}
+
+	found := false
+	for i := range levels {
+		if levels[i].Price == d.Price {
+			if d.Quantity == 0 {
+				levels = append(levels[:i], levels[i+1:]...)
+			} else {
+				levels[i].Quantity = d.Quantity
+			}
+			found = true
+			break
+		}
+	}
+
+	if !found && d.Quantity > 0 {
+		levels = append(levels, Level{Price: