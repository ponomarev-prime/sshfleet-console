# sshfleet companion toolchain

This directory keeps the terminal tools used by sshfleet reproducible without
committing generated binaries or build caches.

## Layout

- `src/` contains pinned Git submodules for lf, dtop, Neovim, and bat.
- `config/` contains the shared, reviewed defaults used by sshfleet launchers.
- `launchers/` contains tracked POSIX shell source that selects those configs.
- `bin/` is reserved for generated/install layouts and is ignored by Git.
- `manifest.toml` records the audited release, upstream URL, and exact commit.
- `go.mod` is an intentional module boundary so `go test ./...` in sshfleet
  never treats upstream fixtures (notably bat syntax samples) as application packages.
- `.toolchain/` at the repository root contains generated binaries, Cargo/CMake
  build trees, and Neovim's installed runtime. It is ignored by Git.

The current submodule URLs point to the official upstream projects. Each local
checkout may use a `sshfleet` branch for experiments. Before a source change is
shared, create a reachable remote fork, push that commit, and change the matching
URL in `.gitmodules` and `manifest.toml`; never commit an unreachable gitlink.

## Commands

```sh
make toolchain-sync       # initialize exact source revisions
make toolchain-build      # build every companion tool locally
make toolchain-check      # verify URLs, revisions, configs, and launchers
make toolchain-smoke      # also execute version checks for built binaries
make toolchain-verify     # verify configs and launch real lf/dtop PTYs
make toolchain-ready      # build everything, then run all toolchain checks
make app-ready            # prepare everything and install the `sshfleet` launcher
make remote-bundle        # create the ignored, checksummed Linux/amd64 archive
make test-remote-bundle   # versions + real Neovim TUI in offline Ubuntu 22.04
```

Run the configured tools through `tools/launchers/lf`, `tools/launchers/dtop`,
`tools/launchers/nvim`, `tools/launchers/bat`, or the Ubuntu-compatible
`tools/launchers/batcat`.
The reviewed lf config opens files with `bat`; files above 50 lines request
`less` paging when that capability exists and otherwise use bat's built-in
pager. `e` resolves the editor in the explicit order `nvim`, `vim`, `nano`
through `tools/launchers/sshfleet-editor`.
To make these defaults available in a shell without installing anything:

```sh
source ./tools/activate.fish       # Fish
# . ./tools/activate.sh            # Bash
```

Shell activation is only for launching companion tools directly. Normal use is
simply `sshfleet`; `make app-ready` installs that launcher into `~/.local/bin` and
injects companion paths internally. The launcher is shell-independent and is
verified through `PATH`, from an unrelated working directory, in POSIX `sh`,
Bash, Zsh, and Fish.

The manual acceptance scenarios are in
[`docs/manual-toolchain-checks.md`](../docs/manual-toolchain-checks.md).

The remote archive is not a server installation. It uses locally built pinned
`lf`, `dtop`, and `bat`, and the official pinned Neovim Linux tarball verified
against its published SHA-256. The archive includes only reviewed configs and
required runtime libraries, is written under ignored `.toolchain/remote/`, and
is deleted from the selected host after each action by default.

For users, `./install.sh` installs the self-contained core binary only and
`./install.sh --full` builds and
installs this complete optional toolchain. Official upstream submodules are kept
instead of routine forks: they cost only gitlink metadata until full mode asks
Git to initialize them. A fork is introduced only for an SSH Fleet Console source patch.

`dtop` is built without its self-update feature. Tool upgrades are explicit:
review an upstream release, update the submodule and manifest together, rebuild,
run `make toolchain-smoke`, and commit only the source lock/config changes.
