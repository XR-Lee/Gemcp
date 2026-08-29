package research

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/iterationplan"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/researchedge"
	"github.com/XR-Lee/Gemcp/ent/researchnode"
	"github.com/XR-Lee/Gemcp/ent/study"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/validation"
	"github.com/google/uuid"
)

const (
	maxStudiesPerProject = 32
	maxNodesPerStudy     = 128
	maxEdgesPerStudy     = 256
	maxPlanSteps         = 16
	maxNameLength        = 80
	maxQuestionLength    = 400
	maxSummaryLength     = 800
	maxTitleLength       = 160
	maxStepDetailLength  = 280
	maxMetricNameLength  = 80
)

var (
	ErrNotFound           = errors.New("research workspace was not found")
	ErrForbidden          = errors.New("forbidden")
	ErrStudyLimit         = errors.New("study limit reached")
	ErrNodeLimit          = errors.New("research graph node limit reached")
	ErrEdgeLimit          = errors.New("research graph edge limit reached")
	ErrStudyConflict      = errors.New("study name is already used in this Project")
	ErrChoice             = errors.New("study selector is required")
	namePattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,79}$`)
	metricPattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,79}$`)
	evidenceCommitPattern = regexp.MustCompile(`(?i)^[0-9a-f]{7,64}$`)
	secretPattern         = regexp.MustCompile(`(?i)(authorization\s*:|bearer\s+[A-Za-z0-9._\-]+|api[_-]?key\s*=|private[_-]?key|BEGIN [A-Z ]+PRIVATE KEY|gmc_[A-Za-z0-9]+_|gne_[A-Za-z0-9]+_|password\s*=)`)
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type WorkspaceInput struct {
	StudyID string `json:"study_id,omitempty" jsonschema:"Study ID; omit when the Project has exactly one Study"`
}

type StudyInput struct {
	ID           string `json:"id,omitempty" jsonschema:"existing Study ID when updating"`
	Name         string `json:"name" jsonschema:"stable Study name"`
	Question     string `json:"question" jsonschema:"research question shown to the Owner"`
	Summary      string `json:"summary,omitempty" jsonschema:"short scientific summary without prompts or credentials"`
	Status       string `json:"status,omitempty" jsonschema:"active, paused, or archived"`
	RepositoryID string `json:"repository_id,omitempty" jsonschema:"same-Project repository to bind as this Study's source"`
}

type PlanStep struct {
	Title  string `json:"title" jsonschema:"one planned action"`
	Detail string `json:"detail,omitempty" jsonschema:"optional bounded detail"`
}

type PlanInput struct {
	StudyID    string     `json:"study_id,omitempty" jsonschema:"Study ID; omit when the Project has exactly one Study"`
	Goal       string     `json:"goal" jsonschema:"current iteration goal"`
	NextAction string     `json:"next_action" jsonschema:"the next human-visible action"`
	Rationale  string     `json:"rationale,omitempty" jsonschema:"why this is the next action"`
	Steps      []PlanStep `json:"steps,omitempty" jsonschema:"bounded remaining steps"`
}

type NodeInput struct {
	StudyID      string   `json:"study_id,omitempty" jsonschema:"Study ID; omit when the Project has exactly one Study"`
	ID           string   `json:"id,omitempty" jsonschema:"existing Graph node ID when attaching an edge or updating that node"`
	Kind         string   `json:"kind" jsonschema:"question, hypothesis, plan, run, result, observation, or decision"`
	Title        string   `json:"title" jsonschema:"short Graph node title"`
	Summary      string   `json:"summary,omitempty" jsonschema:"bounded scientific claim or observation"`
	Status       string   `json:"status,omitempty" jsonschema:"open, running, succeeded, failed, or superseded"`
	MetricName   string   `json:"metric_name,omitempty" jsonschema:"optional scalar metric name"`
	MetricValue  *float64 `json:"metric_value,omitempty" jsonschema:"optional scalar metric value"`
	ExperimentID string   `json:"experiment_id,omitempty" jsonschema:"same-Project Experiment ID for a run or result"`
	OccurredAt   string   `json:"occurred_at,omitempty" jsonschema:"RFC3339 or YYYY-MM-DD scientific time. For historical evidence use git committer date (git log -1 --format=%cI). Do not use the time you called this tool."`
	CommitSHA    string   `json:"commit_sha,omitempty" jsonschema:"optional evidence commit SHA, 7 to 64 hex. The Graph stays claim-based, not one node per commit."`
	FromNodeID   string   `json:"from_node_id,omitempty" jsonschema:"optional source Graph node ID"`
	Relation     string   `json:"relation,omitempty" jsonschema:"leads_to, compares, supersedes, supports, contradicts, or produced"`

	// exemptStudyCaps lets close_run finish a funded run on a full Graph. It
	// is never accepted from API input.
	exemptStudyCaps bool
}

type UpdateInput struct {
	Study *StudyInput `json:"study,omitempty" jsonschema:"create or update one Study"`
	Plan  *PlanInput  `json:"plan,omitempty" jsonschema:"replace the active iteration plan"`
	Node  *NodeInput  `json:"node,omitempty" jsonschema:"record one Graph node and optional edge"`
}

type StudySummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Question  string    `json:"question"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PlanView struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Goal       string     `json:"goal"`
	NextAction string     `json:"next_action"`
	Rationale  string     `json:"rationale,omitempty"`
	Steps      []PlanStep `json:"steps"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type NodeView struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary,omitempty"`
	Status          string     `json:"status"`
	MetricName      string     `json:"metric_name,omitempty"`
	MetricValue     *float64   `json:"metric_value,omitempty"`
	ExperimentID    string     `json:"experiment_id,omitempty"`
	ExperimentState string     `json:"experiment_state,omitempty"`
	OccurredAt      *time.Time `json:"occurred_at,omitempty"`
	CommitSHA       string     `json:"commit_sha,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type EdgeView struct {
	ID       string `json:"id"`
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	Relation string `json:"relation"`
}

type StudyRepositoryView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	Status        string `json:"status"`
}

type HypothesisRunRecord struct {
	RunNodeID      string `json:"run_node_id"`
	ExperimentID   string `json:"experiment_id,omitempty"`
	Title          string `json:"title"`
	State          string `json:"state"`
	Branch         string `json:"branch,omitempty"`
	CommitSHA      string `json:"commit_sha,omitempty"`
	ResultTitle    string `json:"result_title,omitempty"`
	HighlightTitle string `json:"highlight_title,omitempty"`
}

type HypothesisView struct {
	ID          string                `json:"id"`
	Title       string                `json:"title"`
	Summary     string                `json:"summary,omitempty"`
	Status      string                `json:"status"`
	Branch      string                `json:"branch,omitempty"`
	Experiments []HypothesisRunRecord `json:"experiments"`
}

type StudyView struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Question   string               `json:"question"`
	Summary    string               `json:"summary,omitempty"`
	Status     string               `json:"status"`
	Repository *StudyRepositoryView `json:"repository,omitempty"`
	Plan       *PlanView            `json:"plan,omitempty"`
	Nodes      []NodeView           `json:"nodes"`
	Edges      []EdgeView           `json:"edges"`
	Hypotheses []HypothesisView     `json:"hypotheses,omitempty"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

