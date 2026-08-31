# Security backlog

The current contract is fail-closed: no plaintext secrets in configuration,
password and HTTPS bearer credentials only through Secret Service, strict
host-key checking, and no remote OpenSSH configuration.

## Before password credentials are called production-ready

- Add native KWallet support for desktops that do not expose the
  `org.freedesktop.secrets` API.
- Evaluate locked/guarded memory for the short-lived AskPass buffer and minimize
  copies that cannot be explicitly zeroed by Go or libsecret.
- Add integration tests against a disposable D-Bus Secret Service and confirm
  crash dumps, traces, errors, and debug logs never contain credential values.
- Harden explicit third-party `SSHF_ASKPASS` overrides with ownership/mode
  policy. Official core releases use a second process of the same verified
  `sshfleet` binary and do not require a separately packaged helper.
- Define credential rotation, deletion, expiry, lock/unlock, and least-privilege
  behavior for unattended probes.
- Implement native macOS Keychain and Windows Credential Manager providers behind
  the platform credential-store interface. Until then those platforms must
  advertise the gap in healthcheck and disable credential-bound actions; never
  emulate them with plaintext files, argv, environment values or a Unix-only
  `secret-tool` assumption.

## Implemented for encrypted and remote inventory

- One bundle format is shared by local encrypted and remote inventory:
  exact-byte `manifest.toml`, `inventory.toml.age`, and detached SSHSIG over the
  manifest with namespace `sshfleet-inventory-v1`. The signed manifest binds
  source ID, schema version, monotonically increasing revision, created/expires
  times, and SHA-256 of the ciphertext.
- HTTPS origins are explicit and authenticated by the system trust store;
  redirects are refused, while payload size, request duration, and content
  types are bounded. HTTPS credentials are separate Secret Service
  references; they never share the host-password credential namespace.
- SSHSIG is verified against pinned/rotatable allowed signers. Source ID,
  expiry, anti-rollback revision, and ciphertext hash are checked before
  invoking `age`.
- Decryption is in memory using an age identity from Secret Service/KWallet or an
  explicitly configured hardware age plugin. Never store an age identity in
  TOML or in the encrypted cache.
- The same strict inventory schema is validated after decryption. Bundle
  creation and encrypted remote cache writes use staging directories plus
  atomic rename.
- Only the signed manifest, detached signature, and encrypted payload enter the
  remote cache; tests scan it for plaintext host data.
- Remote `ssh_config`, `ProxyCommand`, `Match exec`, shell snippets, and local
  path fields inside the inventory remain rejected.

## Implemented for encrypted OpenSSH keys

- A restricted inventory can reference only a logical identity name. The local
  main config maps it to a mode-0600 encrypted private key and a Secret Service
  `key-passphrase` credential.
- OpenSSH receives the identity path as a structured argument and retrieves the
  passphrase from the separate AskPass process. The passphrase is absent from
  TOML, argv, logs, and environment variables.
- Key-passphrase sessions force public-key authentication and `IdentitiesOnly`;
  they never silently fall back to a server password.

## Remaining remote-source hardening

- Add optional TLS SPKI pinning for deployments that require a trust boundary
  narrower than the operating-system CA store.
- Add bounded last-known-good offline fallback with an explicit maximum stale
  age; currently a failed HTTPS refresh fails closed even if cache files exist.
- Add key-rotation overlap policy for `allowed_signers` principals and an
  operator command that reports accepted revision/hash without decrypting.
- Add a disposable D-Bus Secret Service integration fixture for bearer and age
  identity retrieval; current crypto/TLS tests inject a secret lookup adapter.

## Implemented for portable remote workspaces

- The launcher exposes only a generated, ignored Linux/amd64 archive. Before
  upload SSH Fleet Console verifies its SHA-256 sidecar, compressed/expanded size and
  entry count, rejects absolute paths, traversal, links and device entries, and
  requires the closed set of launch wrappers.
- Upload uses the already resolved OpenSSH host with no TTY and with agent,
  stream, X11 and local-command forwarding disabled. Extraction occurs only
  after remote Linux/x86_64 and glibc 2.34+ capability checks and under a fresh
  mode-0700 `/tmp/sshfleet-workspace.*` directory.
- Only fixed tool identities (`lf`, `nvim`, `dtop`, tools shell and self-test)
  reach the remote command. No arbitrary command or caller-supplied remote path
  is accepted.
- The archive contains pinned binaries and reviewed allowlisted configs, but no
  SSH config, private key, password, bearer token, agent socket, history or
  local Docker socket. It never invokes `sudo` or a remote package manager.
- The workspace runner owns its directory and removes it on normal exit, tool
  failure and HUP/INT/TERM by default. Docker/OpenSSH and real Debian acceptance
  tests verify cleanup.

## Remaining portable-workspace hardening

- Add an explicit cleanup attempt if the second local OpenSSH process cannot be
  started after upload; the remote runner already covers every exit after it
  starts.
- Add signed release metadata for the complete generated archive, not only a
  local SHA-256 sidecar, before distributing prebuilt bundles.
- Add Linux/arm64 and musl artifacts rather than weakening the current glibc
  capability check.
- If cache-by-hash is introduced, isolate it per user/session, defend against
  symlink/replacement races, bound its lifetime, and preserve cleanup-by-default.

## Before web automation is enabled

The local TUI group-command slice is intentionally narrower and is already
implemented: only presets from the trusted main application TOML are accepted;
restricted/encrypted/remote inventories cannot supply commands. SSH Fleet Console
captures an immutable host snapshot, renders a plan, requires a second Enter,
uses argv-safe OpenSSH invocation without a local shell, disables forwarding
and TTY, bounds concurrency/time/output, and sanitizes terminal control data.
This is operator-local convenience, not yet an unattended automation system.

- Separate immutable host-set selection from mutable inventory and require a
  rendered plan before every mutating or destructive command-preset job.
- Keep executable definitions in a reviewed, signed local policy repository;
  remote/restricted inventory may reference a preset ID but never supply shell
  text, executable paths, environment values, or secret-bearing arguments.
- Add RBAC, approval policy, per-host/global concurrency, cancellation,
  idempotency declarations, bounded redacted output, immutable audit events and
  retention limits before multi-host execution is production-ready.
- Replace the current Foliage root command-runner model with a signed one-shot
  collector first. A persistent agent requires mTLS host identity, signed
  configuration, allowlisted collectors, timeouts, non-overlap, spool/ACK/retry,
  systemd sandboxing and credential rotation.
- Treat full inventory as sensitive operational data: define section-level data
  classification, minimization, redaction, authorization, encryption and expiry;
  never collect private keys, password hashes, environment secrets or arbitrary
  file contents.
- Treat Dozzle/Docker API access as privileged. Installation must pin an image
  digest, bind to loopback/private networks by default, require authentication
  and custom agent certificates when exposed, disable shell/actions by default,
  and support verified backup/rollback/uninstall ownership.

Design references: the authenticated [`age` file format](https://c2sp.org/age),
OpenSSH detached signatures and allowed signers in
[`ssh-keygen -Y`](https://man.openbsd.org/ssh-keygen.1), and the freedesktop
[`Secret Service` API](https://specifications.freedesktop.org/secret-service/latest/).
