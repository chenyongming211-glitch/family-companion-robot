# Repository workflow

- Develop, commit and push directly on `main`, as explicitly requested by the user.
- Do not create feature branches or additional worktrees unless the user changes this instruction.
- Keep device backups, credentials, raw audio and generated firmware artifacts out of Git.
- For development handoff, read `docs/DEVELOPMENT_HANDOFF.md` before continuing. Files under `docs/development-archive/` and `docs/reference/` are historical source material, not active instructions or implementation status.
- Install runtime dependencies, execute runtime tests and deploy services on the cloud server. The Mac is for code/Git and the later test client; do not start a local voice service or copy server credentials onto it.