type Workspace struct {
	ProjectID   string         `json:"project_id"`
	Studies     []StudySummary `json:"studies"`
	Study       *StudyView     `json:"study,omitempty"`
	NextActions []NextAction   `json:"next_actions"`
	Warning     string         `json:"warning,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
}

type actor struct {
	tenantID      int
	projectID     int
	projectPublic string
	tokenID       *int
	actorType     auditevent.ActorType
	actorID       string
}

type Service struct{ client *ent.Client }

func NewService(client *ent.Client) *Service { return &Service{client: client} }

func (s *Service) AgentWorkspace(ctx context.Context, principal agentauth.Principal, input WorkspaceInput) (Workspace, error) {
	if !principal.HasScope("read") {
		return Workspace{}, ErrForbidden
	}
	return s.workspace(ctx, actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}, input.StudyID)
}

func (s *Service) AgentUpdate(ctx context.Context, principal agentauth.Principal, input UpdateInput) (Workspace, error) {
	if !principal.HasScope("submit") {
		return Workspace{}, ErrForbidden
	}
	tokenID := principal.TokenID
	return s.update(ctx, actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		tokenID: &tokenID, actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}, input)
}

func (s *Service) OwnerWorkspace(ctx context.Context, tenantID int, projectPublicID, studyID string) (Workspace, error) {
	projectRecord, err := s.project(ctx, tenantID, projectPublicID)
	if err != nil {
		return Workspace{}, err
	}
	return s.workspace(ctx, actor{
		tenantID: tenantID, projectID: projectRecord.ID, projectPublic: projectRecord.PublicID.String(),
		actorType: auditevent.ActorTypeUser,
	}, studyID)
}

func (s *Service) OwnerUpdate(ctx context.Context, tenantID int, actorID, projectPublicID string, input UpdateInput) (Workspace, error) {
	projectRecord, err := s.project(ctx, tenantID, projectPublicID)
	if err != nil {
		return Workspace{}, err
	}
	return s.update(ctx, actor{
		tenantID: tenantID, projectID: projectRecord.ID, projectPublic: projectRecord.PublicID.String(),
		actorType: auditevent.ActorTypeUser, actorID: strings.TrimSpace(actorID),
	}, input)
}

func (s *Service) workspace(ctx context.Context, current actor, studyID string) (Workspace, error) {
	studies, err := s.client.Study.Query().Where(study.ProjectIDEQ(current.projectID)).WithRepository().Order(ent.Desc(study.FieldUpdatedAt)).All(ctx)
	if err != nil {
		return Workspace{}, err
	}
	result := Workspace{ProjectID: current.projectPublic, Studies: makeStudySummaries(studies), GeneratedAt: time.Now().UTC()}
	selected, err := defaultStudy(studies, studyID)
	if err != nil {
		return Workspace{}, err
	}
	if selected == nil {
		if len(studies) == 0 {
			result.NextActions = deriveNextActions(nil)
		}
		return result, nil
	}
	view, err := s.studyView(ctx, selected)
	if err != nil {
		return Workspace{}, err
	}
	result.Study = &view
	result.NextActions = deriveNextActions(&view)
	return result, nil
}

func (s *Service) update(ctx context.Context, current actor, input UpdateInput) (Workspace, error) {
	if input.Study == nil && input.Plan == nil && input.Node == nil {
		return Workspace{}, invalid("provide a study, plan, or graph node")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Workspace{}, err
	}
	defer func() { _ = tx.Rollback() }()
	selected, err := s.applyUpdate(ctx, tx, current, input)
	if err != nil {
		return Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, err
	}
	return s.workspace(ctx, current, selected)
}

func (s *Service) applyUpdate(ctx context.Context, tx *ent.Tx, current actor, input UpdateInput) (string, error) {
	studies, err := tx.Study.Query().Where(study.ProjectIDEQ(current.projectID)).Order(ent.Desc(study.FieldUpdatedAt)).All(ctx)
	if err != nil {
		return "", err
	}
	selectedID := studySelector(input)
	var selected *ent.Study
	if input.Study != nil {
		selected, err = upsertStudy(ctx, tx, current, studies, *input.Study)
		if err != nil {
			return "", err
		}
		selectedID = selected.PublicID.String()
		studies, err = tx.Study.Query().Where(study.ProjectIDEQ(current.projectID)).Order(ent.Desc(study.FieldUpdatedAt)).All(ctx)
		if err != nil {
			return "", err
		}
	}
	if selected == nil {
		selected, err = selectStudy(studies, selectedID)
		if err != nil {
			return "", err
		}
	}
	if selected == nil && (input.Plan != nil || input.Node != nil) {
		if len(studies) > 1 {
			return "", ErrChoice
		}
		return "", invalid("create a Study before recording a plan or Graph node")
	}
	if input.Plan != nil {
		if _, err := replacePlan(ctx, tx, current, selected, *input.Plan); err != nil {
			return "", err
		}
	}
	if input.Node != nil {
		if strings.TrimSpace(input.Node.Kind) == string(researchnode.KindResult) {
			return "", invalid("result nodes can only be written with close_run after the Experiment is terminal")
		}
		if _, err := recordNode(ctx, tx, current, selected, *input.Node); err != nil {
			return "", err
		}
	}
	if selected != nil {
		return selected.PublicID.String(), nil
	}
	return "", nil
}

func upsertStudy(ctx context.Context, tx *ent.Tx, current actor, studies []*ent.Study, input StudyInput) (*ent.Study, error) {
	name, err := normalizeName(input.Name)
	if err != nil {
		return nil, err
	}
	question, err := normalizeText("question", input.Question, 8, maxQuestionLength)
	if err != nil {
		return nil, err
	}
	summary, err := normalizeOptionalText("summary", input.Summary, maxSummaryLength)
	if err != nil {
		return nil, err
	}
	status, err := parseStudyStatus(input.Status, study.StatusActive)
	if err != nil {
		return nil, err
	}
	var bound *ent.Repository
	if strings.TrimSpace(input.RepositoryID) != "" {
		bound, err = findProjectRepository(ctx, tx, current.projectID, input.RepositoryID)
		if err != nil {
			return nil, err
		}
	}
	existing, err := findStudy(studies, input.ID, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if len(studies) >= maxStudiesPerProject {
			return nil, ErrStudyLimit
		}
		create := tx.Study.Create().
			SetTenantID(current.tenantID).SetProjectID(current.projectID).
			SetName(name).SetQuestion(question).SetSummary(summary).SetStatus(status)
		if bound != nil {
			create.SetRepositoryID(bound.ID)
		}
		if current.tokenID != nil {
			create.SetAgentTokenID(*current.tokenID)
		}
		record, err := create.Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return nil, ErrStudyConflict
			}
			return nil, err
		}
		nodeCreate := tx.ResearchNode.Create().
			SetTenantID(current.tenantID).SetProjectID(current.projectID).SetStudyID(record.ID).
			SetKind(researchnode.KindQuestion).SetTitle(truncateTitle(question)).SetSummary(question).SetStatus(researchnode.StatusOpen)
		if current.tokenID != nil {
			nodeCreate.SetAgentTokenID(*current.tokenID)
		}
		if _, err := nodeCreate.Save(ctx); err != nil {
			return nil, err
		}
		if err := writeAudit(ctx, tx, current, "research.study_created", "study", record.PublicID.String(), studyAudit(name, status, bound)); err != nil {
			return nil, err
		}
		return record, nil
	}
	if existing.Name != name {
		conflict, _ := findStudy(studies, "", name)
		if conflict != nil && conflict.ID != existing.ID {
			return nil, ErrStudyConflict
		}
	}
	if strings.TrimSpace(input.Status) == "" {
		status = existing.Status
	}
	update := existing.Update().SetName(name).SetQuestion(question).SetSummary(summary).SetStatus(status)
	if bound != nil {
		update.SetRepositoryID(bound.ID)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ErrStudyConflict
		}
		return nil, err
	}
	if err := writeAudit(ctx, tx, current, "research.study_updated", "study", updated.PublicID.String(), studyAudit(name, status, bound)); err != nil {
		return nil, err
	}
	return updated, nil
}

func replacePlan(ctx context.Context, tx *ent.Tx, current actor, selected *ent.Study, input PlanInput) (*ent.IterationPlan, error) {
	goal, err := normalizeText("goal", input.Goal, 4, maxSummaryLength)
	if err != nil {
		return nil, err
	}
	nextAction, err := normalizeText("next_action", input.NextAction, 4, maxSummaryLength)
	if err != nil {
		return nil, err
	}
	rationale, err := normalizeOptionalText("rationale", input.Rationale, maxSummaryLength)
	if err != nil {
		return nil, err
	}
	steps, err := normalizeSteps(input.Steps)
	if err != nil {
		return nil, err
	}
	active, err := tx.IterationPlan.Query().Where(iterationplan.StudyIDEQ(selected.ID), iterationplan.StatusEQ(iterationplan.StatusActive)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, plan := range active {
		if _, err := plan.Update().SetStatus(iterationplan.StatusSuperseded).Save(ctx); err != nil {
			return nil, err
		}
	}
	create := tx.IterationPlan.Create().
		SetTenantID(current.tenantID).SetProjectID(current.projectID).SetStudyID(selected.ID).
		SetStatus(iterationplan.StatusActive).SetGoal(goal).SetNextAction(nextAction).SetRationale(rationale).SetSteps(steps)
	if current.tokenID != nil {
		create.SetAgentTokenID(*current.tokenID)
	}
	record, err := create.Save(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := selected.Update().SetUpdatedAt(time.Now().UTC()).Save(ctx); err != nil {
		return nil, err
	}
	if err := writeAudit(ctx, tx, current, "research.plan_replaced", "iteration_plan", record.PublicID.String(), map[string]any{
		"study_id": selected.PublicID.String(), "superseded": len(active),
	}); err != nil {
		return nil, err
	}
	return record, nil
}

func recordNode(ctx context.Context, tx *ent.Tx, current actor, selected *ent.Study, input NodeInput) (*ent.ResearchNode, error) {
	kind := researchnode.Kind(strings.TrimSpace(input.Kind))
	if err := researchnode.KindValidator(kind); err != nil {
		return nil, invalid("kind must be question, hypothesis, plan, run, result, observation, or decision")
	}
	title, err := normalizeText("title", input.Title, 2, maxTitleLength)
	if err != nil {
		return nil, err
	}
	summary, err := normalizeOptionalText("summary", input.Summary, maxSummaryLength)
	if err != nil {
		return nil, err
	}
	status := researchnode.StatusOpen
	if strings.TrimSpace(input.Status) != "" {
		if err := researchnode.StatusValidator(researchnode.Status(input.Status)); err != nil {
			return nil, invalid("status must be open, running, succeeded, failed, or superseded")
		}
		status = researchnode.Status(input.Status)
	}
	metricName := strings.TrimSpace(input.MetricName)
	if metricName != "" && !metricPattern.MatchString(metricName) {
		return nil, invalid("metric_name must use letters, numbers, dots, underscores, or hyphens")
	}
	occurredAt, err := parseOccurredAt(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	commitSHA, err := normalizeEvidenceCommit(input.CommitSHA)
	if err != nil {
		return nil, err
	}
	var experimentRecord *ent.Experiment
	if strings.TrimSpace(input.ExperimentID) != "" {
		if kind != researchnode.KindRun && kind != researchnode.KindResult {
			return nil, invalid("only run and result nodes can link an Experiment")
		}
		experimentRecord, err = findProjectExperiment(ctx, tx, current.projectID, input.ExperimentID)
		if err != nil {
			return nil, err
		}
	}
	if experimentRecord != nil {
		if occurredAt == nil {
			stamp := experimentEvidenceTime(experimentRecord)
			occurredAt = &stamp
		}
		if commitSHA == "" {
			commitSHA = experimentRecord.CommitSha
		}
	}
	nodes, err := tx.ResearchNode.Query().Where(researchnode.StudyIDEQ(selected.ID)).WithExperiment().All(ctx)
	if err != nil {
		return nil, err
	}
	var record *ent.ResearchNode
	if strings.TrimSpace(input.ID) != "" {
		existing, findErr := findNode(nodes, input.ID)
		if findErr != nil {
			return nil, invalid("id must be a Graph node ID in this Study")
		}
		if existing.Kind != kind {
			return nil, invalid("id refers to a different kind of Graph node")
		}
		record = existing
	}
	if record == nil && experimentRecord != nil {
		for _, node := range nodes {
			if node.ExperimentID != nil && *node.ExperimentID == experimentRecord.ID {
				if node.Kind != kind {
					return nil, invalid("this Experiment is already bound to a different Graph node")
				}
				record = node
				break
			}
		}
	}
	if record == nil {
		if len(nodes) >= maxNodesPerStudy && !input.exemptStudyCaps {
			return nil, ErrNodeLimit
		}
		create := tx.ResearchNode.Create().
			SetTenantID(current.tenantID).SetProjectID(current.projectID).SetStudyID(selected.ID).
			SetKind(kind).SetTitle(title).SetSummary(summary).SetStatus(status)
		if metricName != "" {
			create.SetMetricName(metricName)
		}
		if input.MetricValue != nil {
			create.SetMetricValue(*input.MetricValue)
		}
		if experimentRecord != nil {
			create.SetExperimentID(experimentRecord.ID)
		}
		if occurredAt != nil {
			create.SetOccurredAt(*occurredAt)
		}
		if commitSHA != "" {
			create.SetCommitSha(commitSHA)
		}
		if current.tokenID != nil {
			create.SetAgentTokenID(*current.tokenID)
		}
		record, err = create.Save(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		update := record.Update().SetTitle(title).SetSummary(summary).SetStatus(status)
		if metricName != "" {
			update.SetMetricName(metricName)
		} else {
			update.ClearMetricName()
		}
		if input.MetricValue != nil {
			update.SetMetricValue(*input.MetricValue)
		} else {
			update.ClearMetricValue()
		}
		if strings.TrimSpace(input.OccurredAt) != "" || (record.OccurredAt == nil && occurredAt != nil) {
			update.SetOccurredAt(*occurredAt)
		}
		if strings.TrimSpace(input.CommitSHA) != "" || ((record.CommitSha == nil || *record.CommitSha == "") && commitSHA != "") {
			update.SetCommitSha(commitSHA)
		}
		record, err = update.Save(ctx)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(input.FromNodeID) != "" || strings.TrimSpace(input.Relation) != "" {
		if err := recordEdge(ctx, tx, current, selected, nodes, record, input.FromNodeID, input.Relation, input.exemptStudyCaps); err != nil {
			return nil, err
		}
	}
	if _, err := selected.Update().SetUpdatedAt(time.Now().UTC()).Save(ctx); err != nil {
		return nil, err
	}
	if err := writeAudit(ctx, tx, current, "research.node_recorded", "research_node", record.PublicID.String(), map[string]any{
		"study_id": selected.PublicID.String(), "kind": string(kind), "status": string(status),
	}); err != nil {
		return nil, err
	}
	return record, nil
}

func recordEdge(ctx context.Context, tx *ent.Tx, current actor, selected *ent.Study, nodes []*ent.ResearchNode, to *ent.ResearchNode, fromID, relationValue string, exemptStudyCaps bool) error {
	relation := researchedge.Relation(strings.TrimSpace(relationValue))
	if err := researchedge.RelationValidator(relation); err != nil {
		return invalid("relation must be leads_to, compares, supersedes, supports, contradicts, or produced")
	}
	from, err := findNode(nodes, fromID)
	if err != nil {
		return err
	}
	if from.ID == to.ID {
		return invalid("a Graph edge cannot connect a node to itself")
	}
	if !legalEdge(from.Kind, to.Kind, relation) {
		return invalid("relation " + string(relation) + " is not allowed from a " + string(from.Kind) + " node to a " + string(to.Kind) + " node")
	}
	existing, err := tx.ResearchEdge.Query().Where(
		researchedge.StudyIDEQ(selected.ID), researchedge.FromNodeIDEQ(from.ID), researchedge.ToNodeIDEQ(to.ID), researchedge.RelationEQ(relation),
	).Exist(ctx)
	if err != nil {
		return err
	}
	if existing {
		return nil
	}
	count, err := tx.ResearchEdge.Query().Where(researchedge.StudyIDEQ(selected.ID)).Count(ctx)
	if err != nil {
		return err
	}
	if count >= maxEdgesPerStudy && !exemptStudyCaps {
		return ErrEdgeLimit
	}
	_, err = tx.ResearchEdge.Create().
		SetTenantID(current.tenantID).SetProjectID(current.projectID).SetStudyID(selected.ID).
		SetFromNodeID(from.ID).SetToNodeID(to.ID).SetRelation(relation).
		Save(ctx)
	return err
}

func (s *Service) studyView(ctx context.Context, selected *ent.Study) (StudyView, error) {
	plans, err := s.client.IterationPlan.Query().Where(iterationplan.StudyIDEQ(selected.ID)).Order(ent.Desc(iterationplan.FieldCreatedAt)).All(ctx)
	if err != nil {
		return StudyView{}, err
	}
	nodes, err := s.client.ResearchNode.Query().Where(researchnode.StudyIDEQ(selected.ID)).WithExperiment().Order(ent.Asc(researchnode.FieldCreatedAt)).All(ctx)
	if err != nil {
		return StudyView{}, err
	}
	edges, err := s.client.ResearchEdge.Query().Where(researchedge.StudyIDEQ(selected.ID)).WithFromNode().WithToNode().Order(ent.Asc(researchedge.FieldCreatedAt)).All(ctx)
	if err != nil {
		return StudyView{}, err
	}
	view := StudyView{
		ID: selected.PublicID.String(), Name: selected.Name, Question: selected.Question, Summary: selected.Summary,
		Status: string(selected.Status), Nodes: make([]NodeView, 0, len(nodes)), Edges: make([]EdgeView, 0, len(edges)),
		UpdatedAt: selected.UpdatedAt.UTC(),
	}
	if repo, err := attachedRepository(ctx, s.client, selected); err != nil {
		return StudyView{}, err
	} else if repo != nil {
		view.Repository = &StudyRepositoryView{
			ID: repo.PublicID.String(), Name: repo.Name, SSHURL: repo.SSHURL,
			DefaultBranch: repo.DefaultBranch, Status: string(repo.Status),
		}
	}
	for _, plan := range plans {
		if plan.Status == iterationplan.StatusActive {
			copied := makePlanView(plan)
			view.Plan = &copied
			break
		}
	}
	for _, node := range nodes {
		view.Nodes = append(view.Nodes, makeNodeView(node))
	}
	for _, edge := range edges {
		from, _ := edge.Edges.FromNodeOrErr()
		to, _ := edge.Edges.ToNodeOrErr()
		if from == nil || to == nil {
			continue
		}
		view.Edges = append(view.Edges, EdgeView{
			ID: edge.PublicID.String(), FromID: from.PublicID.String(), ToID: to.PublicID.String(), Relation: string(edge.Relation),
		})
	}
	branchByExperiment, err := experimentBranches(ctx, s.client, nodes)
	if err != nil {
		return StudyView{}, err
	}
	defaultBranch := ""
	if view.Repository != nil {
		defaultBranch = view.Repository.DefaultBranch
	}
	view.Hypotheses = makeHypothesisRecords(view, defaultBranch, branchByExperiment)
	return view, nil
}

func experimentBranches(ctx context.Context, client *ent.Client, nodes []*ent.ResearchNode) (map[string]string, error) {
	ids := make([]int, 0, len(nodes))
	publicByID := map[int]string{}
	repositoryIDs := map[int]int{}
	for _, node := range nodes {
		experimentRecord, err := node.Edges.ExperimentOrErr()
		if err != nil || experimentRecord == nil {
			continue
		}
		ids = append(ids, experimentRecord.ID)
		publicByID[experimentRecord.ID] = experimentRecord.PublicID.String()
		if experimentRecord.RepositoryID != nil {
			repositoryIDs[experimentRecord.ID] = *experimentRecord.RepositoryID
		}
	}
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	refs := map[string]string{}
	if len(repositoryIDs) > 0 {
		repoIDs := make([]int, 0, len(repositoryIDs))
		for _, id := range repositoryIDs {
			repoIDs = append(repoIDs, id)
		}
		repos, err := client.Repository.Query().Where(repository.IDIn(repoIDs...)).All(ctx)
		if err != nil {
			return nil, err
		}
		byRepo := map[int]string{}
		for _, repo := range repos {
			byRepo[repo.ID] = repo.DefaultBranch
		}
		for experimentID, repoID := range repositoryIDs {
			if branch := strings.TrimSpace(byRepo[repoID]); branch != "" {
				refs[publicByID[experimentID]] = branch
			}
		}
	}
	proposals, err := client.ExperimentProposal.Query().Where(experimentproposal.ExperimentIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, proposal := range proposals {
		if proposal.ExperimentID == nil {
			continue
		}
		publicID := publicByID[*proposal.ExperimentID]
		if publicID == "" || strings.TrimSpace(proposal.RequestedRef) == "" {
			continue
		}
		refs[publicID] = proposal.RequestedRef
	}
	return refs, nil
}

func makeHypothesisRecords(view StudyView, defaultBranch string, branchByExperiment map[string]string) []HypothesisView {
	outgoing := map[string][]EdgeView{}
	for _, edge := range view.Edges {
		outgoing[edge.FromID] = append(outgoing[edge.FromID], edge)
	}
	nodesByID := map[string]NodeView{}
	var hypotheses []NodeView
	for _, node := range view.Nodes {
		nodesByID[node.ID] = node
		if node.Kind == "hypothesis" {
			hypotheses = append(hypotheses, node)
		}
	}
	records := make([]HypothesisView, 0, len(hypotheses))
	for _, hypothesis := range hypotheses {
		kids := descendantsOf(outgoing, nodesByID, hypothesis.ID)
		var runs, results, observations []NodeView
		for _, node := range kids {
			switch node.Kind {
			case "run":
				runs = append(runs, node)
			case "result":
				results = append(results, node)
			case "observation":
				observations = append(observations, node)
			}
		}
		experiments := make([]HypothesisRunRecord, 0, len(runs))
		for _, run := range runs {
			record := HypothesisRunRecord{
				RunNodeID: run.ID, ExperimentID: run.ExperimentID, Title: run.Title,
				State: run.ExperimentState, CommitSHA: run.CommitSHA,
			}
			if record.State == "" {
				record.State = run.Status
			}
			if ref := branchByExperiment[run.ExperimentID]; ref != "" {
				record.Branch = ref
			} else {
				record.Branch = defaultBranch
			}
			producedResultID := ""
			for _, result := range results {
				if hasEdge(view.Edges, run.ID, result.ID, "produced") {
					producedResultID = result.ID
					record.ResultTitle = result.Title
					if record.CommitSHA == "" {
						record.CommitSHA = result.CommitSHA
					}
					break
				}
			}
			// close_run links the highlight observation to its result, which
			// stays unambiguous when one hypothesis accumulates several runs.
			if producedResultID != "" {
				for _, observation := range observations {
					if hasEdge(view.Edges, producedResultID, observation.ID, "leads_to") {
						record.HighlightTitle = observation.Title
						break
					}
				}
			}
			// Legacy fallbacks for graphs written before that edge existed.
			if record.HighlightTitle == "" {
				for _, observation := range observations {
					if observation.CommitSHA != "" && observation.CommitSHA == record.CommitSHA {
						record.HighlightTitle = observation.Title
						break
					}
				}
			}
			if record.HighlightTitle == "" {
				for _, observation := range observations {
					if hasEdge(view.Edges, hypothesis.ID, observation.ID, "leads_to") &&
						(hasEdge(view.Edges, observation.ID, hypothesis.ID, "supports") || hasEdge(view.Edges, observation.ID, hypothesis.ID, "contradicts")) {
						record.HighlightTitle = observation.Title
						break
					}
				}
			}
			experiments = append(experiments, record)
		}
		records = append(records, HypothesisView{
			ID: hypothesis.ID, Title: hypothesis.Title, Summary: hypothesis.Summary,
			Status: hypothesis.Status, Branch: defaultBranch, Experiments: experiments,
		})
	}
	return records
}

func hasEdge(edges []EdgeView, fromID, toID, relation string) bool {
	for _, edge := range edges {
		if edge.FromID == fromID && edge.ToID == toID && edge.Relation == relation {
			return true
		}
	}
	return false
}

func (s *Service) project(ctx context.Context, tenantID int, projectPublicID string) (*ent.Project, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return nil, ErrNotFound
	}
	record, err := s.client.Project.Query().Where(project.TenantIDEQ(tenantID), project.PublicIDEQ(publicID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return record, err
}

func studyAudit(name string, status study.Status, bound *ent.Repository) map[string]any {
	metadata := map[string]any{"name": name, "status": string(status)}
	if bound != nil {
		metadata["repository_id"] = bound.PublicID.String()
	}
	return metadata
}

func attachedRepository(ctx context.Context, client *ent.Client, selected *ent.Study) (*ent.Repository, error) {
	if selected.Edges.Repository != nil {
		return selected.Edges.Repository, nil
	}
	if selected.RepositoryID == nil {
		return nil, nil
	}
	record, err := client.Repository.Get(ctx, *selected.RepositoryID)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return record, err
}

func findProjectRepository(ctx context.Context, tx *ent.Tx, projectID int, repositoryPublicID string) (*ent.Repository, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(repositoryPublicID))
	if err != nil {
		return nil, invalid("repository_id must be a Project repository ID")
	}
	record, err := tx.Repository.Query().Where(repository.ProjectIDEQ(projectID), repository.PublicIDEQ(publicID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, invalid("linked repository was not found in this Project")
	}
	return record, err
}

func findProjectExperiment(ctx context.Context, tx *ent.Tx, projectID int, experimentPublicID string) (*ent.Experiment, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(experimentPublicID))
	if err != nil {
		return nil, invalid("experiment_id must be a Project Experiment ID")
	}
	record, err := tx.Experiment.Query().Where(experiment.ProjectIDEQ(projectID), experiment.PublicIDEQ(publicID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, invalid("linked Experiment was not found in this Project")
	}
	return record, err
}

func selectStudy(studies []*ent.Study, studyID string) (*ent.Study, error) {
	if strings.TrimSpace(studyID) != "" {
		return findStudy(studies, studyID, "")
	}
	if len(studies) == 0 {
		return nil, nil
	}
	if len(studies) == 1 {
		return studies[0], nil
	}
	return nil, nil
}

func defaultStudy(studies []*ent.Study, studyID string) (*ent.Study, error) {
	if strings.TrimSpace(studyID) != "" {
		return findStudy(studies, studyID, "")
	}
	if len(studies) == 0 {
		return nil, nil
	}
	return studies[0], nil
}

func findStudy(studies []*ent.Study, studyID, name string) (*ent.Study, error) {
	if strings.TrimSpace(studyID) != "" {
		publicID, err := uuid.Parse(strings.TrimSpace(studyID))
		if err != nil {
			return nil, invalid("study_id must be a Study ID")
		}
		for _, record := range studies {
			if record.PublicID == publicID {
				return record, nil
			}
		}
		return nil, ErrNotFound
	}
	if name == "" {
		return nil, nil
	}
	for _, record := range studies {
		if record.Name == name {
			return record, nil
		}
	}
	return nil, nil
}

func findNode(nodes []*ent.ResearchNode, nodeID string) (*ent.ResearchNode, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return nil, invalid("from_node_id must be a Graph node ID")
	}
	for _, record := range nodes {
		if record.PublicID == publicID {
			return record, nil
		}
	}
	return nil, invalid("from_node_id was not found in this Study")
}

func studySelector(input UpdateInput) string {
	if input.Study != nil && strings.TrimSpace(input.Study.ID) != "" {
		return input.Study.ID
	}
	if input.Plan != nil && strings.TrimSpace(input.Plan.StudyID) != "" {
		return input.Plan.StudyID
	}
	if input.Node != nil && strings.TrimSpace(input.Node.StudyID) != "" {
		return input.Node.StudyID
	}
	return ""
}

func makeStudySummaries(studies []*ent.Study) []StudySummary {
	result := make([]StudySummary, 0, len(studies))
	for _, record := range studies {
		result = append(result, StudySummary{
			ID: record.PublicID.String(), Name: record.Name, Question: record.Question,
			Status: string(record.Status), UpdatedAt: record.UpdatedAt.UTC(),
		})
	}
	return result
}

func makePlanView(record *ent.IterationPlan) PlanView {
	steps := make([]PlanStep, 0, len(record.Steps))
	for _, step := range record.Steps {
		title, _ := step["title"].(string)
		detail, _ := step["detail"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		steps = append(steps, PlanStep{Title: title, Detail: detail})
	}
	return PlanView{
		ID: record.PublicID.String(), Status: string(record.Status), Goal: record.Goal, NextAction: record.NextAction,
		Rationale: record.Rationale, Steps: steps, CreatedAt: record.CreatedAt.UTC(), UpdatedAt: record.UpdatedAt.UTC(),
	}
}

func makeNodeView(record *ent.ResearchNode) NodeView {
	view := NodeView{
		ID: record.PublicID.String(), Kind: string(record.Kind), Title: record.Title, Summary: record.Summary,
		Status: string(record.Status), CreatedAt: record.CreatedAt.UTC(), UpdatedAt: record.UpdatedAt.UTC(),
	}
	if record.MetricName != nil {
		view.MetricName = *record.MetricName
	}
	if record.MetricValue != nil {
		view.MetricValue = record.MetricValue
	}
	if experimentRecord, err := record.Edges.ExperimentOrErr(); err == nil && experimentRecord != nil {
		view.ExperimentID = experimentRecord.PublicID.String()
		view.ExperimentState = experimentRecord.State
		if view.CommitSHA == "" {
			view.CommitSHA = experimentRecord.CommitSha
		}
		if view.OccurredAt == nil {
			stamp := experimentEvidenceTime(experimentRecord)
			view.OccurredAt = &stamp
		}
	}
	if record.OccurredAt != nil {
		stamp := record.OccurredAt.UTC()
		view.OccurredAt = &stamp
	}
	if record.CommitSha != nil && strings.TrimSpace(*record.CommitSha) != "" {
		view.CommitSHA = *record.CommitSha
	}
	return view
}

func parseOccurredAt(value string) (*time.Time, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil, nil
	}
	var parsed time.Time
	var err error
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err = time.Parse(layout, text)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, invalid("occurred_at must be RFC3339 or YYYY-MM-DD")
	}
	parsed = parsed.UTC()
	if parsed.After(time.Now().UTC().Add(24 * time.Hour)) {
		return nil, invalid("occurred_at cannot be in the future")
	}
	return &parsed, nil
}

func normalizeEvidenceCommit(value string) (string, error) {
	sha := strings.ToLower(strings.TrimSpace(value))
	if sha == "" {
		return "", nil
	}
	if !evidenceCommitPattern.MatchString(sha) {
		return "", invalid("commit_sha must be a 7 to 64 character hexadecimal SHA")
	}
	return sha, nil
}

func experimentEvidenceTime(record *ent.Experiment) time.Time {
	if record.StartedAt != nil {
		return record.StartedAt.UTC()
	}
	return record.CreatedAt.UTC()
}

func normalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if !namePattern.MatchString(name) {
		return "", invalid("name must use 1 to 80 letters, numbers, spaces, dots, underscores, or hyphens")
	}
	if err := rejectSecrets(name); err != nil {
		return "", err
	}
	return name, nil
}

func normalizeSteps(input []PlanStep) ([]map[string]any, error) {
	if len(input) > maxPlanSteps {
		return nil, invalid("a plan can include at most 16 steps")
	}
	steps := make([]map[string]any, 0, len(input))
	for index, step := range input {
		title, err := normalizeText(fmt.Sprintf("steps[%d].title", index), step.Title, 2, maxTitleLength)
		if err != nil {
			return nil, err
		}
		detail, err := normalizeOptionalText(fmt.Sprintf("steps[%d].detail", index), step.Detail, maxStepDetailLength)
		if err != nil {
			return nil, err
		}
		item := map[string]any{"title": title}
		if detail != "" {
			item["detail"] = detail
		}
		steps = append(steps, item)
	}
	return steps, nil
}

func normalizeText(field, value string, minLength, maxLength int) (string, error) {
	text := strings.TrimSpace(value)
	if utf8.RuneCountInString(text) < minLength {
		return "", invalid(field + " is too short")
	}
	return normalizeOptionalText(field, text, maxLength)
}

func normalizeOptionalText(field, value string, maxLength int) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", nil
	}
	if utf8.RuneCountInString(text) > maxLength {
		return "", invalid(field + " is too long")
	}
	if strings.Count(text, "\n") > 8 {
		return "", invalid(field + " has too many lines")
	}
	for _, runeValue := range text {
		if runeValue == '\n' || runeValue == '\t' {
			continue
		}
		if !unicode.IsPrint(runeValue) {
			return "", invalid(field + " contains unsupported characters")
		}
	}
	if err := rejectSecrets(text); err != nil {
		return "", err
	}
	return text, nil
}

func rejectSecrets(value string) error {
	if secretPattern.MatchString(value) {
		return invalid("research text cannot contain credentials or private keys")
	}
	return nil
}

func truncateTitle(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxTitleLength {
		return string(runes)
	}
	return string(runes[:maxTitleLength-1]) + "…"
}

func parseStudyStatus(value string, fallback study.Status) (study.Status, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	status := study.Status(value)
	if err := study.StatusValidator(status); err != nil {
		return "", invalid("status must be active, paused, or archived")
	}
	return status, nil
}

func writeAudit(ctx context.Context, tx *ent.Tx, current actor, action, targetType, targetID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["project_id"] = current.projectPublic
	_, err := tx.AuditEvent.Create().
		SetTenantID(current.tenantID).
		SetActorType(current.actorType).
		SetActorID(current.actorID).
		SetAction(action).
		SetTargetType(targetType).
		SetTargetID(targetID).
		SetMetadata(metadata).
		Save(ctx)
	return err
}
