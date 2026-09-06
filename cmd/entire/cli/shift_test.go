package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shiftEvidenceReaderStub struct {
	summary     *checkpoint.CheckpointSummary
	summaryErr  error
	metadata    *checkpoint.Metadata
	metadataErr error
	prompts     string
	content     *checkpoint.SessionContent
	contentErr  error
}

func (s shiftEvidenceReaderStub) Read(context.Context, id.CheckpointID) (*checkpoint.CheckpointSummary, error) {
	return s.summary, s.summaryErr
}

func (s shiftEvidenceReaderStub) List(context.Context) ([]checkpoint.CheckpointInfo, error) {
	return nil, nil
}

func (s shiftEvidenceReaderStub) ReadSessionContent(context.Context, id.CheckpointID, int) (*checkpoint.SessionContent, error) {
	return s.content, s.contentErr
}

func (s shiftEvidenceReaderStub) ReadSessionMetadata(context.Context, id.CheckpointID, int) (*checkpoint.Metadata, error) {
	return s.metadata, s.metadataErr
}

func (s shiftEvidenceReaderStub) ReadSessionPrompts(context.Context, id.CheckpointID, int) (string, error) {
	return s.prompts, s.metadataErr
}

func (s shiftEvidenceReaderStub) ReadSessionMetadataAndPrompts(context.Context, id.CheckpointID, int) (*checkpoint.Metadata, string, error) {
	return s.metadata, s.prompts, s.metadataErr
}

func completeShiftReader() shiftEvidenceReaderStub {
	metadata := &checkpoint.Metadata{
		SessionID: "session-1",
		Summary:   &checkpoint.Summary{Intent: "Preserve trusted refresh-token behavior."},
	}
	return shiftEvidenceReaderStub{
		summary: &checkpoint.CheckpointSummary{
			FilesTouched: []string{"auth/token.go", "auth/session.go"},
			Sessions:     []checkpoint.SessionFilePaths{{Metadata: "metadata.json", Transcript: "full.jsonl", Prompt: "prompt.txt"}},
		},
		metadata: metadata,
		prompts:  "Add refresh-token support for trusted clients.",
		content: &checkpoint.SessionContent{
			Metadata:   *metadata,
			Prompts:    "Add refresh-token support for trusted clients.",
			Transcript: []byte(`{"type":"message","content":"implemented trusted refresh flow"}`),
		},
	}
}

func TestNormalizeShiftEvidence(t *testing.T) {
	cpID := id.MustCheckpointID("abcdef123456")
	tests := []struct {
		name       string
		mutate     func(*shiftEvidenceReaderStub)
		want       shiftContextStatus
		metadata   shiftFieldAvailability
		prompt     shiftFieldAvailability
		transcript shiftFieldAvailability
	}{
		{
			name:       "fully populated checkpoint is complete",
			want:       shiftContextComplete,
			metadata:   shiftFieldAvailable,
			prompt:     shiftFieldAvailable,
			transcript: shiftFieldAvailable,
		},
		{
			name: "missing transcript is partial missing",
			mutate: func(reader *shiftEvidenceReaderStub) {
				reader.content = nil
				reader.contentErr = checkpoint.ErrNoTranscript
			},
			want:       shiftContextPartialMissing,
			metadata:   shiftFieldAvailable,
			prompt:     shiftFieldAvailable,
			transcript: shiftFieldMissing,
		},
		{
			name: "redacted transcript is partial redacted",
			mutate: func(reader *shiftEvidenceReaderStub) {
				reader.content.Transcript = []byte(`{"content":"REDACTED"}`)
			},
			want:       shiftContextPartialRedacted,
			metadata:   shiftFieldAvailable,
			prompt:     shiftFieldAvailable,
			transcript: shiftFieldRedacted,
		},
		{
			name: "missing metadata remains useful but partial",
			mutate: func(reader *shiftEvidenceReaderStub) {
				reader.metadata = nil
			},
			want:       shiftContextPartialMissing,
			metadata:   shiftFieldMissing,
			prompt:     shiftFieldAvailable,
			transcript: shiftFieldAvailable,
		},
		{
			name: "metadata read failure is unreadable",
			mutate: func(reader *shiftEvidenceReaderStub) {
				reader.metadata = nil
				reader.metadataErr = errors.New("metadata blob unreadable")
			},
			want:       shiftContextPartialMissing,
			metadata:   shiftFieldUnreadable,
			prompt:     shiftFieldUnreadable,
			transcript: shiftFieldAvailable,
		},
		{
			name: "unreadable checkpoint is unavailable",
			mutate: func(reader *shiftEvidenceReaderStub) {
				reader.summary = nil
				reader.summaryErr = errors.New("checkpoint unavailable")
			},
			want:       shiftContextUnavailable,
			metadata:   shiftFieldUnreadable,
			prompt:     shiftFieldUnreadable,
			transcript: shiftFieldUnreadable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := completeShiftReader()
			if tt.mutate != nil {
				tt.mutate(&reader)
			}
			evidence := normalizeShiftEvidence(t.Context(), reader, cpID)
			assert.Equal(t, tt.want, evidence.Context)
			assert.Equal(t, tt.metadata, evidence.Metadata.Availability)
			assert.Equal(t, tt.prompt, evidence.Prompt.Availability)
			assert.Equal(t, tt.transcript, evidence.Transcript.Availability)
		})
	}
}

