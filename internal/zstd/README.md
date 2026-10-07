# Zstandard decoder

Copied from [Go go1.26.4, internal/zstd](https://github.com/golang/go/tree/go1.26.4/src/internal/zstd)
on 2026-09-24. The eight production `.go` files are unchanged upstream source.
`upstream-sha256.json` records the original source, fixture and license digests.
The Go Authors' BSD license is in `LICENSE` and reproduced in Kolk's distribution license.

This pure Go decoder lets Localia extract official Linux `.tar.zst` bundles without an
external decompressor or an additional module dependency. It supports checksums and
concatenated frames, retains at most an 8 MiB history window, and does not support dictionaries.
The caller must bound compressed and decompressed input and handle cancellation.

Tests retain upstream sample, malformed-input, reset, table, window, checksum and fixture
coverage. `zstd_test.go` omits Go-private test helpers, external-compressor tests and benchmarks;
`fuzz_test.go` retains the self-contained decoder fuzzer and omits compressor comparisons;
`xxhash_test.go` retains the known vectors and omits the shared Go fixture and external tool fuzzer.
No test requires an installed `zstd` command.

Maintenance belongs to Kolk's Localia maintainers: review upstream fixes when updating Go or
the runtime installer, refresh the pinned sources/digests/license deliberately, and rerun the
decoder and adversarial archive suites. **Updating the Go toolchain does not update this copy.**
