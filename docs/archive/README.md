# Archive

Landed audit reports and implementation plans. **Historical context only** — every task described here has been executed. Read these to understand *why* the code looks the way it does, not as a to-do list.

| Doc | Status | What it captured |
|---|---|---|
| [improvement-audit-2026-04-08.md](improvement-audit-2026-04-08.md) | ✅ landed | UI/UX consistency pass — footer hint unification, Esc-verb standardisation, color centralisation, error handling. Phase A applied in `9fa3bda`; Phase C refactors landed in `d3a0906` → `1b296ce`. |
| [perf-audit-2026-04-08.md](perf-audit-2026-04-08.md) | ✅ landed | `lipgloss` allocation churn fixes, per-message format cache, friend-card pre-decode, debounced config / notifications saves, `FriendStore` index map. Applied in `f10af4e`, `8b56ac4`. |
| [dead-code-audit-2026-04-08.md](dead-code-audit-2026-04-08.md) | ✅ landed | 6 `staticcheck U1000` items removed in `e8b8d60`. Tree is clean under `staticcheck -checks U1000,U1001`. |
| [phase-c-plan-2026-04-08.md](phase-c-plan-2026-04-08.md) | ✅ landed | Step-by-step plan for the three Phase C refactors — `SelectableList` primitive, `model.go` split into `handlers_*.go` + `cmds.go` + `persist.go`, `OverlayScaffold`. All landed. |

For the current state of the architecture after these passes, see `../architecture/overview.md` and `../architecture/tui-model-split.md`.
