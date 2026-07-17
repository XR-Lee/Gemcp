package setup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	providerservice "github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/secrets"
)

const providerTokenAAD = providerservice.CredentialAAD

var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}[a-z0-9]$`)

var ErrAlreadyInitialized = errors.New("Gemcp is already initialized")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

type Service struct {
	client *ent.Client
	box    *secrets.Box
	now    func() time.Time
}

type Input struct {
	OrganizationName string               `json:"organization_name"`
	Owner            OwnerInput           `json:"owner"`
	Provider         ProviderInput        `json:"provider"`
	Project          ProjectInput         `json:"project"`
	Environment      EnvironmentInput     `json:"environment"`
	ResourceProfile  ResourceProfileInput `json:"resource_profile"`
	AgentTokenLabel  string               `json:"agent_token_label"`
}

type OwnerInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type ProviderInput struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

type ProjectInput struct {
	Name                    string `json:"name"`
	Slug                    string `json:"slug"`
	MonthlyBudgetMilli      int64  `json:"monthly_budget_milli"`
	MaxExperimentMilli      int64  `json:"max_experiment_milli"`
	MaxConcurrency          int    `json:"max_concurrency"`
	MaxRuntimeSeconds       int    `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds int    `json:"timeout_extension_seconds"`
	TerminationGraceSeconds int    `json:"termination_grace_seconds"`
}

type EnvironmentInput struct {
	Name      string `json:"name"`
	ImageUUID string `json:"image_uuid"`
}

type ResourceProfileInput struct {
	Name           string   `json:"name"`
	Region         string   `json:"region"`
	GPUNames       []string `json:"gpu_names"`
	GPUNum         int      `json:"gpu_num"`
	CUDAFrom       int      `json:"cuda_from"`
	CUDATo         int      `json:"cuda_to"`
	CPUFrom        int      `json:"cpu_from"`
	CPUTo          int      `json:"cpu_to"`
	MemoryFromGB   int      `json:"memory_from_gb"`
	MemoryToGB     int      `json:"memory_to_gb"`
	PriceFromMilli int64    `json:"price_from_milli"`
	PriceToMilli   int64    `json:"price_to_milli"`
	ReuseContainer bool     `json:"reuse_container"`
}

type Result struct {
	TenantID          string `json:"tenant_id"`
	OwnerID           string `json:"owner_id"`
	ProviderID        string `json:"provider_id"`
	ProjectID         string `json:"project_id"`
	EnvironmentID     string `json:"environment_id"`
	ResourceProfileID string `json:"resource_profile_id"`
	AgentToken        string `json:"agent_token"`
	AgentTokenPrefix  string `json:"agent_token_prefix"`
}

func NewService(client *ent.Client, box *secrets.Box) *Service {
	return &Service{client: client, box: box, now: time.Now}
}

func (s *Service) Initialized(ctx context.Context) (bool, error) {
	if s == nil || s.client == nil {
		return false, fmt.Errorf("setup service is not initialized")
	}
	return s.client.User.Query().Exist(ctx)
}

func (s *Service) Initialize(ctx context.Context, input Input) (Result, error) {
	var result Result
	if err := validateInput(input); err != nil {
		return result, err
	}
	if s == nil || s.client == nil || s.box == nil {
		return result, fmt.Errorf("setup service is not initialized")
	}

	passwordHash, err := auth.HashPassword(input.Owner.Password)
	if err != nil {
		return result, err
	}
	encryptedToken, err := providerservice.EncryptCredential(s.box, input.Provider.Token)
	if err != nil {
		return result, err
	}
	agentToken, tokenPrefix, err := secrets.RandomToken("gmc", 32)
	if err != nil {
		return result, err
	}

	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin setup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	initialized, err := tx.User.Query().Exist(ctx)
	if err != nil {
		return result, fmt.Errorf("check setup state: %w", err)
	}
	if initialized {
		return result, ErrAlreadyInitialized
	}

	tenant, err := tx.Tenant.Create().
		SetName(strings.TrimSpace(input.OrganizationName)).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create tenant: %w", err)
	}
	owner, err := tx.User.Create().
		SetTenantID(tenant.ID).
		SetEmail(strings.ToLower(strings.TrimSpace(input.Owner.Email))).
		SetPasswordHash(passwordHash).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create owner: %w", err)
	}
	provider, err := tx.ProviderAccount.Create().
		SetTenantID(tenant.ID).
		SetName(strings.TrimSpace(input.Provider.Name)).
		SetBaseURL(strings.TrimRight(strings.TrimSpace(input.Provider.BaseURL), "/")).
		SetCredentialCiphertext(encryptedToken).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create provider account: %w", err)
	}
	project, err := tx.Project.Create().
		SetTenantID(tenant.ID).
		SetName(strings.TrimSpace(input.Project.Name)).
		SetSlug(strings.ToLower(strings.TrimSpace(input.Project.Slug))).
		SetMonthlyBudgetMilli(input.Project.MonthlyBudgetMilli).
		SetMaxExperimentMilli(input.Project.MaxExperimentMilli).
		SetMaxConcurrency(input.Project.MaxConcurrency).
		SetMaxRuntimeSeconds(input.Project.MaxRuntimeSeconds).
		SetTimeoutExtensionSeconds(input.Project.TimeoutExtensionSeconds).
		SetTerminationGraceSeconds(input.Project.TerminationGraceSeconds).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create project: %w", err)
	}
	environment, err := tx.Environment.Create().
		SetProjectID(project.ID).
		SetName(strings.TrimSpace(input.Environment.Name)).
		SetImageUUID(strings.TrimSpace(input.Environment.ImageUUID)).
		SetIsDefault(true).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create environment: %w", err)
	}
	profile := input.ResourceProfile
	resourceProfile, err := tx.ResourceProfile.Create().
		SetProjectID(project.ID).
		SetName(strings.TrimSpace(profile.Name)).
		SetRegion(strings.TrimSpace(profile.Region)).
		SetGpuNames(profile.GPUNames).
		SetGpuNum(profile.GPUNum).
		SetCudaFrom(profile.CUDAFrom).
		SetCudaTo(profile.CUDATo).
		SetCPUFrom(profile.CPUFrom).
		SetCPUTo(profile.CPUTo).
		SetMemoryFromGB(profile.MemoryFromGB).
		SetMemoryToGB(profile.MemoryToGB).
		SetPriceFromMilli(profile.PriceFromMilli).
		SetPriceToMilli(profile.PriceToMilli).
		SetReuseContainer(profile.ReuseContainer).
		SetIsDefault(true).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create resource profile: %w", err)
	}
	label := strings.TrimSpace(input.AgentTokenLabel)
	if label == "" {
		label = "default-agent"
	}
	tokenRecord, err := tx.AgentToken.Create().
		SetProjectID(project.ID).
		SetLabel(label).
		SetPrefix(tokenPrefix).
		SetTokenHash(s.box.Digest("agent-token", agentToken)).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create Agent token: %w", err)
	}
	_, err = tx.AuditEvent.Create().
		SetTenantID(tenant.ID).
		SetActorType("user").
		SetActorID(owner.PublicID.String()).
		SetAction("system.initialized").
		SetTargetType("tenant").
		SetTargetID(tenant.PublicID.String()).
		SetMetadata(map[string]any{"project_id": project.PublicID.String(), "provider_id": provider.PublicID.String()}).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("write setup audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit setup transaction: %w", err)
	}
	return Result{
		TenantID:          tenant.PublicID.String(),
		OwnerID:           owner.PublicID.String(),
		ProviderID:        provider.PublicID.String(),
		ProjectID:         project.PublicID.String(),
		EnvironmentID:     environment.PublicID.String(),
		ResourceProfileID: resourceProfile.PublicID.String(),
		AgentToken:        agentToken,
		AgentTokenPrefix:  tokenRecord.Prefix,
	}, nil
}

