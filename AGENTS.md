# SSH Fleet Console agent instructions

Before planning or changing this repository, read
`docs/project-goals-and-scenarios.md` completely.

- Treat that note as the persistent product contract.
- Keep the HOSTS pane primary and preserve the documented keyboard scenarios.
- Preserve every source trust boundary and fail closed on ambiguous security
  state.
- Never place passwords, bearer tokens, age identities, or private keys in the
  repository, TOML values, argv, logs, test snapshots, or examples.
- Add or update automated tests for every changed user scenario, especially the
  Enter action menu, source loading, PTY behavior, scheduler bounds, and
  backup-first host-key repair.
- Run the relevant Makefile checks before committing.
- When a product goal, scenario, or security contract changes, update the note
  in the same commit as the implementation.
