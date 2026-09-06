package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/mdrender"
	"github.com/spf13/cobra"
)

const shiftContractVersion = "1"

type shiftFieldAvailability string

const (
	shiftFieldAvailable  shiftFieldAvailability = "AVAILABLE"
	shiftFieldRedacted   shiftFieldAvailability = "REDACTED"
	shiftFieldMissing    shiftFieldAvailability = "MISSING"
	shiftFieldUnreadable shiftFieldAvailability = "UNREADABLE"
)

type shiftContextStatus string

const (
	shiftContextComplete        shiftContextStatus = "COMPLETE"
	shiftContextPartialRedacted shiftContextStatus = "PARTIAL_REDACTED"
	shiftContextPartialMissing  shiftContextStatus = "PARTIAL_MISSING"
	shiftContextUnavailable     shiftContextStatus = "UNAVAILABLE"
)

type shiftClaimStatus string

const (
	shiftClaimExplicit   shiftClaimStatus = "EXPLICIT"
	shiftClaimInferred   shiftClaimStatus = "INFERRED"
	shiftClaimUnverified shiftClaimStatus = "UNVERIFIED"
)

type shiftAssertionStatus string

const (
	shiftAssertionPass       shiftAssertionStatus = "PASS"
	shiftAssertionFail       shiftAssertionStatus = "FAIL"
	shiftAssertionUnverified shiftAssertionStatus = "UNVERIFIED"
)

type shiftOverallStatus string

const (
	shiftChangeVerified         shiftOverallStatus = "CHANGE VERIFIED"
	shiftChangeBlocked          shiftOverallStatus = "CHANGE BLOCKED"
	shiftVerificationIncomplete shiftOverallStatus = "VERIFICATION INCOMPLETE"
)

type shiftEvidenceField struct {
	Availability shiftFieldAvailability `json:"availability"`
	Reason       string                 `json:"reason,omitempty"`
}

type shiftEvidence struct {
	CheckpointID string             `json:"checkpoint_id"`
	SessionID    string             `json:"session_id,omitempty"`
	Context      shiftContextStatus `json:"context_status"`
	Metadata     shiftEvidenceField `json:"metadata"`
	Prompt       shiftEvidenceField `json:"prompt"`
	Transcript   shiftEvidenceField `json:"transcript"`
	FilesTouched []string           `json:"files_touched,omitempty"`

	// safeIntent is deliberately not serialized. It is populated only from an
	// available, non-redacted persisted summary — never from raw prompts or a
	// transcript — and is copied into the contract's historical intent.
	safeIntent string
}

type shiftProvenance struct {
	CheckpointID string `json:"checkpoint_id"`
	SessionID    string `json:"session_id,omitempty"`
	Field        string `json:"field"`
}

type shiftInvariant struct {
	ID                      string           `json:"id"`
	Statement               string           `json:"statement"`
	EvidenceStatus          shiftClaimStatus `json:"evidence_status"`
	RequiredForVerification bool             `json:"required_for_verification"`
	Reason                  string           `json:"reason,omitempty"`
	Provenance              *shiftProvenance `json:"provenance,omitempty"`
}

type shiftContract struct {
	Version                    string           `json:"version"`
	Requirement                string           `json:"requirement"`
	Evidence                   shiftEvidence    `json:"evidence"`
	HistoricalIntent           string           `json:"historical_intent,omitempty"`
	Invariants                 []shiftInvariant `json:"invariants"`
	ImplementationInstructions []string         `json:"implementation_instructions"`
	VerificationPlan           []string         `json:"verification_plan"`
}

type shiftInvariantResult struct {
	ID     string               `json:"id"`
	Status shiftAssertionStatus `json:"status"`
	Reason string               `json:"reason,omitempty"`
}

type shiftVerification struct {
	Overall           shiftOverallStatus     `json:"overall_status"`
	Context           shiftContextStatus     `json:"context_status"`
	RequirementStatus shiftAssertionStatus   `json:"requirement_status"`
	StructuralStatus  shiftAssertionStatus   `json:"structural_status"`
	TestStatus        shiftAssertionStatus   `json:"test_status"`
	Invariants        []shiftInvariantResult `json:"invariants"`
}

