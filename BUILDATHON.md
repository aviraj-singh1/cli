# SHIFT Buildathon Evidence

SHIFT is a requirement-change control layer for AI coding agents. It turns a
new requirement, checkpoint-backed historical intent, and Entire Graph
structural evidence into a pre-change contract, then deterministically verifies
the implementation against that same contract.

## Milestone 1: Initial understanding and architecture

Recorded before product implementation in the fresh post-Graph-activation
Codex session.

### Required environment checks

- Mirror clone: `C:\Users\DELL\Documents\SHIFT\cli`
- Entire status: enabled on `main`; Codex is enabled; checkpoint sync target is
  `origin`.
- Entire Graph version: `v0.4.0`.
- Graph capabilities report Go semantic relations including definitions,
  calls, type use, field access, data flow, test relationships, and semantic
  diff support.

The `entire` executable was not present on this PowerShell process's `PATH`, so
development evidence uses the installed absolute binary at
`C:\Users\DELL\.local\bin\entire.exe`. This is an environment constraint, not
evidence that Graph is unavailable.

### Architecture decision

The implementation will stay native to the existing Go/Cobra CLI and separate
the responsibilities that the PRD identifies:

1. a `shift` Cobra group with non-interactive `analyze` and `verify` commands;
2. a structured contract model and deterministic status reducer;
3. checkpoint retrieval through Entire's existing checkpoint readers rather
   than direct parsing of checkpoint Git storage;
4. a Graph adapter that invokes the installed `entire graph` surface and keeps
   failures explicitly `UNVERIFIED`;
5. deterministic Markdown/JSON artifact rendering under `.shift/`;
6. verification driven by explicit evidence inputs and test-command results;
7. a small authentication fixture demonstrating blocked and verified paths.

Checkpoint provenance is load-bearing: an invariant cannot be marked
`EXPLICIT` without a successfully read checkpoint and a source reference.
Graph output remains structural evidence and never substitutes for source or
test proof.

### Early Entire Graph evidence

Command:

```text
entire graph search --repo . --profile full --query \
  "native Cobra command registration and checkpoint reading for shift analyze verify"
```

The search indexed the mirror at commit `3dbdf8b83c39ef7ae73b9c613612f4c51aff3f1a`
and returned these relevant existing seams:

- `api/checkpoint/interfaces.go:93` — `ReadCheckpoint`, the shared reader
  abstraction that normalizes missing checkpoints and wraps reader errors;
- `cmd/entire/cli/explain.go:693` — the existing local checkpoint lookup and
  explanation path;
- `cmd/entire/cli/strategy/manual_commit.go:54` — construction of checkpoint
  stores with the elected read-remotes chain;
- `cmd/entire/cli/strategy/common.go:314` — checkpoint listing metadata.

The top-ranked result was an API response identity validator, so rank alone was
not treated as an architecture decision. The focused source and neighboring
callers must determine the reusable local reader path.

## Remaining required evidence

- Graph relationship/impact analysis before the checkpoint/command integration
  change.
- Graph semantic diff after implementation.
- Stable implementation, curveball-response, and final-verification Entire
  checkpoint milestones.
