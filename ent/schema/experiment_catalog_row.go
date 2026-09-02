package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ExperimentCatalogRow is one raw experiment record extracted from a
// registered repository research branch. It is not a Graph node and
// never starts a workload.
type ExperimentCatalogRow struct{ ent.Schema }

func (ExperimentCatalogRow) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ExperimentCatalogRow) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("repository_id").Immutable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.String("branch").NotEmpty().MaxLen(255),
		field.String("setting").NotEmpty().MaxLen(400),
		field.String("method").NotEmpty().MaxLen(160),
		field.Text("implementation").NotEmpty(),
		field.String("metric").NotEmpty().MaxLen(160),
		field.Text("result").NotEmpty(),
		field.String("link").Optional().MaxLen(512),
		field.String("commit_hash").NotEmpty().MaxLen(64),
	}
}

func (ExperimentCatalogRow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("experiment_catalog_rows").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("experiment_catalog_rows").Field("project_id").Unique().Required().Immutable(),
		edge.From("repository", Repository.Type).Ref("experiment_catalog_rows").Field("repository_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("experiment_catalog_rows").Field("agent_token_id").Unique().Immutable(),
	}
}

func (ExperimentCatalogRow) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("repository_id", "branch", "commit_hash", "setting").Unique(),
		index.Fields("project_id", "repository_id", "updated_at"),
		index.Fields("repository_id", "branch"),
	}
}