type shiftEvidenceReader interface {
	checkpoint.CheckpointReader
	checkpoint.SessionReader
}

func newShiftCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shift",
		Short: "Control requirement changes with checkpoint-backed contracts",
		Long: `SHIFT creates a pre-change contract from local checkpoint evidence and
verifies the resulting change without treating missing or redacted history as
an authoritative guarantee. Raw prompts and transcripts remain local.`,
	}
	cmd.AddCommand(newShiftAnalyzeCmd())
	cmd.AddCommand(newShiftVerifyCmd())
	return cmd
}

func newShiftAnalyzeCmd() *cobra.Command {
	var checkpointFlag string
	var requirementFile string
	var outputDir string

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Generate a checkpoint-backed Change Contract",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShiftAnalyze(cmd.Context(), cmd.OutOrStdout(), checkpointFlag, requirementFile, outputDir)
		},
	}
	cmd.Flags().StringVar(&checkpointFlag, "checkpoint", "", "Checkpoint ID containing historical evidence (required)")
	cmd.Flags().StringVar(&requirementFile, "requirement-file", "", "File containing the new requirement (required)")
	cmd.Flags().StringVar(&outputDir, "output-dir", ".shift", "Directory for Change Contract artifacts")
	_ = cmd.MarkFlagRequired("checkpoint")
	_ = cmd.MarkFlagRequired("requirement-file")
	return cmd
}

func newShiftVerifyCmd() *cobra.Command {
	var contractPath string
	var outputDir string
	var invariantFlags []string
	var requirementFlag string
	var structuralFlag string
	var testFlag string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a change against its Change Contract",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShiftVerify(cmd.OutOrStdout(), contractPath, outputDir, invariantFlags, requirementFlag, structuralFlag, testFlag)
		},
	}
	cmd.Flags().StringVar(&contractPath, "contract", filepath.Join(".shift", "change-contract.json"), "Change Contract JSON file")
	cmd.Flags().StringVar(&outputDir, "output-dir", ".shift", "Directory for verification artifacts")
	cmd.Flags().StringSliceVar(&invariantFlags, "invariant", nil, "Invariant result as INV-01=PASS|FAIL|UNVERIFIED (repeatable)")
	cmd.Flags().StringVar(&requirementFlag, "requirement-status", string(shiftAssertionUnverified), "Requirement result: PASS, FAIL, or UNVERIFIED")
	cmd.Flags().StringVar(&structuralFlag, "structural-status", string(shiftAssertionUnverified), "Structural evidence result: PASS, FAIL, or UNVERIFIED")
	cmd.Flags().StringVar(&testFlag, "test-status", string(shiftAssertionUnverified), "Test result: PASS, FAIL, or UNVERIFIED")
	return cmd
}

func runShiftAnalyze(ctx context.Context, w io.Writer, checkpointFlag, requirementFile, outputDir string) error {
	cpID, err := id.NewCheckpointID(strings.TrimSpace(checkpointFlag))
	if err != nil {
		return err
	}
	requirementBytes, err := os.ReadFile(requirementFile)
	if err != nil {
		return fmt.Errorf("read requirement file: %w", err)
	}
	requirement := strings.TrimSpace(string(requirementBytes))
	if requirement == "" {
		return errors.New("requirement file is empty")
	}

	lookup, err := newExplainCheckpointLookup(ctx)
	if err != nil {
		return err
	}
	defer lookup.Close()

	evidence := normalizeShiftEvidence(ctx, lookup.store, cpID)
	contract := buildShiftContract(requirement, evidence)
	if err := writeShiftContract(outputDir, contract); err != nil {
		return err
	}

	markdown := renderShiftContractMarkdown(contract)
	rendered, renderErr := mdrender.RenderForWriter(w, markdown)
	if renderErr != nil {
		rendered = markdown
	}
	_, err = fmt.Fprint(w, rendered)
	return err
}

