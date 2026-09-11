package experiment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/datasetcatalog"
)

const maxArtifactReadBytes = 64 << 10

type ArtifactManifestEntry struct {
	Name              string `json:"name"`
	MediaType         string `json:"media_type"`
	SizeBytes         *int64 `json:"size_bytes,omitempty"`
	Checksum          string `json:"checksum,omitempty"`
	Available         bool   `json:"available"`
	Availability      string `json:"availability"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	Readable          bool   `json:"readable"`
}

type ArtifactReadInput struct {
	ExperimentID string `json:"experiment_id" jsonschema:"experiment ID"`
	Name         string `json:"name" jsonschema:"registered artifact filename; no path"`
}

type ArtifactReadView struct {
	ExperimentID      string `json:"experiment_id"`
	Name              string `json:"name"`
	MediaType         string `json:"media_type"`
	SizeBytes         int64  `json:"size_bytes"`
	Checksum          string `json:"checksum"`
	Truncated         bool   `json:"truncated"`
	Available         bool   `json:"available"`
	Availability      string `json:"availability"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	Text              string `json:"text,omitempty"`
	JSON              any    `json:"json,omitempty"`
}

func (s *Service) Artifacts(ctx context.Context, principal agentauth.Principal, experimentID string) (ArtifactView, error) {
	var result ArtifactView
	if !principal.HasScope("read") {
		return result, ErrForbidden
	}
	record, err := s.getRecord(ctx, principal.ProjectID, experimentID)
	if err != nil {
		return result, err
	}
	return s.artifactView(ctx, record)
}

func (s *Service) ReadArtifact(ctx context.Context, principal agentauth.Principal, input ArtifactReadInput) (ArtifactReadView, error) {
	var result ArtifactReadView
	if !principal.HasScope("read") {
		return result, ErrForbidden
	}
	name, err := normalizeArtifactName(input.Name)
	if err != nil {
		return result, err
	}
	record, err := s.getRecord(ctx, principal.ProjectID, strings.TrimSpace(input.ExperimentID))
	if err != nil {
		return result, err
	}
	names, err := s.registeredArtifacts(ctx, record)
	if err != nil {
		return result, err
	}
	if !containsArtifactName(names, name) {
		return result, &ValidationError{Message: "artifact is not registered for this experiment"}
	}
	return readRegisteredArtifact(record, name), nil
}

func (s *Service) artifactView(ctx context.Context, record *ent.Experiment) (ArtifactView, error) {
	names, err := s.registeredArtifacts(ctx, record)
	if err != nil {
		return ArtifactView{}, err
	}
	return ArtifactView{
		ExperimentID: record.PublicID.String(),
		OutputPath:   record.OutputPath,
		Artifacts:    names,
		Manifest:     artifactManifest(record, names),
	}, nil
}

func (s *Service) registeredArtifacts(ctx context.Context, record *ent.Experiment) ([]string, error) {
	isDiagnostic, err := record.QueryDiagnosticRun().Exist(ctx)
	if err != nil {
		return nil, err
	}
	return registeredArtifactNames(record, isDiagnostic), nil
}

func registeredArtifactNames(record *ent.Experiment, isDiagnostic bool) []string {
	artifacts := []string{}
	if strings.HasPrefix(record.OutputPath, "/root/autodl-fs/") && record.ProviderResourceID != nil {
		artifacts = append(artifacts, "gemcp-launch.log")
	}
	if record.StartedAt != nil || record.LogTail != nil {
		artifacts = append(artifacts, "run.log")
	}
	if record.ExitCode != nil {
		artifacts = append(artifacts, "gemcp-result.json")
	}
	if len(record.Metrics) > 0 {
		artifacts = append(artifacts, "metrics.json")
	}
	if isDiagnostic && record.ExitCode != nil {
		artifacts = append(artifacts, "diagnostic-report.txt")
	}
	return artifacts
}

func artifactManifest(record *ent.Experiment, names []string) []ArtifactManifestEntry {
	result := make([]ArtifactManifestEntry, 0, len(names))
	for _, name := range names {
		entry := ArtifactManifestEntry{Name: name, MediaType: artifactMediaType(name)}
		if content, ok := artifactControlPlaneContent(record, name); ok {
			payload := boundArtifactBytes(content)
			size := int64(len(content))
			entry.Available = true
			entry.Readable = true
			entry.Availability = "control_plane"
			entry.SizeBytes = &size
			entry.Checksum = artifactChecksum(payload)
		} else {
			entry.Availability = "shared_storage"
			entry.UnavailableReason = artifactUnavailableReason(name)
		}
		result = append(result, entry)
	}
	return result
}

