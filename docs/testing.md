# Tests

```sh
make check
make smoke
python3 scripts/test_install.py
GO=go python3 scripts/notices.py --check
```

`make check` runs vet and the complete race suite. Tests cover counter differences and resets, missing samples, ring buffers, CPU / memory limits, sensor parsing, accelerator backends, configuration, process sorting and identity, and terminal layouts.

The PTY test starts Metra in a real pseudo-terminal and exercises pages, search, tree mode, sorting, settings, pause and exit. It checks that the alternate screen is restored. It does not press the process termination key; signal tests use only a child process created by the test itself.

## Layout and charts

Layout cases range from 1×1 to 200×60, in English and Chinese, with many cores and multiple accelerators. They check exact output dimensions, complete chart borders and values remaining accessible when graphs are hidden.

Chart tests cover zero and full-scale values, half-cell rounding, tiny positive samples, overlapping series, missing data and fixed-time scrolling. Memory composition uses cumulative rounding so segments always fit the available width. Color tests check contrast in numeric-only views. Layout regressions also compare frame positions while values cross 9.9 / 10.0 / 100.0 and byte-rate unit boundaries, so digit growth cannot move charts.

```sh
python3 scripts/smoke.py bin/metra --responsive --language zh-CN --terminal-background light
python3 scripts/smoke.py bin/metra --exit-method ctrl-c
python3 scripts/smoke.py bin/metra --exit-method sigterm
```

## Platform checks

CI runs on Ubuntu and macOS and packages Linux / macOS for amd64 / arm64. Cross-compilation does not substitute for testing on the target hardware.

The Linux resource tests use temporary proc / cgroup fixtures for v1, v2, nested limits, cpusets, affinity, fractional CPU quotas and Slurm allocations. Filesystem and interface tests cover Snap, EFI, bind mounts, root overlay, read-only data volumes, default routes, container networks and VPN accounting.

Accelerator tests use parser fixtures by default. Live NVIDIA checks require `METRA_TEST_NVIDIA=1`; Apple and Ascend PTY checks have explicit flags listed in [CONTRIBUTING.md](../CONTRIBUTING.md). Missing hardware does not count as a successful hardware check.

The code has been exercised on Apple M2, an 8×RTX 5090 host, an H200 / H100 host, Ascend 910B3 and a Raspberry Pi. Those checks establish behavior on those systems, not universal driver compatibility.

## Benchmarks and recordings

```sh
go test ./internal/chart ./internal/tui -run '^$' -bench . -benchmem
python3 scripts/smoke.py bin/metra --capture /tmp/metra-capture
python3 scripts/preview.py /tmp/metra-capture/performance.ansi /tmp/metra.png
```

Benchmarks measure rendering separately from collection. Process count, vendor tools and refresh interval affect the total cost. The optional `pyte` / Pillow tools render captured terminal cells; they do not generate sample data.