func runShiftVerify(w io.Writer, contractPath, outputDir string, invariantFlags []string, requirementFlag, structuralFlag, testFlag string) error {
	contractBytes, err := os.ReadFile(contractPath)
	if err != nil {
		return fmt.Errorf("read Change Contract: %w", err)
	}
	var contract shiftContract
	if err := json.Unmarshal(contractBytes, &contract); err != nil {
		return fmt.Errorf("parse Change Contract: %w", err)
	}

	overrides, err := parseShiftInvariantResults(invariantFlags)
	if err != nil {
		return err
	}
	requirementStatus, err := parseShiftAssertionStatus(requirementFlag)
	if err != nil {
		return fmt.Errorf("requirement status: %w", err)
	}
	structuralStatus, err := parseShiftAssertionStatus(structuralFlag)
	if err != nil {
		return fmt.Errorf("structural status: %w", err)
	}
	testStatus, err := parseShiftAssertionStatus(testFlag)
	if err != nil {
		return fmt.Errorf("test status: %w", err)
	}

	result := verifyShiftContract(contract, overrides, requirementStatus, structuralStatus, testStatus)
	if err := writeShiftVerification(outputDir, result); err != nil {
		return err
	}
	markdown := renderShiftVerificationMarkdown(result)
	rendered, renderErr := mdrender.RenderForWriter(w, markdown)
	if renderErr != nil {
		rendered = markdown
	}
	_, err = fmt.Fprint(w, rendered)
	return err
}

func normalizeShiftEvidence(ctx context.Context, reader shiftEvidenceReader, checkpointID id.CheckpointID) shiftEvidence {
	evidence := shiftEvidence{
		CheckpointID: checkpointID.String(),
		Metadata:     shiftEvidenceField{Availability: shiftFieldUnreadable},
		Prompt:       shiftEvidenceField{Availability: shiftFieldUnreadable},
		Transcript:   shiftEvidenceField{Availability: shiftFieldUnreadable},
	}

	summary, err := checkpoint.ReadCheckpoint(ctx, reader, checkpointID)
	if err != nil {
		reason := err.Error()
		evidence.Metadata.Reason = reason
		evidence.Prompt.Reason = reason
		evidence.Transcript.Reason = reason
		evidence.Context = shiftContextUnavailable
		return evidence
	}
	evidence.FilesTouched = append([]string(nil), summary.FilesTouched...)
	sort.Strings(evidence.FilesTouched)
	if len(summary.Sessions) == 0 {
		reason := "checkpoint contains no session records"
		evidence.Metadata = shiftEvidenceField{Availability: shiftFieldMissing, Reason: reason}
		evidence.Prompt = shiftEvidenceField{Availability: shiftFieldMissing, Reason: reason}
		evidence.Transcript = shiftEvidenceField{Availability: shiftFieldMissing, Reason: reason}
		evidence.Context = shiftContextUnavailable
		return evidence
	}

	latest := len(summary.Sessions) - 1
	metadata, prompts, metadataErr := reader.ReadSessionMetadataAndPrompts(ctx, checkpointID, latest)
	if metadataErr != nil {
		evidence.Metadata = shiftEvidenceField{Availability: shiftFieldUnreadable, Reason: metadataErr.Error()}
		evidence.Prompt = shiftEvidenceField{Availability: shiftFieldUnreadable, Reason: metadataErr.Error()}
	} else {
		if metadata == nil {
			evidence.Metadata = shiftEvidenceField{Availability: shiftFieldMissing, Reason: "checkpoint session metadata is absent"}
		} else {
			evidence.SessionID = metadata.SessionID
			evidence.Metadata = availableShiftField("session metadata is available locally")
			if metadata.Summary != nil && containsShiftRedactionMarker(metadata.Summary.Intent) {
				evidence.Metadata = shiftEvidenceField{Availability: shiftFieldRedacted, Reason: "metadata summary contains redacted content"}
			}
			if metadata.Summary != nil && evidence.Metadata.Availability == shiftFieldAvailable {
				intent := strings.TrimSpace(metadata.Summary.Intent)
				if intent != "" && !containsShiftRedactionMarker(intent) {
					evidence.safeIntent = intent
				}
			}
		}

		switch {
		case strings.TrimSpace(prompts) == "":
			evidence.Prompt = shiftEvidenceField{Availability: shiftFieldMissing, Reason: "checkpoint prompt is absent"}
		case containsShiftRedactionMarker(prompts):
			evidence.Prompt = shiftEvidenceField{Availability: shiftFieldRedacted, Reason: "checkpoint prompt contains redacted content; raw value remains local"}
		default:
			evidence.Prompt = availableShiftField("checkpoint prompt is available locally; raw value is not included in the contract")
		}
	}

	content, contentErr := reader.ReadSessionContent(ctx, checkpointID, latest)
	switch {
	case contentErr != nil && isShiftMissingTranscriptError(contentErr):
		evidence.Transcript = shiftEvidenceField{Availability: shiftFieldMissing, Reason: contentErr.Error()}
	case contentErr != nil:
		evidence.Transcript = shiftEvidenceField{Availability: shiftFieldUnreadable, Reason: contentErr.Error()}
	case content == nil || len(content.Transcript) == 0:
		evidence.Transcript = shiftEvidenceField{Availability: shiftFieldMissing, Reason: "checkpoint transcript is absent"}
	case containsShiftRedactionMarker(string(content.Transcript)):
		evidence.Transcript = shiftEvidenceField{Availability: shiftFieldRedacted, Reason: "checkpoint transcript contains redacted content; raw value remains local"}
	default:
		evidence.Transcript = availableShiftField("checkpoint transcript is available locally; raw value is not included in the contract")
	}

	evidence.Context = aggregateShiftContext(evidence.Metadata, evidence.Prompt, evidence.Transcript)
	return evidence
}