func TestBuildShiftContractQualifiesRedactedEvidenceWithoutLeakingRawContent(t *testing.T) {
	reader := completeShiftReader()
	reader.prompts = "password=REDACTED"
	reader.content.Prompts = reader.prompts
	reader.content.Transcript = []byte(`{"content":"token REDACTED"}`)
	reader.metadata.Summary = nil

	evidence := normalizeShiftEvidence(t.Context(), reader, id.MustCheckpointID("abcdef123456"))
	contract := buildShiftContract("Add Google OAuth.", evidence)

	require.Equal(t, shiftContextPartialRedacted, contract.Evidence.Context)
	require.Len(t, contract.Invariants, 1)
	assert.Equal(t, shiftClaimUnverified, contract.Invariants[0].EvidenceStatus)
	assert.Nil(t, contract.Invariants[0].Provenance)
	assert.Contains(t, strings.Join(contract.ImplementationInstructions, "\n"), "MUST CONFIRM INV-01")

	encoded, err := json.Marshal(contract)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "password=")
	assert.NotContains(t, string(encoded), "token REDACTED")
	assert.Contains(t, string(encoded), `"context_status":"PARTIAL_REDACTED"`)
}

func TestBuildShiftContractMakesAvailableSummaryExplicitWithProvenance(t *testing.T) {
	evidence := normalizeShiftEvidence(t.Context(), completeShiftReader(), id.MustCheckpointID("abcdef123456"))
	contract := buildShiftContract("Add Google OAuth.", evidence)

	require.Len(t, contract.Invariants, 1)
	invariant := contract.Invariants[0]
	assert.Equal(t, "INV-01", invariant.ID)
	assert.Equal(t, shiftClaimExplicit, invariant.EvidenceStatus)
	require.NotNil(t, invariant.Provenance)
	assert.Equal(t, "metadata.summary.intent", invariant.Provenance.Field)
}

