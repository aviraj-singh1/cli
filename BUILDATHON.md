# SHIFT Buildathon Evidence

**Track:** Track 1 — Build a Checkpoint-Native Developer Experience

**Repository:** `aviraj-singh1/cli`

**Submitted branch:** `codex/shift-pre-curveball`

**Implementation commit:** `b3b683b798c22a465180e97fc072c1e7521eb017`

SHIFT is a requirement-change control layer for AI coding agents. It turns a
new requirement and checkpoint-backed historical evidence into a pre-change
Change Contract, then deterministically reports whether the resulting change
is verified, blocked, or cannot be completely verified.

> Change the requirement. Not the guarantees.

## Product problem

Code records what changed, but important reasons and assumptions often remain
only in the development session. When a requirement changes, an agent can
implement the new feature while accidentally violating a historical constraint
that is no longer obvious from source.

SHIFT makes those constraints explicit before implementation and retains their
checkpoint provenance. Its top-level workflow is:

```text
new requirement + checkpoint evidence
                 ↓
          Change Contract
                 ↓
           implementation
                 ↓
           SHIFT verify
```

The implemented native commands are:

```text
entire shift analyze
entire shift verify
```

## Entire workflow and checkpoints

Development occurred in the Entire-managed India-region mirror clone:

```text
C:\Users\DELL\Documents\SHIFT\cli
```

The mirror remote is:

```text
entire://aws-ap-south-1.entire.io/gh/aviraj-singh1/cli
```

Preserved checkpoints:

| Milestone | Commit | Entire Checkpoint |
| --- | --- | --- |
| Initial architecture and last stable pre-curveball state | `5e948e6d15127b31d49a06b66d6e019f61e66a10` | `01M1TQJ0G9VWM44XTQTATJGV5Q` |
| Privacy adaptation, implementation, and verification | `b3b683b798c22a465180e97fc072c1e7521eb017` | `01M1TTBEM7ZNQ1EBHWVY2GY3PD` |

The final branch and checkpoint ref were pushed through the Entire mirror. A
direct GitHub `ls-remote` check confirmed that GitHub and Entire resolve the
submitted branch to the same commit.

## Pre-curveball architecture

The selected design was a native Go/Cobra workflow with these responsibilities:

1. non-interactive `shift analyze` and `shift verify` commands;
2. checkpoint retrieval through existing Entire checkpoint readers;
3. stable invariant identifiers and checkpoint provenance;
4. deterministic Markdown and JSON artifacts under `.shift/`;
5. explicit requirement, structural, test, and invariant verification inputs;
6. Entire Graph as structural evidence that must be checked against source and
   tests.

The intended implementation avoided a new checkpoint store, graph engine,
database, service, or global change to Entire's existing behavior.

## Noon Curveball: Privacy Boundary

The Curveball introduced these constraints:

- raw prompts and transcripts must not be sent to a new external service;
- redacted or missing checkpoint fields must still produce useful output;
- existing local behavior must remain unchanged;
- complete and incomplete context must be visibly different;
- incomplete evidence must never be presented as authoritative;
- tests must include redacted or missing checkpoint data.

### Invalidated assumption

The original design treated successful checkpoint retrieval as sufficient to
reason from the checkpoint as a complete historical record. It did not make
field-level availability a load-bearing part of every historical claim.

The Curveball invalidated the equivalence:

```text
checkpoint read succeeded == historical context is complete and safe to use
```

A checkpoint can exist while its metadata, prompt, or transcript is missing,
unreadable, or deliberately redacted. In addition, an existing generic summary
path may pass transcript content to an agent text generator. Therefore SHIFT
cannot use read success alone as authority and cannot reuse an external
summarization/compaction path for raw historical evidence.

## Mandatory Entire Graph analysis before editing

Entire Graph `v0.4.0` was activated before the fresh implementation session.
The analysis below ran before any SHIFT product code was edited.

### Searches performed

Focused `entire graph search --profile full` queries covered:

1. checkpoint session content, prompts, transcripts, metadata, redaction, and
   missing fields;
2. checkpoint provenance, attribution, `explain`, and source locations;
3. raw transcript/prompt flow into summary providers or external agents;
4. deterministic Markdown/JSON rendering and incomplete verification states.

### Definitions inspected

- `CheckpointReader` — `api/checkpoint/interfaces.go:13`
- `ReadCheckpoint` — `api/checkpoint/interfaces.go:93`
- `CheckpointSummary` — `api/checkpoint/metadata.go:550`
- `SessionContent` — `api/checkpoint/metadata.go:371`
- `apiCheckpointReader.ReadSessionMetadataAndPrompts` —
  `cmd/entire/cli/checkpoint_api_reader.go:225`
