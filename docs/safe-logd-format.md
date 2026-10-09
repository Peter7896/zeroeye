# Safe diagnostic bundles, version 1

`safe-logd-v1` replaces the bundled opaque Encryptly executable in the build
path. It is a new format, **not Encryptly-compatible**. Old artifacts remain
historical records; this build path never executes their bundled decoder.

## Explicit inputs and boundaries

`tools/safe_logd.py` accepts exactly three in-memory UTF-8 strings named
`build.log`, `build-summary.txt`, and `system-info.txt`. It has no directory-pack
API, default include paths, HOME scan, recursive discovery, network operation,
or shell execution. The caller controls output parents. Existing final files,
symlinks, and extraction directories are rejected instead of overwritten.

Each text is limited to 8 MiB, total raw UTF-8 to 16 MiB, and serialized JSON to
16 MiB. Escaping can make the serialized limit stricter. Oversize input fails
explicitly; encrypted logs are never silently truncated. Archive input is bounded
by the payload cap plus its header, nonce and tag. CLI metadata is limited to
1 MiB. Each module's JSON `output` preview is limited to 16 KiB of UTF-8;
`output_truncated` and `output_byte_count` describe the full redacted output.
`full_output` is `build.log` only when a real encrypted archive exists.

## Encryption and decoding

The binary representation is:

1. Nine bytes `SAFELOGD\x01`, including the version byte.
2. A fresh random 12-byte nonce.
3. AES-256-GCM ciphertext followed by the full 16-byte authentication tag.

The complete magic/version is authenticated as associated data. `cryptography`,
pinned in `requirements-diagnostics.txt`, provides AES-GCM. Each archive gets a
fresh random 32-byte key. The plaintext is the UTF-8 JSON object:

```json
{"format":"safe-logd-v1","files":{"build.log":"...","build-summary.txt":"...","system-info.txt":"..."}}
```

The decoder rejects altered ciphertext, wrong keys, duplicate JSON keys, unknown
fields, other filenames, non-text content, unsupported versions and oversize
documents. Authentication completes before parsing or writing content.
Extraction creates a new directory and only these fixed filenames; it never
joins archive-controlled filenames into destination paths.

The JSON `password` field contains the canonical padded base64url encoding of
the random AES key, not a human password. Publishing it makes the bundle publicly
readable and permits anyone to produce another valid ciphertext. AES-GCM is not
an author signature and does not prove a build occurred. Verify the public source
revision, reviewed producer, actual logs and recorded results.

## Build provenance and privacy

Commit reviewed source before invoking `python build.py`. The builder records
full `source_commit` and `source_tree`, plus Git state before/after building.
Unknown state, a changed HEAD/tree, tracked modifications or untracked unignored
files cause diagnostic generation to refuse. Ignored compiler outputs are
allowed. The full source IDs also appear in the encrypted summary; they refer to
the code built, not the later commit that adds diagnostic artifacts.

`diagnostic/` must stay inside the repository and cannot be a symlink. Existing
artifacts for the same source prefix are preserved. A deliberate retry requires
reviewing prior evidence and a fresh committed source revision. The builder does
not commit or push files. The diagnostic CI gate is unchanged, and a nonempty
`.logd` alone does not validate its contents or demonstrate all modules passed.

Only Python version, OS name/release, architecture and CPU count are explicitly
collected for system information. Known repository/home/temp path spellings and
their resolved paths, user/host identities and common labelled credentials are
redacted from all three texts and JSON output. This cannot recognize arbitrary
secrets printed by dependency scripts; inspect decrypted contents before public
submission. No environment dump, mount listing or out-of-repository file scan is
performed by diagnostic generation.

Build failure logs are real evidence and are packaged with `FAIL` statuses. The
process exits unsuccessfully if any selected module fails or packaging fails.
A JSON error report without an archive is not a successful diagnostic bundle.
Selected builds such as `-m backend,v2-market-stream` cover only those two
modules, not all ten. Test fixtures are never project build evidence.

## Verify generated files

Install the pinned dependency in a virtual environment, then use a new output
directory:

```sh
python -m pip install -r requirements-diagnostics.txt
python tools/safe_logd.py unpack diagnostic/build-12345678.logd /path/to/new-output \
  --metadata diagnostic/build-12345678.json
```

The generator performs a real encrypt/decrypt comparison before recording a
successful archive. Compare decoded full source IDs and module results against
the JSON, then check UTF-8 byte lengths and SHA-256 hashes in `files`, and archive
hashes in `artifact_sha256`. These public hashes are integrity checks, not signed
attestations. Confirm that the decoded set is exactly the three required files.

The existing ordered `-part001.logd`, `-part002.logd`, ... convention remains
available for archives over `DIAGNOSTIC_CHUNK_SIZE`. The default is 40 MiB, above
the version-1 payload cap, so default builds produce one file. If a deployment
lowers that threshold, concatenate exactly the JSON-listed parts in order into
a new file before decoding; do not use a broad glob over unrelated artifacts.