func TestVerifyShiftContractDeterministicOutcomes(t *testing.T) {
	completeEvidence := normalizeShiftEvidence(t.Context(), completeShiftReader(), id.MustCheckpointID("abcdef123456"))
	completeContract := buildShiftContract("Add Google OAuth.", completeEvidence)

	partialReader := completeShiftReader()
	partialReader.content = nil
	partialReader.contentErr = checkpoint.ErrNoTranscript
	partialContract := buildShiftContract("Add Google OAuth.", normalizeShiftEvidence(t.Context(), partialReader, id.MustCheckpointID("abcdef123456")))

	tests := []struct {
		name        string
		contract    shiftContract
		overrides   map[string]shiftAssertionStatus
		requirement shiftAssertionStatus
		structural  shiftAssertionStatus
		tests       shiftAssertionStatus
		want        shiftOverallStatus
	}{
		{
			name:        "required missing evidence is incomplete",
			contract:    partialContract,
			overrides:   map[string]shiftAssertionStatus{"INV-01": shiftAssertionPass},
			requirement: shiftAssertionPass,
			structural:  shiftAssertionPass,
			tests:       shiftAssertionPass,
			want:        shiftVerificationIncomplete,
		},
		{
			name:        "hard invariant failure blocks",
			contract:    completeContract,
			overrides:   map[string]shiftAssertionStatus{"INV-01": shiftAssertionFail},
			requirement: shiftAssertionPass,
			structural:  shiftAssertionPass,
			tests:       shiftAssertionPass,
			want:        shiftChangeBlocked,
		},
		{
			name:        "hard invariant failure remains blocking with partial evidence",
			contract:    partialContract,
			overrides:   map[string]shiftAssertionStatus{"INV-01": shiftAssertionFail},
			requirement: shiftAssertionPass,
			structural:  shiftAssertionPass,
			tests:       shiftAssertionPass,
			want:        shiftChangeBlocked,
		},
		{
			name:        "failing tests block verification",
			contract:    completeContract,
			overrides:   map[string]shiftAssertionStatus{"INV-01": shiftAssertionPass},
			requirement: shiftAssertionPass,
			structural:  shiftAssertionPass,
			tests:       shiftAssertionFail,
			want:        shiftChangeBlocked,
		},
		{
			name:        "complete passing evidence verifies",
			contract:    completeContract,
			overrides:   map[string]shiftAssertionStatus{"INV-01": shiftAssertionPass},
			requirement: shiftAssertionPass,
			structural:  shiftAssertionPass,
			tests:       shiftAssertionPass,
			want:        shiftChangeVerified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verifyShiftContract(tt.contract, tt.overrides, tt.requirement, tt.structural, tt.tests)
			assert.Equal(t, tt.want, result.Overall)
		})
	}
}

func TestShiftMarkdownAndJSONUseSameCompletenessState(t *testing.T) {
	reader := completeShiftReader()
	reader.content.Transcript = []byte(`{"content":"REDACTED"}`)
	contract := buildShiftContract("Add Google OAuth.", normalizeShiftEvidence(t.Context(), reader, id.MustCheckpointID("abcdef123456")))

	markdown := renderShiftContractMarkdown(contract)
	data, err := json.Marshal(contract)
	require.NoError(t, err)

	assert.Contains(t, markdown, "Checkpoint Context: **PARTIAL_REDACTED**")
	assert.Contains(t, string(data), `"context_status":"PARTIAL_REDACTED"`)
}

func TestWriteShiftContractProducesUsefulArtifactsWithoutRawEvidence(t *testing.T) {
	reader := completeShiftReader()
	reader.prompts = "private prompt body"
	reader.content.Prompts = reader.prompts
	contract := buildShiftContract("Add Google OAuth.", normalizeShiftEvidence(t.Context(), reader, id.MustCheckpointID("abcdef123456")))
	outDir := t.TempDir()

	require.NoError(t, writeShiftContract(outDir, contract))
	markdown, err := os.ReadFile(filepath.Join(outDir, "change-contract.md"))
	require.NoError(t, err)
	structured, err := os.ReadFile(filepath.Join(outDir, "change-contract.json"))
	require.NoError(t, err)

	for _, artifact := range [][]byte{markdown, structured} {
		assert.Contains(t, string(artifact), "Add Google OAuth")
		assert.NotContains(t, string(artifact), "private prompt body")
	}
}

func TestRootRegistersShiftCommands(t *testing.T) {
	root := NewRootCmd()
	shift, _, err := root.Find([]string{"shift"})
	require.NoError(t, err)
	assert.Equal(t, "shift", shift.Name())
	assert.NotNil(t, findSubcommand(shift, "analyze"))
	assert.NotNil(t, findSubcommand(shift, "verify"))
}

func findSubcommand(parent interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	for _, command := range parent.Commands() {
		if command.Name() == name {
			return command
		}
	}
	return nil
}
