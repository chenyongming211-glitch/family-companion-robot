# Repository workflow

- Develop, commit and push directly on `main`, as explicitly requested by the user.
- Do not create feature branches or additional worktrees unless the user changes this instruction.
- Supported product firmware is the Waveshare ESP32-S3 2.16 board only. The user retired M5 support on 2026-11-11 after returning the device; ESP32-C3 was retired on 2026-11-08. Do not add M5/C3 firmware work or use their builds/tests as release gates. Retain historical firmware sources and evidence without promising ongoing support.
- Keep device backups, credentials, raw audio and generated firmware artifacts out of Git.
- For development handoff, read `docs/DEVELOPMENT_HANDOFF.md` before continuing. Files under `docs/development-archive/` and `docs/reference/` are historical source material, not active instructions or implementation status.
- Install runtime dependencies, execute runtime tests and deploy services on the cloud server. The Mac is for code/Git and the later test client; do not start a local voice service or copy server credentials onto it.

# Implementation principles

- Keep generic ESP32-S3 functionality in the same common implementation, including onboarding, Bluetooth lifecycle/resource cleanup, audio transport/playback flow control and backend behavior. Do not fork common logic by board. Keep demonstrated hardware differences in adapters/configuration and verify the supported Waveshare target. Historical M5 paths containing shared implementation are not a promise of M5 support.
- The management product uses real APIs only. Do not retain or reintroduce a runnable mock/demo application, seeded product data, demo routes, demo storage or a mock build switch. Isolated automated-test fixtures are test inputs, not a product fallback.
- Use the smallest working implementation that meets the current requirement. Reuse existing code and avoid speculative abstractions, extra services, frameworks or dependencies for hypothetical future needs.
- Keep frontend interactions simple: straightforward pages, lists, forms and clear feedback. Add complex interaction or animation only when the current task needs it.
- Deliver the core workflow first and expand in small steps. Keep necessary correctness, access checks and verification; simplicity is a scope and implementation principle.