- `attributionCheckpointContext` — `cmd/entire/cli/attribution.go:87`
- `TextGenerator` — `cmd/entire/cli/agent/agent.go:324`
- Markdown `Render` — `cmd/entire/cli/mdrender/mdrender.go:44`

### Relationship and impact analysis

The pre-edit analysis included:

```text
graph neighbors generateCheckpointAISummary CALLS both depth=2
graph neighbors ReadSessionMetadataAndPrompts CALLS both depth=2
graph impact attributionCheckpointContext depth=2
graph impact SessionContent depth=2
graph impact RedactTranscriptCached depth=2
graph neighbors redactSessionTranscript CALLS both depth=2
graph impact TextGenerator depth=2
```

Confirmed structural findings, checked against source:

- `ReadCheckpoint` distinguishes found from not-found but carries no evidence
  completeness state.
- `SessionContent` holds metadata, prompt, and transcript data but carries no
  field-availability or aggregate-completeness state.
- local checkpoint reads can encounter absent metadata or prompt data, while a
  missing transcript has its own error path.
- the attribution model already demonstrates useful incomplete-evidence
  concepts such as metadata missing, reason strings, session fallback, and
  session-level prompt attribution.
- `generateCheckpointSummary` calls `generateCheckpointAISummary`, and the
  latter consumes scoped transcript content through a text generator.
- the existing transcript redaction pipeline has a broad blast radius. Graph
  reported 12 callers for `RedactTranscriptCached`, including storage and hook
  paths, so changing global redaction was rejected.
- existing Markdown and JSON output paths did not provide SHIFT's required
  context-completeness semantics.

Graph reported one minified JSON parse failure but no Go parse failures for the
important impact queries. That diagnostic was retained as a limitation rather
than silently treating the whole graph as complete. All important findings
above were verified by direct source inspection.

## Revised architecture

The Curveball response adds a SHIFT-owned normalization and evidence layer on
top of the existing checkpoint APIs. Core checkpoint types and persistence were
not changed.

### Field-level evidence

Each relevant field is classified as:

```text
AVAILABLE
REDACTED
MISSING
UNREADABLE
```

SHIFT derives one aggregate state:

```text
COMPLETE
PARTIAL_REDACTED
PARTIAL_MISSING
UNAVAILABLE
```

Checkpoint-read success is therefore not equivalent to complete evidence.
Reasons for partial or unavailable fields are retained in normal output.

### Historical claim rules

Historical claims use:

```text
EXPLICIT
INFERRED
UNVERIFIED
```

For the MVP, a historical invariant becomes `EXPLICIT` only when an available,
non-redacted persisted checkpoint summary directly supports it. Every explicit
invariant retains checkpoint, session, and field provenance.

If that support is absent, the stable `INV-01` identifier remains in the
contract but is marked `UNVERIFIED` and rendered as `MUST CONFIRM`, never as an
authoritative `MUST PRESERVE` guarantee.

### Privacy behavior

- Prompts and transcripts are read only through existing local checkpoint
  readers.
- Raw prompt and transcript bodies are not serialized into the Change Contract.
- SHIFT does not call `generateCheckpointSummary`, an agent `TextGenerator`, or
  the external transcript compactor.
- No API, SaaS service, remote LLM endpoint, vector database, or new backend was
  introduced.
- Entire's global summarization, checkpoint persistence, redaction, attribution,
  and `checkpoint explain` behavior remain unchanged.

### Deterministic verification

The reducer implements:

```text
hard FAIL
→ CHANGE BLOCKED

no FAIL + required incomplete evidence
→ VERIFICATION INCOMPLETE

complete required evidence + all conditions PASS
→ CHANGE VERIFIED
```

A supplied `PASS` cannot promote a required historical guarantee when its
checkpoint support is redacted, missing, unreadable, or otherwise
non-authoritative.

## Output

`entire shift analyze` writes:

```text
.shift/change-contract.md
.shift/change-contract.json
```

`entire shift verify` writes:

```text
.shift/verification.md
.shift/verification.json
```

Completeness is normal user-visible output in both formats. Markdown and JSON
are generated from the same typed state.

## Tests and build evidence

Focused tests cover:

