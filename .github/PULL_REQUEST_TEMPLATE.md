## What changed

Describe the user-visible behavior and the product goal or scenario it supports.

## Trust boundary

State whether this changes sources, credentials, OpenSSH invocation, host-key
handling, remote commands, terminal input, or release artifacts. Write “none”
when it does not.

## Verification

- [ ] Relevant unit/model tests were added or updated.
- [ ] Relevant PTY/E2E scenario was exercised.
- [ ] `make check-core` passes.
- [ ] `make regression` passes when the change affects a release-critical path.
- [ ] Documentation and `docs/project-goals-and-scenarios.md` are updated.
- [ ] Screenshots and logs contain no private fleet data or credentials.

## Release note

One sentence suitable for a changelog, or “not user-visible”.
