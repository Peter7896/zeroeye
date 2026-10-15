package orderbook

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

type UpdateSide string

const (
	SideBid UpdateSide = "bid"
	SideAsk UpdateSide = "ask"
)

type LevelUpdate struct {
	Side     UpdateSide
	Price    decimal.Decimal
	Quantity decimal.Decimal
}

type SnapshotUpdate struct {
	Symbol   types.Symbol
	Sequence uint64
	Bids     []types.Level
	Asks     []types.Level
	Checksum string
}

type DeltaUpdate struct {
	Symbol           types.Symbol
	PreviousSequence uint64
	Sequence         uint64
	Levels           []LevelUpdate
	Checksum         string
}

type DeltaValidationError struct {
	Field   string
	Message string
}

func (e *DeltaValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (ob *OrderBook) Sequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.sequence
}

func (ob *OrderBook) ApplySnapshot(snapshot SnapshotUpdate) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}
	if err := validateSymbol(snapshot.Symbol, ob.symbol); err != nil {
		return err
	}
	if snapshot.Sequence == 0 {
		return fieldError("sequence", "snapshot sequence must be greater than zero")
	}

	bids, err := normalizeLevels(snapshot.Bids, "bids", true, false, ob.config.MaxDepth)
	if err != nil {
		return err
	}
	asks, err := normalizeLevels(snapshot.Asks, "asks", false, false, ob.config.MaxDepth)
	if err != nil {
		return err
	}
	if snapshot.Checksum != "" {
		expected := ComputeBookChecksum(snapshot.Symbol, snapshot.Sequence, bids, asks)
		if !strings.EqualFold(strings.TrimSpace(snapshot.Checksum), expected) {
			return fieldError("checksum", "snapshot checksum mismatch")
		}
	}

	ob.bids = levelsToPointers(bids)
	ob.asks = levelsToPointers(asks)
	ob.sequence = snapshot.Sequence
	ob.updatedAt = time.Now()
	return nil
}

func (ob *OrderBook) ApplyDelta(delta DeltaUpdate) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}
	if err := validateSymbol(delta.Symbol, ob.symbol); err != nil {
		return err
	}
	if delta.Sequence <= ob.sequence {
		return fieldError("sequence", "stale delta sequence")
	}
	if delta.PreviousSequence != ob.sequence {
		return fieldError("previous_sequence", "out-of-order delta sequence")
	}
	if len(delta.Levels) == 0 {
		return fieldError("levels", "at least one level update is required")
	}

	bids := clonePointerLevels(ob.bids)
	asks := clonePointerLevels(ob.asks)

	for i, level := range delta.Levels {
		field := fmt.Sprintf("levels[%d]", i)
		if err := validateLevelUpdate(level, field); err != nil {
			return err
		}
		switch level.Side {
		case SideBid:
			bids = applyLevelUpdate(bids, level, true, ob.config.MaxDepth)
		case SideAsk:
			asks = applyLevelUpdate(asks, level, false, ob.config.MaxDepth)
		default:
			return fieldError(field+".side", "side must be bid or ask")
		}
	}

	if delta.Checksum != "" {
		expected := ComputeBookChecksum(delta.Symbol, delta.Sequence, bids, asks)
		if !strings.EqualFold(strings.TrimSpace(delta.Checksum), expected) {
			return fieldError("checksum", "delta checksum mismatch")
		}
	}

	ob.bids = levelsToPointers(bids)
	ob.asks = levelsToPointers(asks)
	ob.sequence = delta.Sequence
	ob.updatedAt = time.Now()
	return nil
}

func ComputeBookChecksum(symbol types.Symbol, sequence uint64, bids, asks []types.Level) string {
	hasher := sha256.New()
	fmt.Fprintf(hasher, "%s|%d", symbol, sequence)
	writeLevelsForChecksum(hasher, "B", bids)
	writeLevelsForChecksum(hasher, "A", asks)
	return hex.EncodeToString(hasher.Sum(nil))
}

