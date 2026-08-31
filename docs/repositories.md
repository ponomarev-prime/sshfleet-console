# SSH Fleet repository map

SSH Fleet is a product family with separate deployables, release cycles and
security boundaries. It is intentionally not a monorepo.

| Product | Repository | Responsibility | User entrypoint |
|---|---|---|---|
| SSH Fleet Console | `sshfleet-console` | Local Go TUI, OpenSSH integration, local sources and credentials | `sshfleet` |
| SSH Fleet Web | `sshfleet-web` | Browser UI for fleets, subscriptions and audited jobs | Browser |
| SSH Fleet Hub | `sshfleet-hub` | Authentication, subscription API, signed/encrypted source delivery and audit | HTTPS API |

`SSH Fleet Control` is not the console name. “Control” describes capabilities
of the future Hub control plane and would make the local, agentless TUI sound
like an always-on server or unrestricted remote executor.

## Stable names

The repository and human-facing application name may be specific, while the
long-lived local namespace remains short:

- binary: `sshfleet`;
- optional collision-safe alias: `sf`;
- transitional legacy alias until `v1.0.0`: `sshf`;
- environment prefix: `SSHF_`;
- config/data directories: `~/.config/sshfleet` and
  `~/.local/share/sshfleet`;
- Secret Service and signed inventory namespaces: `sshfleet/...`;
- workspace paths and compatibility backup suffixes: `sshfleet-*`.

These are protocol and compatibility identifiers, not stale product labels.
Changing them would need an explicit migration and deprecation period.

## Integration contract

The repositories must not import one another's source trees or depend on a
shared checkout. Integration uses independently versioned contracts:

1. restricted inventory and encrypted bundle schemas;
2. OpenAPI for Hub HTTP endpoints;
3. versioned event and job-result schemas;
4. signed release artifacts and checksums;
5. explicit capability negotiation and fail-closed handling of unknown major
   versions.

A shared contract may later receive its own `sshfleet-spec` repository when two
implementations genuinely need it. Until then, the authoritative schema lives
with its owning service and released snapshots are consumed by the other
repositories.

## Release ownership

- Console SemVer and binaries are produced only by `sshfleet-console`.
- Web assets are produced only by `sshfleet-web`.
- Hub server images and API migrations are produced only by `sshfleet-hub`.
- A family release may publish a compatibility matrix, but it never rebuilds
  another repository's artifacts.
