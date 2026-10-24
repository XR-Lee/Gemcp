package mcpserver

import (
	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/google/jsonschema-go/jsonschema"
)

// updateResearchWorkspaceInputSchema is the inferred UpdateInput schema with
// the Study name character restriction copied from normalizeName. The
// jsonschema struct tag can only carry a description.
func updateResearchWorkspaceInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[research.UpdateInput](nil)
	if err != nil {
		panic(err)
	}
	name := studyNameSchema(schema)
	minLength := 1
	maxLength := research.StudyNameMaxLength
	name.MinLength = &minLength
	name.MaxLength = &maxLength
	name.Pattern = research.StudyNamePattern
	return schema
}

func studyNameSchema(schema *jsonschema.Schema) *jsonschema.Schema {
	study := schema.Properties["study"]
	if study == nil || study.Properties == nil || study.Properties["name"] == nil {
		panic("update_research_workspace schema is missing study.name")
	}
	return study.Properties["name"]
}