type checksumWriter interface {
	Write([]byte) (int, error)
}

func writeLevelsForChecksum(w checksumWriter, side string, levels []types.Level) {
	fmt.Fprintf(w, "|%s", side)
	for _, level := range levels {
		fmt.Fprintf(w, "|%s:%s:%d", level.Price.String(), level.Quantity.String(), level.Count)
	}
}

func validateSymbol(got, want types.Symbol) error {
	if strings.TrimSpace(string(got)) == "" {
		return fieldError("symbol", "symbol is required")
	}
	if got != want {
		return fieldError("symbol", fmt.Sprintf("symbol %q does not match book %q", got, want))
	}
	return nil
}

func validateLevelUpdate(level LevelUpdate, field string) error {
	if level.Side != SideBid && level.Side != SideAsk {
		return fieldError(field+".side", "side must be bid or ask")
	}
	if !level.Price.GreaterThan(decimal.Zero) {
		return fieldError(field+".price", "price must be positive")
	}
	if level.Quantity.LessThan(decimal.Zero) {
		return fieldError(field+".quantity", "quantity must be zero or positive")
	}
	return nil
}

func normalizeLevels(levels []types.Level, field string, desc bool, allowZero bool, maxDepth int) ([]types.Level, error) {
	result := make([]types.Level, 0, len(levels))
	for i, level := range levels {
		itemField := fmt.Sprintf("%s[%d]", field, i)
		if !level.Price.GreaterThan(decimal.Zero) {
			return nil, fieldError(itemField+".price", "price must be positive")
		}
		if allowZero {
			if level.Quantity.LessThan(decimal.Zero) {
				return nil, fieldError(itemField+".quantity", "quantity must be zero or positive")
			}
		} else if !level.Quantity.GreaterThan(decimal.Zero) {
			return nil, fieldError(itemField+".quantity", "quantity must be positive")
		}
		if level.Count <= 0 {
			level.Count = 1
		}
		result = append(result, level)
	}
	sortLevels(result, desc)
	if maxDepth > 0 && len(result) > maxDepth {
		result = result[:maxDepth]
	}
	return result, nil
}

func clonePointerLevels(levels []*types.Level) []types.Level {
	result := make([]types.Level, 0, len(levels))
	for _, level := range levels {
		if level != nil {
			result = append(result, *level)
		}
	}
	return result
}

func levelsToPointers(levels []types.Level) []*types.Level {
	result := make([]*types.Level, 0, len(levels))
	for i := range levels {
		level := levels[i]
		result = append(result, &level)
	}
	return result
}

func applyLevelUpdate(levels []types.Level, update LevelUpdate, desc bool, maxDepth int) []types.Level {
	for i, level := range levels {
		if level.Price.Equal(update.Price) {
			if update.Quantity.IsZero() {
				return append(levels[:i], levels[i+1:]...)
			}
			levels[i].Quantity = update.Quantity
			if levels[i].Count <= 0 {
				levels[i].Count = 1
			}
			sortLevels(levels, desc)
			return trimDepth(levels, maxDepth)
		}
	}

	if update.Quantity.IsZero() {
		return levels
	}

	levels = append(levels, types.Level{
		Price:    update.Price,
		Quantity: update.Quantity,
		Count:    1,
	})
	sortLevels(levels, desc)
	return trimDepth(levels, maxDepth)
}

func sortLevels(levels []types.Level, desc bool) {
	sort.SliceStable(levels, func(i, j int) bool {
		if desc {
			return levels[i].Price.GreaterThan(levels[j].Price)
		}
		return levels[i].Price.LessThan(levels[j].Price)
	})
}

func trimDepth(levels []types.Level, maxDepth int) []types.Level {
	if maxDepth > 0 && len(levels) > maxDepth {
		return levels[:maxDepth]
	}
	return levels
}

func fieldError(field, message string) error {
	return &DeltaValidationError{Field: field, Message: message}
}
