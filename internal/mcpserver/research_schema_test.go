package mcpserver

import (
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/research"
)

func TestUpdateResearchWorkspaceNameSchemaDocumentsTheRestriction(t *testing.T) {
	schema := updateResearchWorkspaceInputSchema()
	name := studyNameSchema(schema)
	if name.Pattern != research.StudyNamePattern {
		t.Fatalf("pattern = %q", name.Pattern)
	}
	if name.MinLength == nil || *name.MinLength != 1 || name.MaxLength == nil || *name.MaxLength != research.StudyNameMaxLength {
		t.Fatalf("lengths min=%v max=%v", name.MinLength, name.MaxLength)
	}
	if !strings.Contains(name.Description, "1 to 80") || !strings.Contains(name.Description, "starting with a letter or number") {
		t.Fatalf("description = %q", name.Description)
	}
}
