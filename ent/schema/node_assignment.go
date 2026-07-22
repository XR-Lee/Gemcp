package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NodeAssignment is the durable ownership record for one Attempt on one Self-hosted node.
type NodeAssignment struct{ ent.Schema }

func (NodeAssignment) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (NodeAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.Int("attempt_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.Enum("state").Values("starting", "running", "stopping", "collecting", "succeeded", "failed", "cancelled", "timed_out", "lost").Default("starting"),
		field.String("output_ref").NotEmpty().MaxLen(512).Immutable(),
		field.String("start_command_id").Optional().Nillable().MaxLen(64),
		field.String("stop_command_id").Optional().Nillable().MaxLen(64),
		field.String("workload_id").Optional().Nillable().MaxLen(255),
		field.Time("started_at").Optional().Nillable(),
		field.Time("last_heartbeat_at").Optional().Nillable(),
		field.Time("deadline_at").Optional().Nillable(),
		field.Time("hard_deadline_at").Optional().Nillable(),
		field.Time("stop_requested_at").Optional().Nillable(),
		field.String("stop_reason").Optional().Nillable().MaxLen(80),
		field.Time("finished_at").Optional().Nillable(),
		field.Int("exit_code").Optional().Nillable(),
		field.Text("log_tail").Optional().Nillable(),
		field.JSON("metrics", map[string]any{}).Default(map[string]any{}),
		field.String("failure_code").Optional().Nillable().MaxLen(100),
		field.Text("failure_reason").Optional().Nillable(),
		field.Text("last_error").Optional().Nillable(),
	}
}

func (NodeAssignment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("node_assignments").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("node_assignments").Field("project_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("node_assignments").Field("experiment_id").Unique().Required().Immutable(),
		edge.From("attempt", Attempt.Type).Ref("node_assignment").Field("attempt_id").Unique().Required().Immutable(),
		edge.From("node", SelfHostedNode.Type).Ref("assignments").Field("node_id").Unique().Required().Immutable(),
		edge.To("commands", NodeCommand.Type),
	}
}

func (NodeAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("attempt_id").Unique(),
		index.Fields("experiment_id", "created_at"),
		index.Fields("node_id", "state", "created_at"),
		index.Fields("state", "hard_deadline_at"),
	}
}
