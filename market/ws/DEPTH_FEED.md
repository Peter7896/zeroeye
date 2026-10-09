# Depth helper contract

`ParseDepthDelta` decodes the existing `depth_delta` / `depth_snapshot` envelope
into `types.DepthUpdate`. Every level requires an explicit, non-null quantity.
Numeric and string decimals are supported. Positive quantity upserts a level;
explicit `0` / `"0"` deletes a level when applied as a delta. Missing/null quantity
is rejected before conversion or book mutation. Price must be positive.

The legacy depth payload has no sequence or checksum fields. `ToDelta` converts
one bid or ask into an `orderbook.Delta` and leaves `Sequence` and `Checksum`
unset. The feed adapter must obtain the sequence from its authoritative transport
metadata and assign it before `ApplyDelta`; arrival order is not a substitute.
A sequence of zero is invalid, stale/replayed values are rejected, and a gap
requires a fresh snapshot. Snapshot sequence also comes from that adapter.

If the feed provides a compatible post-update checksum, the adapter assigns it
to `Delta.Checksum`. An empty checksum disables that optional check. The current
book checksum is lowercase unpadded hexadecimal FNV-1a 64 over canonical decimal
`price:quantity;` rows, lexically sorted within bids and within asks, with `|`
after each side. It describes the post-update, MaxDepth-trimmed book and is an
integrity check, not a cryptographic guarantee. Exchange-specific checksum
formats require their own adapter; this helper does not infer or silently convert
them.

These are additive helpers; they do not change the server's subscription or
message handlers. Callers route snapshots to `ApplySnapshot` and single-level
deltas through `ToDelta` / `ApplyDelta`. `ToDelta` rejects multiple levels or both
nonempty sides, and its existing empty-frame flag does not recover the envelope
event type.

The regression tests exercise the real parse/conversion/book path, covering
missing/null quantities on both sides, legal updates and zero deletions, missing,
stale and gapped sequences, and a checksum mismatch with unchanged book state.
The quantity-presence design follows the protection already present in PR #29;
this focused repair preserves natemiller23's PR #30 implementation and history.

Local package tests do not validate encrypted diagnostics. The existing PR #30
diagnostic artifacts refer to its preceding code commit and cannot validate this
repair. A current-source diagnostic run and review of failed modules remain
separate acceptance requirements.
