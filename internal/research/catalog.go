package research

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	"github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/experimentcatalogrow"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/google/uuid"
)

const (
	maxCatalogRowsPerRepository = 512
	maxCatalogRowsPerWrite      = 64
	maxCatalogSettingLength     = 400
	maxCatalogMethodLength      = 160
	maxCatalogImplementationLen = 400
	maxCatalogMetricLength      = 160
	maxCatalogResultLength      = 800
	maxCatalogLinkLength        = 512
)

var catalogBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)

type CatalogRowInput struct {
	Branch         string `json:"branch" jsonschema:"research branch, tag, or documented git ref"`
	Setting        string `json:"setting" jsonschema:"experimental Setting: dataset, protocol, seed, hardware bound, or config identity"`
	Method         string `json:"method" jsonschema:"named method, arm, or scientific claim"`
	Implementation string `json:"implementation" jsonschema:"code path, config, serializer, or training recipe actually used"`
	Metric         string `json:"metric" jsonschema:"metric name or name=value copied from the repository evidence"`
	Result         string `json:"result" jsonschema:"reported number or frozen conclusion; do not invent"`
	Link           string `json:"link,omitempty" jsonschema:"source path or URL inside the repository evidence"`
	Hash           string `json:"hash" jsonschema:"git commit SHA 7 to 64 hex from that research branch"`
}

type CatalogRecordInput struct {
	RepositoryID string            `json:"repository_id" jsonschema:"registered Project repository ID"`
	Rows         []CatalogRowInput `json:"rows" jsonschema:"raw experiment rows extracted from research branches"`
}

type CatalogListInput struct {
	RepositoryID string `json:"repository_id,omitempty" jsonschema:"optional registered repository ID; omit to list every Project repository"`
}

