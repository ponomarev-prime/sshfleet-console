# Contributing

SSH Fleet Console uses `dev` as the integration branch and `main` as the protected,
release-ready branch. Open normal changes against `dev`; promote a tested
release from `dev` to `main` through a pull request. Do not develop directly on
`main`.

Before changing behavior, read
[`docs/project-goals-and-scenarios.md`](docs/project-goals-and-scenarios.md) and
the repository's `AGENTS.md`. Every user-visible or security-sensitive change
must update tests and, when needed, the product note in the same commit.

Core development needs Go and OpenSSH. The optional companion toolchain also
needs Git submodules, Rust/Cargo, CMake, and the Neovim build prerequisites.

```sh
make check-core       # unit, vet, build, isolated installer
make test-e2e         # real PTY path
make test-screenshots # PTY menu traversal and terminal snapshots
make test-install-full # build and verify the Linux/x86_64 optional toolchain
make regression       # complete local run with evidence under .artifacts/
make audit-public     # history secret scan and publication hygiene
```

Never add real infrastructure names, private addresses, local absolute home
paths, SSH configs, credentials, or generated test artifacts. Use RFC 5737
documentation addresses such as `192.0.2.10` and `.example` hostnames.

The `lf`, `dtop`, Neovim, and `bat` repositories remain official upstream
submodules unless SSH Fleet Console carries a real patch. If a patch is required, first
publish it in a reachable fork, document the upstream issue or pull request,
then update both `.gitmodules` and `tools/manifest.toml` in one reviewed change.
