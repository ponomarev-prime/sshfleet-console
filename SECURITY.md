# Security policy

SSH Fleet Console handles connection metadata and launches the system OpenSSH client,
so security reports should not be posted as public issues before a fix exists.

## Reporting a vulnerability

Use GitHub's **Security → Report a vulnerability** flow for this repository.
Include the affected revision, a minimal reproduction using documentation-only
addresses, impact, and any suggested mitigation. Never attach real SSH configs,
private keys, passwords, bearer tokens, `known_hosts`, agent sockets, or fleet
inventories.

If private vulnerability reporting has not yet been enabled, contact a
maintainer privately and ask them to open a draft GitHub Security Advisory.

## Supported versions

Until the first stable release, security fixes are made on `dev`, promoted to
`main`, and included in the next tagged release. After stable releases begin,
the latest release and `dev` receive security fixes; older snapshots are not
supported.

The security model and hardening backlog are documented in
[`docs/project-goals-and-scenarios.md`](docs/project-goals-and-scenarios.md) and
[`docs/security-backlog.md`](docs/security-backlog.md).