type CatalogRowView struct {
	ID             string    `json:"id"`
	RepositoryID   string    `json:"repository_id"`
	Branch         string    `json:"branch"`
	Setting        string    `json:"setting"`
	Method         string    `json:"method"`
	Implementation string    `json:"implementation"`
	Metric         string    `json:"metric"`
	Result         string    `json:"result"`
	Link           string    `json:"link,omitempty"`
	Hash           string    `json:"hash"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CatalogRepositoryView struct {
	ID                       string           `json:"id"`
	Name                     string           `json:"name"`
	SSHURL                   string           `json:"ssh_url"`
	DefaultBranch            string           `json:"default_branch"`
	Status                   string           `json:"status"`
	LastVerifiedAt           *time.Time       `json:"last_verified_at,omitempty"`
	ObservationWritesAllowed bool             `json:"observation_writes_allowed"`
	PendingNote              string           `json:"pending_note,omitempty"`
	Rows                     []CatalogRowView `json:"rows"`
}

type CatalogView struct {
	ProjectID    string                  `json:"project_id"`
	Repositories []CatalogRepositoryView `json:"repositories"`
	GeneratedAt  time.Time               `json:"generated_at"`
}

func (s *Service) AgentRecordCatalog(ctx context.Context, principal agentauth.Principal, input CatalogRecordInput) (CatalogView, error) {
	if !principal.HasScope("submit") {
		return CatalogView{}, ErrForbidden
	}
	tokenID := principal.TokenID
	return s.recordCatalog(ctx, actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		tokenID: &tokenID, actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}, input)
}

func (s *Service) AgentCatalog(ctx context.Context, principal agentauth.Principal, input CatalogListInput) (CatalogView, error) {
	if !principal.HasScope("read") {
		return CatalogView{}, ErrForbidden
	}
	return s.catalog(ctx, actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}, input.RepositoryID)
}

func (s *Service) OwnerCatalog(ctx context.Context, tenantID int, projectPublicID, repositoryID string) (CatalogView, error) {
	projectRecord, err := s.project(ctx, tenantID, projectPublicID)
	if err != nil {
		return CatalogView{}, err
	}
	return s.catalog(ctx, actor{
		tenantID: tenantID, projectID: projectRecord.ID, projectPublic: projectRecord.PublicID.String(),
		actorType: auditevent.ActorTypeUser,
	}, repositoryID)
}

func (s *Service) recordCatalog(ctx context.Context, current actor, input CatalogRecordInput) (CatalogView, error) {
	if len(input.Rows) == 0 {
		return CatalogView{}, invalid("provide at least one experiment catalog row")
	}
	if len(input.Rows) > maxCatalogRowsPerWrite {
		return CatalogView{}, invalid("a catalog write can include at most 64 rows")
	}
	normalized := make([]CatalogRowInput, 0, len(input.Rows))
	for _, row := range input.Rows {
		parsed, err := normalizeCatalogRow(row)
		if err != nil {
			return CatalogView{}, err
		}
		normalized = append(normalized, parsed)
	}
	before, err := s.workloadCounts(ctx, current.projectID)
	if err != nil {
		return CatalogView{}, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return CatalogView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	repo, err := findProjectRepository(ctx, tx, current.projectID, input.RepositoryID)
	if err != nil {
		return CatalogView{}, err
	}
	existingCount, err := tx.ExperimentCatalogRow.Query().Where(experimentcatalogrow.RepositoryIDEQ(repo.ID)).Count(ctx)
	if err != nil {
		return CatalogView{}, err
	}
	created := 0
	for _, row := range normalized {
		match, err := tx.ExperimentCatalogRow.Query().Where(
			experimentcatalogrow.RepositoryIDEQ(repo.ID),
			experimentcatalogrow.BranchEQ(row.Branch),
			experimentcatalogrow.CommitHashEQ(row.Hash),
			experimentcatalogrow.SettingEQ(row.Setting),
		).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return CatalogView{}, err
		}
		if match != nil {
			if match.Method == row.Method && match.Implementation == row.Implementation && match.Metric == row.Metric && match.Result == row.Result && match.Link == row.Link {
				continue
			}
			update := match.Update().
				SetMethod(row.Method).
				SetImplementation(row.Implementation).
				SetMetric(row.Metric).
				SetResult(row.Result).
				SetLink(row.Link)
			if _, err := update.Save(ctx); err != nil {
				return CatalogView{}, err
			}
			continue
		}
		if existingCount+created >= maxCatalogRowsPerRepository {
			return CatalogView{}, ErrCatalogLimit
		}
		create := tx.ExperimentCatalogRow.Create().
			SetTenantID(current.tenantID).
			SetProjectID(current.projectID).
			SetRepositoryID(repo.ID).
			SetBranch(row.Branch).
			SetSetting(row.Setting).
			SetMethod(row.Method).
			SetImplementation(row.Implementation).
			SetMetric(row.Metric).
			SetResult(row.Result).
			SetLink(row.Link).
			SetCommitHash(row.Hash)
		if current.tokenID != nil {
			create.SetAgentTokenID(*current.tokenID)
		}
		if _, err := create.Save(ctx); err != nil {
			return CatalogView{}, err
		}
		created++
	}
	if err := writeAudit(ctx, tx, current, "research.catalog_recorded", "repository", repo.PublicID.String(), map[string]any{
		"repository_id": repo.PublicID.String(),
		"rows":          len(normalized),
		"created":       created,
	}); err != nil {
		return CatalogView{}, err
	}
	if err := tx.Commit(); err != nil {
		return CatalogView{}, err
	}
	after, err := s.workloadCounts(ctx, current.projectID)
	if err != nil {
		return CatalogView{}, err
	}
	if after != before {
		return CatalogView{}, invalid("catalog ingest must not create an Experiment, Proposal, or budget reservation")
	}
	return s.catalog(ctx, current, repo.PublicID.String())
}

func (s *Service) catalog(ctx context.Context, current actor, repositoryID string) (CatalogView, error) {
	query := s.client.Repository.Query().Where(repository.ProjectIDEQ(current.projectID)).Order(ent.Asc(repository.FieldName))
	if strings.TrimSpace(repositoryID) != "" {
		publicID, err := uuid.Parse(strings.TrimSpace(repositoryID))
		if err != nil {
			return CatalogView{}, invalid("repository_id must be a Project repository ID")
		}
		query = query.Where(repository.PublicIDEQ(publicID))
	}
	repos, err := query.All(ctx)
	if err != nil {
		return CatalogView{}, err
	}
	if strings.TrimSpace(repositoryID) != "" && len(repos) == 0 {
		return CatalogView{}, ErrNotFound
	}
	ids := make([]int, 0, len(repos))
	for _, repo := range repos {
		ids = append(ids, repo.ID)
	}
	rows := []*ent.ExperimentCatalogRow{}
	if len(ids) > 0 {
		rows, err = s.client.ExperimentCatalogRow.Query().
			Where(experimentcatalogrow.ProjectIDEQ(current.projectID), experimentcatalogrow.RepositoryIDIn(ids...)).
			Order(ent.Asc(experimentcatalogrow.FieldBranch), ent.Asc(experimentcatalogrow.FieldSetting)).
			All(ctx)
		if err != nil {
			return CatalogView{}, err
		}
	}
	byRepo := map[int][]CatalogRowView{}
	repoPublic := map[int]string{}
	for _, repo := range repos {
		repoPublic[repo.ID] = repo.PublicID.String()
		byRepo[repo.ID] = []CatalogRowView{}
	}
	for _, row := range rows {
		byRepo[row.RepositoryID] = append(byRepo[row.RepositoryID], makeCatalogRowView(row, repoPublic[row.RepositoryID]))
	}
	views := make([]CatalogRepositoryView, 0, len(repos))
	for _, repo := range repos {
		view := CatalogRepositoryView{
			ID:                       repo.PublicID.String(),
			Name:                     repo.Name,
			SSHURL:                   repo.SSHURL,
			DefaultBranch:            repo.DefaultBranch,
			Status:                   string(repo.Status),
			ObservationWritesAllowed: gitrepository.ObservationWritesAllowed(string(repo.Status)),
			PendingNote:              gitrepository.PendingNote(string(repo.Status)),
			Rows:                     byRepo[repo.ID],
		}
		if repo.LastVerifiedAt != nil {
			stamp := repo.LastVerifiedAt.UTC()
			view.LastVerifiedAt = &stamp
		}
		views = append(views, view)
	}
	return CatalogView{ProjectID: current.projectPublic, Repositories: views, GeneratedAt: time.Now().UTC()}, nil
}

func makeCatalogRowView(record *ent.ExperimentCatalogRow, repositoryPublicID string) CatalogRowView {
	return CatalogRowView{
		ID:             record.PublicID.String(),
		RepositoryID:   repositoryPublicID,
		Branch:         record.Branch,
		Setting:        record.Setting,
		Method:         record.Method,
		Implementation: record.Implementation,
		Metric:         record.Metric,
		Result:         record.Result,
		Link:           record.Link,
		Hash:           record.CommitHash,
		CreatedAt:      record.CreatedAt.UTC(),
		UpdatedAt:      record.UpdatedAt.UTC(),
	}
}

func normalizeCatalogRow(input CatalogRowInput) (CatalogRowInput, error) {
	branch, err := normalizeCatalogBranch(input.Branch)
	if err != nil {
		return CatalogRowInput{}, err
	}
	hash, err := normalizeEvidenceCommit(input.Hash)
	if err != nil {
		return CatalogRowInput{}, err
	}
	if hash == "" {
		return CatalogRowInput{}, invalid("hash is required")
	}
	setting, err := normalizeText("setting", input.Setting, 2, maxCatalogSettingLength)
	if err != nil {
		return CatalogRowInput{}, err
	}
	method, err := normalizeText("method", input.Method, 2, maxCatalogMethodLength)
	if err != nil {
		return CatalogRowInput{}, err
	}
	implementation, err := normalizeText("implementation", input.Implementation, 2, maxCatalogImplementationLen)
	if err != nil {
		return CatalogRowInput{}, err
	}
	metric, err := normalizeText("metric", input.Metric, 1, maxCatalogMetricLength)
	if err != nil {
		return CatalogRowInput{}, err
	}
	result, err := normalizeText("result", input.Result, 1, maxCatalogResultLength)
	if err != nil {
		return CatalogRowInput{}, err
	}
	link, err := normalizeCatalogLink(input.Link)
	if err != nil {
		return CatalogRowInput{}, err
	}
	return CatalogRowInput{
		Branch: branch, Setting: setting, Method: method, Implementation: implementation,
		Metric: metric, Result: result, Link: link, Hash: hash,
	}, nil
}

func normalizeCatalogBranch(value string) (string, error) {
	branch := strings.TrimSpace(value)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	branch = strings.TrimPrefix(branch, "refs/tags/")
	branch = strings.TrimPrefix(branch, "origin/")
	if utf8.RuneCountInString(branch) > 255 || !catalogBranchPattern.MatchString(branch) {
		return "", invalid("branch must be a git ref using letters, numbers, dots, underscores, slashes, or hyphens")
	}
	if err := rejectSecrets(branch); err != nil {
		return "", err
	}
	return branch, nil
}

func normalizeCatalogLink(value string) (string, error) {
	link, err := normalizeOptionalText("link", value, maxCatalogLinkLength)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(link, " \t") {
		return "", invalid("link must be a single path or URL")
	}
	return link, nil
}

type workloadCounts struct {
	experiments int
	proposals   int
	budgets     int
}

func (s *Service) workloadCounts(ctx context.Context, projectID int) (workloadCounts, error) {
	experiments, err := s.client.Experiment.Query().Where(experiment.ProjectIDEQ(projectID)).Count(ctx)
	if err != nil {
		return workloadCounts{}, err
	}
	proposals, err := s.client.ExperimentProposal.Query().Where(experimentproposal.ProjectIDEQ(projectID)).Count(ctx)
	if err != nil {
		return workloadCounts{}, err
	}
	budgets, err := s.client.BudgetEntry.Query().Where(budgetentry.ProjectIDEQ(projectID)).Count(ctx)
	if err != nil {
		return workloadCounts{}, err
	}
	return workloadCounts{experiments: experiments, proposals: proposals, budgets: budgets}, nil
}