func availableShiftField(reason string) shiftEvidenceField {
	return shiftEvidenceField{Availability: shiftFieldAvailable, Reason: reason}
}

func containsShiftRedactionMarker(value string) bool {
	upper := strings.ToUpper(value)
	return strings.Contains(upper, "REDACTED") || strings.Contains(upper, "<REDACTED")
}

func isShiftMissingTranscriptError(err error) bool {
	return errors.Is(err, checkpoint.ErrNoTranscript) || strings.Contains(strings.ToLower(err.Error()), "no stored transcript") || strings.Contains(strings.ToLower(err.Error()), "no transcript")
}

func aggregateShiftContext(fields ...shiftEvidenceField) shiftContextStatus {
	available := 0
	redacted := false
	incomplete := false
	for _, field := range fields {
		switch field.Availability {
		case shiftFieldAvailable:
			available++
		case shiftFieldRedacted:
			redacted = true
			incomplete = true
		case shiftFieldMissing, shiftFieldUnreadable:
			incomplete = true
		}
	}
	if !incomplete {
		return shiftContextComplete
	}
	if redacted {
		return shiftContextPartialRedacted
	}
	if available == 0 {
		return shiftContextUnavailable
	}
	return shiftContextPartialMissing
}

func buildShiftContract(requirement string, evidence shiftEvidence) shiftContract {
	invariant := shiftInvariant{
		ID:                      "INV-01",
		Statement:               "Preserve checkpoint-backed behavior unless the new requirement explicitly changes it.",
		EvidenceStatus:          shiftClaimUnverified,
		RequiredForVerification: true,
		Reason:                  "no inspectable historical intent is available; this is not an authoritative MUST PRESERVE guarantee",
	}
	historicalIntent := "Historical intent is incomplete and requires developer confirmation."
	if evidence.safeIntent != "" && evidence.Metadata.Availability == shiftFieldAvailable {
		historicalIntent = evidence.safeIntent
		invariant.Statement = evidence.safeIntent
		invariant.EvidenceStatus = shiftClaimExplicit
		invariant.Reason = "supported by an available persisted checkpoint summary"
		invariant.Provenance = &shiftProvenance{
			CheckpointID: evidence.CheckpointID,
			SessionID:    evidence.SessionID,
			Field:        "metadata.summary.intent",
		}
	}

	instructions := []string{
		"Implement the new requirement.",
		"Keep raw checkpoint prompts and transcripts local.",
		"Verify Graph findings against source code and tests.",
	}
	if invariant.EvidenceStatus == shiftClaimExplicit {
		instructions = append(instructions, "MUST PRESERVE "+invariant.ID+": "+invariant.Statement)
	} else {
		instructions = append(instructions, "MUST CONFIRM "+invariant.ID+" before treating it as a guarantee: "+invariant.Reason)
	}

	return shiftContract{
		Version:                    shiftContractVersion,
		Requirement:                requirement,
		Evidence:                   evidence,
		HistoricalIntent:           historicalIntent,
		Invariants:                 []shiftInvariant{invariant},
		ImplementationInstructions: instructions,
		VerificationPlan: []string{
			"Verify the new requirement.",
			"Verify every required historical invariant with inspectable evidence.",
			"Verify structural findings against source and tests.",
		},
	}
}

