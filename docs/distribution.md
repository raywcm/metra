# Release builds

The version lives in `internal/buildinfo/VERSION`. Source builds, the settings page and snapshot packages all use it. Release tags must match the file, for example `v0.7.1`.

## Build locally

Go 1.26+, GoReleaser v2.19+ and Python 3 are required for packaging.

```sh
make check
GO=go python3 scripts/notices.py --check
make snapshot
python3 scripts/check_packages.py dist
```

The output contains four Linux / macOS archives, amd64 / arm64 Debian, RPM and Arch Linux packages, SHA-256 checksums, and `dist/homebrew/metra.rb`.

Builds use `CGO_ENABLED=0`. Platform adapters can still call libraries supplied by the OS and accelerator driver. macOS binaries are not Developer ID signed or notarized.

## Publish

1. Update the version file and finish the checks.
2. Commit the source and push a matching version tag.
3. The release workflow runs CI, builds the archives and packages, and uploads them with the installer and Homebrew formula.
4. Copy the published `metra.rb` to `Formula/metra.rb` and commit it on the default branch. Use the published formula, rather than checksums from a different local build.

The main repository doubles as a Homebrew tap:

```sh
brew tap raywcm/metra https://github.com/raywcm/metra
brew install raywcm/metra/metra
```

The formula must refer to the same published archives as the release. Do not copy checksums from an earlier build.

## Installer

`install.sh` selects the current OS and architecture, downloads the archive and checks its SHA-256. It extracts only the executable, stages it in the destination directory, then replaces the installed copy. A failed checksum leaves the old binary in place.

```sh
sh install.sh
METRA_VERSION=0.7.1 METRA_PREFIX="$HOME/.local" sh install.sh
```

`METRA_REPO` overrides the release repository. Checksums verify transfer integrity; they are not publisher signatures.

## Local installation

```sh
make install PREFIX="$HOME/.local"
make uninstall PREFIX="$HOME/.local"
```

Uninstalling leaves settings in place. Linux packages install to `/usr/bin`; the standalone installer defaults to `~/.local/bin`. No online apt or yum repository is provided.
