package orderbook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

var (
	ErrInvalidDeltaSymbol   = errors.New("invalid delta symbol")
	ErrInvalidDeltaSequence = errors.New("stale or out-of-order delta sequence")
	ErrInvalidDeltaPrice    = errors.New("invalid delta price")
	ErrInvalidDeltaQuantity = errors.New("invalid delta quantity")
	ErrInvalidDeltaChecksum = errors.New("invalid delta checksum")
)

type Delta struct {
	Symbol   types.Symbol  `json:"symbol"`
	Sequence uint64        `json:"sequence"`
	Bids     []types.Level `json:"bids"`
	Asks     []types.Level `json:"asks"`
	Checksum string        `json:"checksum,omitempty"`
}

func (ob *OrderBook) Sequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.sequence
}

func (ob *OrderBook) ReplaceSnapshot(snapshot *types.DepthUpdate, sequence uint64) error {
	if snapshot == nil || snapshot.Symbol == "" || snapshot.Symbol != ob.symbol {
		return ErrInvalidDeltaSymbol
	}
	if err := validateLevels(snapshot.Bids, "bid"); err != nil {
		return err
	}
	if err := validateLevels(snapshot.Asks, "ask"); err != nil {
		return err
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()
	if ob.closed {
		return ErrBookClosed
	}
	ob.bids = cloneLevelPointers(snapshot.Bids)
	ob.asks = cloneLevelPointers(snapshot.Asks)
	sortLevels(ob.bids, true)
	sortLevels(ob.asks, false)
	ob.sequence = sequence
	ob.updatedAt = time.Now()
	return nil
}

func (ob *OrderBook) ApplyDelta(delta Delta) error {
	if delta.Symbol == "" || delta.Symbol != ob.symbol {
		return ErrInvalidDeltaSymbol
	}
	if err := validateLevels(delta.Bids, "bid"); err != nil {
		return err
	}
	if err := validateLevels(delta.Asks, "ask"); err != nil {
		return err
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()
	if ob.closed {
		return ErrBookClosed
	}
	if delta.Sequence <= ob.sequence {
		return ErrInvalidDeltaSequence
	}

	nextBids := cloneLevelPointersFromPointers(ob.bids)
	nextAsks := cloneLevelPointersFromPointers(ob.asks)
	nextBids = applyLevels(nextBids, delta.Bids, true)
	nextAsks = applyLevels(nextAsks, delta.Asks, false)

	if delta.Checksum != "" {
		got := ComputeChecksum(nextBids, nextAsks)
		if !strings.EqualFold(delta.Checksum, got) {
			return fmt.Errorf("%w: expected %s got %s", ErrInvalidDeltaChecksum, delta.Checksum, got)
		}
	}

	ob.bids = nextBids
	ob.asks = nextAsks
	ob.sequence = delta.Sequence
	ob.updatedAt = time.Now()
	return nil
}

func ComputeChecksum(bids []*types.Level, asks []*types.Level) string {
	h := sha256.New()
	writeLevels := func(side string, levels []*types.Level) {
		for _, level := range levels {
			if level == nil {
				continue
			}
			fmt.Fprintf(h, "%s:%s:%s:%d;", side, level.Price.String(), level.Quantity.String(), level.Count)
		}
	}
	writeLevels("bid", bids)
	writeLevels("ask", asks)
	return hex.EncodeToString(h.Sum(nil))
}

func validateLevels(levels []types.Level, side string) error {
	for i, level := range levels {
		if level.Price.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("%w: %s[%d]", ErrInvalidDeltaPrice, side, i)
		}
		if level.Quantity.LessThan(decimal.Zero) {
			return fmt.Errorf("%w: %s[%d]", ErrInvalidDeltaQuantity, side, i)
		}
	}
	return nil
}

func applyLevels(current []*types.Level, updates []types.Level, desc bool) []*types.Level {
	for _, update := range updates {
		current = removeLevel(current, update.Price)
		if update.Quantity.GreaterThan(decimal.Zero) {
			level := update
			current = append(current, &level)
		}
	}
	sortLevels(current, desc)
	return current
}

func cloneLevelPointers(levels []types.Level) []*types.Level {
	result := make([]*types.Level, 0, len(levels))
	for _, level := range levels {
		copy := level
		result = append(result, &copy)
	}
	return result
}

func cloneLevelPointersFromPointers(levels []*types.Level) []*types.Level {
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
