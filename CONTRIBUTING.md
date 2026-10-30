# Contributing

Bug reports are most useful with the Metra version, OS, terminal, window size and relevant hardware. For missing GPU or NPU metrics, include the driver version and whether the vendor's own tool can read the metric. Remove hostnames, addresses and process arguments before posting logs.

## Development

Go 1.26+ and Python 3 are enough for the standard checks:

```sh
make check
make build
make smoke
python3 scripts/test_install.py
GO=go python3 scripts/notices.py --check
```

`make check` runs vet and race tests. The PTY smoke test exercises navigation, process search and sorting, settings, resizing and terminal restoration. It never sends a process termination key.

Hardware-specific checks are optional; they query the host without starting accelerator workloads:

```sh
python3 scripts/smoke.py bin/metra --apple-accelerators --responsive
python3 scripts/smoke.py bin/metra --nvidia-engines --nvidia-bandwidth --responsive
python3 scripts/smoke.py bin/metra --ascend --responsive
```

See [test coverage](docs/testing.md) and [architecture](docs/architecture.md). Keep collection, platform adapters and rendering separate. An unreadable counter should remain unknown, rather than becoming a zero.

## Recording the demo

The recording tools are development-only. Metra itself does not use Python.

```sh
python3 -m venv .venv
.venv/bin/pip install -r scripts/requirements-demo.txt
.venv/bin/python scripts/demo.py bin/metra
```

This records an actual PTY session, sends navigation keys, and renders its cells to `docs/assets/demo.gif`. It uses live samples and hides host / user labels. The default font is Menlo on macOS; pass `--font /path/to/monospace.ttf` on Linux.

For a remote NVIDIA host, `scripts/record.py BINARY OUTPUT.cast` needs only Python 3. Copy the recording back, then run `scripts/render_recording.py OUTPUT.cast docs/assets/demo-h200.gif` in the demo virtual environment. Raw recordings can contain host details and are ignored by Git.

## Pull requests

Describe what was wrong, what changes for the user, and how you checked it. Add a regression test when fixing sampling or layout behavior. Please keep unrelated formatting changes out of the diff.

Release packaging is described in [docs/distribution.md](docs/distribution.md).