func readRegisteredArtifact(record *ent.Experiment, name string) ArtifactReadView {
	view := ArtifactReadView{
		ExperimentID: record.PublicID.String(),
		Name:         name,
		MediaType:    artifactMediaType(name),
		Availability: "shared_storage",
	}
	content, ok := artifactControlPlaneContent(record, name)
	if !ok {
		view.UnavailableReason = artifactUnavailableReason(name)
		return view
	}
	payload := boundArtifactBytes(content)
	view.Available = true
	view.Availability = "control_plane"
	view.SizeBytes = int64(len(content))
	view.Truncated = len(payload) < len(content)
	view.Checksum = artifactChecksum(payload)
	if view.MediaType == "application/json" {
		var decoded any
		if err := json.Unmarshal(payload, &decoded); err == nil {
			view.JSON = decoded
			return view
		}
	}
	view.Text = string(payload)
	return view
}

func artifactControlPlaneContent(record *ent.Experiment, name string) ([]byte, bool) {
	switch name {
	case "run.log":
		if record.LogTail == nil {
			return nil, false
		}
		return []byte(*record.LogTail), true
	case "metrics.json":
		if len(record.Metrics) == 0 {
			return nil, false
		}
		encoded, err := json.Marshal(record.Metrics)
		if err != nil {
			return nil, false
		}
		return encoded, true
	case "gemcp-result.json":
		if record.ExitCode == nil {
			return nil, false
		}
		payload := map[string]any{
			"experiment_id": record.PublicID.String(),
			"exit_code":     *record.ExitCode,
			"reason":        artifactResultReason(record),
		}
		if record.FailureCode != nil {
			payload["failure_code"] = *record.FailureCode
		}
		if record.FailureReason != nil {
			payload["failure_reason"] = *record.FailureReason
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, false
		}
		return encoded, true
	default:
		return nil, false
	}
}

func artifactResultReason(record *ent.Experiment) string {
	if record.FailureCode != nil && strings.TrimSpace(*record.FailureCode) != "" {
		return strings.TrimSpace(*record.FailureCode)
	}
	switch record.State {
	case "succeeded":
		return "completed"
	case "cancelled":
		return "cancelled"
	case "timed_out":
		return "timeout"
	default:
		if record.State != "" {
			return record.State
		}
		return "completed"
	}
}

func artifactMediaType(name string) string {
	switch name {
	case "metrics.json", "gemcp-result.json":
		return "application/json"
	default:
		return "text/plain"
	}
}

func artifactUnavailableReason(name string) string {
	switch name {
	case "gemcp-launch.log":
		return "gemcp-launch.log stays on managed shared storage. The control plane registers the name after dispatch and does not browse Provider files."
	case "diagnostic-report.txt":
		return "diagnostic-report.txt is written on the managed output path. The control plane does not copy arbitrary remote files."
	default:
		return "This registered artifact is not held in the control plane. Gemcp does not browse shared storage or SSH."
	}
}

func boundArtifactBytes(content []byte) []byte {
	if len(content) <= maxArtifactReadBytes {
		return content
	}
	return content[:maxArtifactReadBytes]
}

func artifactChecksum(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeArtifactName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", &ValidationError{Message: "artifact name must be a registered filename"}
	}
	switch name {
	case "gemcp-launch.log", "run.log", "gemcp-result.json", "metrics.json", "diagnostic-report.txt":
		return name, nil
	default:
		return "", &ValidationError{Message: "artifact name must be a registered filename"}
	}
}

func containsArtifactName(names []string, name string) bool {
	for _, item := range names {
		if item == name {
			return true
		}
	}
	return false
}

func datasetBindingsFromSnapshot(snapshot map[string]any) []ProposalDatasetBinding {
	raw, ok := snapshot["dataset_bindings"]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]map[string]any); ok {
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		} else {
			return nil
		}
	}
	result := make([]ProposalDatasetBinding, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		binding := ProposalDatasetBinding{
			ID:                  snapshotString(fields, "id"),
			Name:                snapshotString(fields, "name"),
			Backend:             snapshotString(fields, "backend"),
			CanonicalRoot:       snapshotString(fields, "canonical_root"),
			EnvironmentVariable: snapshotString(fields, "environment_variable"),
			RequiredMarkers:     snapshotStrings(fields, "required_markers"),
		}
		if sources, ok := fields["sources"].([]any); ok {
			for _, source := range sources {
				sourceFields, ok := source.(map[string]any)
				if !ok {
					continue
				}
				item := datasetcatalog.SourceFile{
					URL:          snapshotString(sourceFields, "url"),
					RelativePath: snapshotString(sourceFields, "relative_path"),
					SHA256:       snapshotString(sourceFields, "sha256"),
				}
				if item.URL != "" || item.RelativePath != "" {
					binding.Sources = append(binding.Sources, item)
				}
			}
		}
		if binding.Name != "" || binding.CanonicalRoot != "" {
			result = append(result, binding)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
