# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root
- **`docs/adr/`** — read ADRs that touch the area you're about to work in

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skill (`/grill-with-docs`) creates them lazily when terms or decisions actually get resolved.

## File structure

This is a single-context repo:

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-....md
│   └── 0002-....md
└── ...
```

## Where the docs came from

`CONTEXT.md` at this repo's root is canonical. It originated in the planning repo at `~/Projects/plans/Ganymede-harness/` and was moved here when implementation started — treat the copy still sitting there as historical and do not read or update it.

The planning repo remains the home of `SPEC.md` (the build's source of truth), `MAP.md` (decision map), and `issues/` (per-decision rationale, not requirements). Those are not domain docs and these rules don't cover them.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

This matters more than usual here: `CONTEXT.md` is normative for code identifiers and UI copy, not just prose. Session states are `Working` / `Blocked` / `Ready` / `Idle` / `Shell` / `Gone`; root states are `Free` / `In use by agent` / `Claimed`, with `Takeover` as the action. Each entry lists the synonyms to avoid — respect them.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/grill-with-docs`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