func verifyShiftContract(contract shiftContract, overrides map[string]shiftAssertionStatus, requirementStatus, structuralStatus, testStatus shiftAssertionStatus) shiftVerification {
	result := shiftVerification{
		Overall:           shiftChangeVerified,
		Context:           contract.Evidence.Context,
		RequirementStatus: requirementStatus,
		StructuralStatus:  structuralStatus,
		TestStatus:        testStatus,
	}
	hardFailure := requirementStatus == shiftAssertionFail || structuralStatus == shiftAssertionFail || testStatus == shiftAssertionFail
	incomplete := requirementStatus != shiftAssertionPass || structuralStatus != shiftAssertionPass || testStatus != shiftAssertionPass

	for _, invariant := range contract.Invariants {
		status := shiftAssertionUnverified
		reason := "verification result was not supplied"
		if override, ok := overrides[invariant.ID]; ok {
			status = override
			reason = ""
		}
		if invariant.EvidenceStatus != shiftClaimExplicit && status != shiftAssertionFail {
			status = shiftAssertionUnverified
			reason = "supporting historical evidence is not authoritative"
		}
		if invariant.RequiredForVerification && status == shiftAssertionFail {
			hardFailure = true
		}
		if invariant.RequiredForVerification && (status != shiftAssertionPass || contract.Evidence.Context != shiftContextComplete) {
			incomplete = true
			if status == shiftAssertionPass && contract.Evidence.Context != shiftContextComplete {
				status = shiftAssertionUnverified
				reason = "required checkpoint context is " + string(contract.Evidence.Context)
			}
		}
		result.Invariants = append(result.Invariants, shiftInvariantResult{ID: invariant.ID, Status: status, Reason: reason})
	}

	switch {
	case hardFailure:
		result.Overall = shiftChangeBlocked
	case incomplete:
		result.Overall = shiftVerificationIncomplete
	default:
		result.Overall = shiftChangeVerified
	}
	return result
}

func parseShiftInvariantResults(values []string) (map[string]shiftAssertionStatus, error) {
	results := make(map[string]shiftAssertionStatus, len(values))
	for _, value := range values {
		idValue, statusValue, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(idValue) == "" {
			return nil, fmt.Errorf("invalid invariant result %q: expected INV-01=PASS|FAIL|UNVERIFIED", value)
		}
		status, err := parseShiftAssertionStatus(statusValue)
		if err != nil {
			return nil, fmt.Errorf("invariant %s: %w", strings.TrimSpace(idValue), err)
		}
		results[strings.TrimSpace(idValue)] = status
	}
	return results, nil
}

