# ... (truncated) ...

# Function to validate WebSocket order book deltas

def validate_order_book_deltas(deltas):
    for delta in deltas:
        if not is_valid_delta(delta):
            raise ValueError(f"Malformed delta: {delta}")
        if is_stale(delta):
            continue  # Ignore stale deltas
        if is_out_of_order(delta):
            raise ValueError(f"Out-of-order delta: {delta}")
    return True

# Helper functions for validation

def is_valid_delta(delta):
    required_fields = ['price', 'quantity', 'side', 'symbol']
    return all(field in delta for field in required_fields) and delta['price'] > 0 and delta['quantity'] > 0

def is_stale(delta):
    # Implement logic to check if the delta is stale
    pass

def is_out_of_order(delta):
    # Implement logic to check if the delta is out of order
    pass

# ... (truncated) ...
                f"    {color('✓', Colors.GREEN)} {path.relative_to(ROOT)} created "
                f"({size_kb:.1f} KiB)"
            )
        if len(logd_files) > 1:
            print(
                f"    {color('✓', Colors.GREEN)} split oversized diagnostic log into "
                f"{len(logd_files)} chunks of at most {DIAGNOSTIC_CHUNK_SIZE // (1024 * 1024)} MiB"
            )
        if safe_pw:
            print()
            print(f"  {color('Password', Colors.BOLD)} - this is required to decrypt the diagnostic log,")
            print(f"             which is required to submit a PR. Upload the")
            print(f"             diagnostic log file(s) and metadata file with this password.")
            if len(logd_files) > 1:
                print(f"             Reassemble chunks in order before unpacking:")
                print(f"             cat {' '.join(logd_relpaths)} > {logd_path.relative_to(ROOT)}")
            print(f"  {color(safe_pw, Colors.CYAN)}")
            print(f"  {color(f'encryptly unpack {decrypt_target} <outdir> --password {safe_pw}', Colors.GRAY)}")
        return True

    finally:
        shutil.rmtree(workspace, ignore_errors=True)


def print_summary(results: list[tuple[str, bool, float, str, Optional[str]]]):
    print(f"  {color('Build Summary', Colors.BOLD)}")

    total = len(results)
    passed = sum(1 for _, s, _, _, _ in results if s)
    failed = total - passed
    total_time = sum(t for _, _, t, _, _ in results)

    for name, success, elapsed, output, binary in results:
        status_icon = color("✓", Colors.GREEN) if success else color("✗", Colors.RED)
        status_text = color("PASS", Colors.GREEN) if success else color("FAIL", Colors.RED)
        time_str = f"{elapsed:.1f}s" if elapsed < 60 else f"{elapsed / 60:.1f}m"

        print(f"\n  {status_icon}  {color(name + ':', Colors.BOLD)} {status_text}  ({time_str})")
        if binary:
            print(f"       artifact: {color(binary, Colors.GRAY)}")
# ... (truncated) ...