func validateInput(input Input) error {
	if strings.TrimSpace(input.OrganizationName) == "" {
		return invalid("organization name is required")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(input.Owner.Email))
	if err != nil || !strings.EqualFold(address.Address, strings.TrimSpace(input.Owner.Email)) {
		return invalid("valid owner email is required")
	}
	if _, err := auth.HashPassword(input.Owner.Password); err != nil {
		return invalid(err.Error())
	}
	providerURL, err := url.Parse(strings.TrimSpace(input.Provider.BaseURL))
	if err != nil || providerURL.Scheme != "https" || providerURL.Host == "" || providerURL.User != nil || providerURL.RawQuery != "" || providerURL.Fragment != "" {
		return invalid("provider base URL must be a credential-free HTTPS URL")
	}
	normalizedProviderURL := strings.TrimRight(providerURL.String(), "/")
	if normalizedProviderURL != autodl.DefaultBaseURL && normalizedProviderURL != autodl.PrivateBaseURL {
		return invalid("provider base URL must be an official AutoDL API URL")
	}
	if strings.TrimSpace(input.Provider.Name) == "" || strings.TrimSpace(input.Provider.Token) == "" {
		return invalid("provider name and token are required")
	}
	if strings.TrimSpace(input.Project.Name) == "" || !slugPattern.MatchString(strings.ToLower(strings.TrimSpace(input.Project.Slug))) {
		return invalid("project name and a 3-64 character lowercase slug are required")
	}
	if input.Project.MonthlyBudgetMilli <= 0 || input.Project.MaxExperimentMilli <= 0 || input.Project.MaxExperimentMilli > input.Project.MonthlyBudgetMilli {
		return invalid("project budgets must be positive and the experiment cap cannot exceed the monthly budget")
	}
	if input.Project.MaxConcurrency <= 0 || input.Project.MaxRuntimeSeconds <= 0 || input.Project.TimeoutExtensionSeconds < 0 || input.Project.TerminationGraceSeconds < 0 {
		return invalid("project concurrency and runtime limits are invalid")
	}
	totalRuntime := int64(input.Project.MaxRuntimeSeconds) + int64(input.Project.TimeoutExtensionSeconds) + int64(input.Project.TerminationGraceSeconds)
	if totalRuntime > int64((30*24*time.Hour)/time.Second) || input.Project.TerminationGraceSeconds > 3600 {
		return invalid("project runtime, extension, and grace must fit within 30 days and grace must not exceed one hour")
	}
	if strings.TrimSpace(input.Environment.Name) == "" || strings.TrimSpace(input.Environment.ImageUUID) == "" {
		return invalid("default environment name and image UUID are required")
	}
	profile := input.ResourceProfile
	if strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.Region) == "" || len(profile.GPUNames) == 0 || profile.GPUNum <= 0 || profile.GPUNum > 4 {
		return invalid("resource profile name, region, GPU candidates, and GPU count are required")
	}
	if profile.CUDAFrom <= 0 || profile.CUDATo < profile.CUDAFrom || profile.CPUFrom <= 0 || profile.CPUTo < profile.CPUFrom || profile.MemoryFromGB <= 0 || profile.MemoryToGB < profile.MemoryFromGB {
		return invalid("resource profile CUDA, CPU, or memory range is invalid")
	}
	if profile.PriceFromMilli < 0 || profile.PriceToMilli <= 0 || profile.PriceToMilli < profile.PriceFromMilli {
		return invalid("resource profile price range is invalid")
	}
	return nil
}