- fully populated checkpoint → `COMPLETE`;
- missing transcript → `PARTIAL_MISSING`;
- redacted transcript → `PARTIAL_REDACTED`;
- missing metadata remains useful and partial;
- metadata read failure → `UNREADABLE`;
- unreadable checkpoint → `UNAVAILABLE`;
- redacted raw content does not appear in artifacts;
- unsupported historical claims cannot become `EXPLICIT`;
- explicit claims retain provenance;
- incomplete required evidence → `VERIFICATION INCOMPLETE`;
- hard invariant failure → `CHANGE BLOCKED`;
- failing test evidence → `CHANGE BLOCKED`;
- complete passing evidence → `CHANGE VERIFIED`;
- Markdown and JSON expose the same completeness state;
- root command registration for `shift`, `analyze`, and `verify`.

The organiser-provided redacted fixture was not present in the workspace. The
tests therefore use a clearly scoped synthetic checkpoint-reader fixture that
matches Entire's checkpoint interfaces. The real pre-curveball checkpoint also
provided an end-to-end redacted-data demonstration.

Results:

```text
PASS  SHIFT-focused cmd/entire/cli tests
PASS  gofmt on changed Go files
PASS  go build ./cmd/entire
PASS  live `entire shift --help`
```

Fresh focused test command and result:

```text
go test -count=1 ./cmd/entire/cli -run \
  'Shift|NormalizeShiftEvidence|BuildShiftContract|VerifyShiftContract|RootRegistersShiftCommands'

ok  github.com/entireio/cli/cmd/entire/cli  0.239s
```

Build command:

```text
go build ./cmd/entire
```

The broad existing `cmd/entire/cli` suite was attempted. It reached its
10-minute timeout with widespread Windows temporary-directory lock cleanup
errors in unrelated legacy tests. The focused SHIFT tests and CLI build passed.

## End-to-end privacy demonstration

Analyze was run against the real pre-curveball checkpoint:

```text
entire shift analyze \
  --checkpoint 01M1TQJ0G9VWM44XTQTATJGV5Q \
  --requirement-file <curveball-requirement> \
  --output-dir <temporary-output>
```

Observed result:

```text
Checkpoint Context: PARTIAL_REDACTED
Metadata:            AVAILABLE
Prompt:              AVAILABLE
Transcript:          REDACTED
INV-01:              UNVERIFIED
```

The structured artifact did not contain the raw checkpoint prompt.

Verify was then invoked with requirement, structural, test, and invariant
inputs all supplied as `PASS`. Because the required historical evidence was
redacted, SHIFT safely returned:

```text
VERIFICATION INCOMPLETE
```

It did not return `CHANGE VERIFIED`.

## Final Entire Graph semantic diff

After implementation, the required semantic diff was run:

```text
entire graph diff \
  --base 5e948e6d15127b31d49a06b66d6e019f61e66a10 \
  --head HEAD
```

Graph identified:

- `NewRootCmd` body changed in `cmd/entire/cli/root.go`, with 103 heuristic
  dependents;
- the new SHIFT evidence, contract, provenance, rendering, and verification
  symbols in `cmd/entire/cli/shift.go`;
- the new focused fixtures and tests in `cmd/entire/cli/shift_test.go`.

The `NewRootCmd` dependent count was treated as risk evidence, not proof of a
break. It was checked with the explicit root-registration test and a successful
CLI build. The diff showed no semantic changes to checkpoint persistence,
redaction, attribution, or existing explain behavior.

## Files changed in the submitted implementation

```text
cmd/entire/cli/root.go
cmd/entire/cli/shift.go
cmd/entire/cli/shift_test.go
```

This evidence document is prepared separately and does not alter the submitted
implementation behavior.

## Known limitations

- The MVP evaluates the latest session in the selected checkpoint.
- Redaction detection recognizes explicit redaction markers in locally read
  content; it does not replace Entire's redaction engine.
- Only persisted, available summary intent can currently create an authoritative
  historical invariant.
- Requirement, structural, test, and invariant results are explicit inputs to
  `shift verify`; SHIFT does not execute Graph or test commands itself.
- Graph was used as required engineering evidence before and after the change,
  but the MVP does not embed or persist raw Graph results in the contract.
- The available checkpoint history contains two checkpoint IDs covering the
  pre-curveball and final states rather than four distinct checkpoint IDs.
- Seven Codex hooks still require UI approval; the final session was explicitly
  attached and its checkpoint successfully pushed despite that local warning.

## Submission links

- Branch: <https://github.com/aviraj-singh1/cli/tree/codex/shift-pre-curveball>
- Implementation commit: <https://github.com/aviraj-singh1/cli/commit/b3b683b798c22a465180e97fc072c1e7521eb017>

## Final positioning

**Git tells you what changed. Entire tells you why it changed. SHIFT tells you
what must survive the next change.**
