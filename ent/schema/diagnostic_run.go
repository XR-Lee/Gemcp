package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DiagnosticRun struct{ ent.Schema }

func (DiagnosticRun) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (DiagnosticRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.Enum("backend").Values("autodl_private", "self_hosted").Immutable(),
		field.Enum("suite").Values("gpu_connectivity", "pytorch_cuda").Immutable(),
		field.String("requested_by").NotEmpty().MaxLen(120).Immutable(),
		field.Bytes("idempotency_key_hash").Sensitive().Immutable(),
		field.Bytes("request_fingerprint").Sensitive().Immutable(),
		field.JSON("preflight", map[string]any{}).Default(map[string]any{}).Immutable(),
	}
}

func (DiagnosticRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("diagnostic_runs").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("diagnostic_runs").Field("project_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("diagnostic_run").Field("experiment_id").Unique().Required().Immutable(),
	}
}

func (DiagnosticRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "idempotency_key_hash").Unique(),
		index.Fields("project_id", "created_at"),
	}
}
