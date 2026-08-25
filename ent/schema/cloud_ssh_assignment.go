package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CloudSSHAssignment is the durable ownership record for one Attempt on one Cloud SSH node.
type CloudSSHAssignment struct{ ent.Schema }

func (CloudSSHAssignment) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (CloudSSHAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.Int("attempt_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.Enum("state").Values("starting", "running", "stopping", "collecting", "succeeded", "failed", "cancelled", "timed_out", "lost").Default("starting"),
		field.String("remote_dir").NotEmpty().MaxLen(512).Immutable(),
		field.String("container_id").Optional().Nillable().MaxLen(128),
		field.String("output_ref").NotEmpty().MaxLen(512).Immutable(),
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

func (CloudSSHAssignment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("cloud_ssh_assignments").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("cloud_ssh_assignments").Field("project_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("cloud_ssh_assignments").Field("experiment_id").Unique().Required().Immutable(),
		edge.From("attempt", Attempt.Type).Ref("cloud_ssh_assignment").Field("attempt_id").Unique().Required().Immutable(),
		edge.From("node", CloudSSHNode.Type).Ref("assignments").Field("node_id").Unique().Required().Immutable(),
	}
}

func (CloudSSHAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("attempt_id").Unique(),
		index.Fields("experiment_id", "created_at"),
		index.Fields("node_id", "state", "created_at"),
		index.Fields("state", "hard_deadline_at"),
	}
}