func parseShiftAssertionStatus(value string) (shiftAssertionStatus, error) {
	status := shiftAssertionStatus(strings.ToUpper(strings.TrimSpace(value)))
	switch status {
	case shiftAssertionPass, shiftAssertionFail, shiftAssertionUnverified:
		return status, nil
	default:
		return "", fmt.Errorf("invalid status %q: expected PASS, FAIL, or UNVERIFIED", value)
	}
}

func writeShiftContract(outputDir string, contract shiftContract) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create SHIFT output directory: %w", err)
	}
	markdown := renderShiftContractMarkdown(contract)
	if err := os.WriteFile(filepath.Join(outputDir, "change-contract.md"), []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write Change Contract Markdown: %w", err)
	}
	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Change Contract JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "change-contract.json"), data, 0o644); err != nil {
		return fmt.Errorf("write Change Contract JSON: %w", err)
	}
	return nil
}

func writeShiftVerification(outputDir string, result shiftVerification) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create SHIFT output directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "verification.md"), []byte(renderShiftVerificationMarkdown(result)), 0o644); err != nil {
		return fmt.Errorf("write SHIFT verification Markdown: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode SHIFT verification JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "verification.json"), data, 0o644); err != nil {
		return fmt.Errorf("write SHIFT verification JSON: %w", err)
	}
	return nil
}

func renderShiftContractMarkdown(contract shiftContract) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# SHIFT Change Contract\n\n## New Requirement\n\n%s\n\n", contract.Requirement)
	b.WriteString("## Evidence Status\n\n")
	fmt.Fprintf(&b, "- Checkpoint Context: **%s**\n", contract.Evidence.Context)
	fmt.Fprintf(&b, "- Metadata: **%s**%s\n", contract.Evidence.Metadata.Availability, shiftReasonSuffix(contract.Evidence.Metadata.Reason))
	fmt.Fprintf(&b, "- Prompt: **%s**%s\n", contract.Evidence.Prompt.Availability, shiftReasonSuffix(contract.Evidence.Prompt.Reason))
	fmt.Fprintf(&b, "- Transcript: **%s**%s\n\n", contract.Evidence.Transcript.Availability, shiftReasonSuffix(contract.Evidence.Transcript.Reason))
	b.WriteString("## Historical Intent\n\n")
	fmt.Fprintf(&b, "%s\n\n", contract.HistoricalIntent)
	b.WriteString("## Historical Invariants\n\n")
	for _, invariant := range contract.Invariants {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n- Evidence: **%s**\n", invariant.ID, invariant.Statement, invariant.EvidenceStatus)
		if invariant.Reason != "" {
			fmt.Fprintf(&b, "- Reason: %s\n", invariant.Reason)
		}
		if invariant.Provenance != nil {
			fmt.Fprintf(&b, "- Provenance: checkpoint `%s`, session `%s`, field `%s`\n", invariant.Provenance.CheckpointID, invariant.Provenance.SessionID, invariant.Provenance.Field)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Implementation Instructions\n\n")
	for _, instruction := range contract.ImplementationInstructions {
		fmt.Fprintf(&b, "- %s\n", instruction)
	}
	b.WriteString("\n## Verification Plan\n\n")
	for _, item := range contract.VerificationPlan {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	return b.String()
}

func renderShiftVerificationMarkdown(result shiftVerification) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", result.Overall)
	fmt.Fprintf(&b, "- Checkpoint Context: **%s**\n", result.Context)
	fmt.Fprintf(&b, "- New Requirement: **%s**\n", result.RequirementStatus)
	fmt.Fprintf(&b, "- Structural Evidence: **%s**\n", result.StructuralStatus)
	fmt.Fprintf(&b, "- Test Evidence: **%s**\n", result.TestStatus)
	for _, invariant := range result.Invariants {
		fmt.Fprintf(&b, "- %s: **%s**%s\n", invariant.ID, invariant.Status, shiftReasonSuffix(invariant.Reason))
	}
	return b.String()
}

func shiftReasonSuffix(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " — " + reason
}
