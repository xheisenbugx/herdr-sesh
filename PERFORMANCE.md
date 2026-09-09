# Directory and preview verification

Measured locally on 2026-09-04 with an Apple M1 Ultra, Go 1.27.1,
Herdr 0.8.2, sesh 2.29.0, and the existing plugin configuration.

## Directory parity

The installed sesh and `zoxide query --list` both returned 249 paths in the same
order. Before this change, herdr-sesh returned 152 entries: 98 original paths
were missing, and one repository root had been introduced by rewriting paths.
Afterward, `list --source zoxide` returned all 249 paths in the same order, with
zero missing or extra entries.

This follows [upstream sesh's zoxide listing](https://github.com/joshmedeski/sesh/blob/main/lister/zoxide.go):
preserve the backend path, and shorten only the home-directory prefix for
display. Listing no longer discovers Git roots or deduplicates by repository or
basename. The combined list still suppresses zoxide paths represented by live
or configured sessions, using exact paths.

## Latency

Median elapsed time, including process startup, after one warm-up and 20 measured
runs per command. The baseline was built from the unchanged checkout before the
fix. Both binaries used the same configuration and live Herdr socket.

| Command | Before | After |
| --- | ---: | ---: |
| Zoxide source, JSON | 26.96 ms | 21.99 ms |
| All sources, JSON | 27.98 ms | 23.25 ms |
| All sources, encoded picker rows | 28.03 ms | 23.34 ms |
| Directory preview | 36.09 ms | 19.79 ms |

For comparison, installed `sesh list -z` measured 43.84 ms. These are warmed local
measurements, not latency guarantees. Occasional roughly 100 ms outliers occurred
in both versions; this run did not isolate their cause.

The Go benchmark for naming/deduplicating 250 nested directories changed from
6.87 ms to 0.235 ms per operation, with allocations falling from 14,517 to 265.
The updated benchmark retains all 250 directories; the old operation collapsed
them to one.

Changes responsible for the improvement:

- Defer Git naming until workspace creation.
- Load Herdr state and frecency independently in parallel.
- Resolve explicit preview paths without listing all sources.
- Run preview shells without login profiles; propagate the selected config to
  avoid rediscovery on picker reloads and previews.
- Buffer picker output, and read up to four live panes concurrently.

## Checks

- `go test -race ./...` and `go vet ./...` passed.
- Rebuilt the plugin with `scripts/build.sh`.
- Compared all zoxide paths and their order against the installed sesh.
- Executed encoded previews for a live workspace, both configured file previews,
  and a deeply nested directory; each returned nonempty ANSI output.
- Exercised the actual fzf UI in a temporary terminal: nested-path filtering,
  switching to configured sessions, file previews, single-column directory
  previews, line wrapping, and cancellation.
- Linked the local checkout in Herdr and successfully invoked its picker action.

Reproduce using `python3 scripts/benchmark.py --runs 20` inside Herdr. Use
`--before /path/to/older/binary` to compare another build, and `--config` to pin
the configuration explicitly. The script runs listing and preview commands;
zoxide may perform its normal database housekeeping during queries.
