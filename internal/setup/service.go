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
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/auth"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/projectpolicy"
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
	SkipProvider     bool                 `json:"skip_provider,omitempty"`
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
	Backend string `json:"backend,omitempty"`
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
	ProviderID        string `json:"provider_id,omitempty"`
	ProjectID         string `json:"project_id"`
	EnvironmentID     string `json:"environment_id,omitempty"`
	ResourceProfileID string `json:"resource_profile_id,omitempty"`
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
	var providerBackend provideraccount.Backend
	var executionBackend environment.Backend
	var encryptedToken string
	if !input.SkipProvider {
		providerBackend, executionBackend, err = backendsForProvider(input.Provider.BaseURL, input.Provider.Backend)
		if err != nil {
			return result, err
		}
		encryptedToken, err = providerservice.EncryptCredential(s.box, input.Provider.Token)
		if err != nil {
			return result, err
		}
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
	var provider *ent.ProviderAccount
	var environmentRecord *ent.Environment
	var resourceProfile *ent.ResourceProfile
	if !input.SkipProvider {
		provider, err = tx.ProviderAccount.Create().
			SetTenantID(tenant.ID).
			SetName(strings.TrimSpace(input.Provider.Name)).
			SetBaseURL(strings.TrimRight(strings.TrimSpace(input.Provider.BaseURL), "/")).
			SetBackend(providerBackend).
			SetCredentialCiphertext(encryptedToken).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("create provider account: %w", err)
		}
		environmentRecord, err = tx.Environment.Create().
			SetProjectID(project.ID).
			SetBackend(executionBackend).
			SetName(strings.TrimSpace(input.Environment.Name)).
			SetImageUUID(strings.TrimSpace(input.Environment.ImageUUID)).
			SetIsDefault(true).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("create environment: %w", err)
		}
		profile := input.ResourceProfile
		resourceProfile, err = tx.ResourceProfile.Create().
			SetProjectID(project.ID).
			SetBackend(resourceprofile.Backend(executionBackend)).
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
	}
	label := strings.TrimSpace(input.AgentTokenLabel)
	if label == "" {
		label = "default-agent"
	}
	createToken := tx.AgentToken.Create().
		SetProjectID(project.ID).
		SetLabel(label).
		SetPrefix(tokenPrefix).
		SetTokenHash(s.box.Digest("agent-token", agentToken))
	if input.SkipProvider {
		// Local skip_provider needs configure + operate_nodes so the CPU
		// loop can register a host, environment, and dataset without a
		// second Owner scope edit.
		createToken.SetScopes([]string{"read", "submit", "cancel", "configure", "operate_nodes"})
	}
	tokenRecord, err := createToken.Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create Agent token: %w", err)
	}
	metadata := map[string]any{"project_id": project.PublicID.String(), "provider_skipped": input.SkipProvider}
	if provider != nil {
		metadata["provider_id"] = provider.PublicID.String()
	}
	_, err = tx.AuditEvent.Create().
		SetTenantID(tenant.ID).
		SetActorType("user").
		SetActorID(owner.PublicID.String()).
		SetAction("system.initialized").
		SetTargetType("tenant").
		SetTargetID(tenant.PublicID.String()).
		SetMetadata(metadata).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("write setup audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit setup transaction: %w", err)
	}
	result = Result{
		TenantID:         tenant.PublicID.String(),
		OwnerID:          owner.PublicID.String(),
		ProjectID:        project.PublicID.String(),
		AgentToken:       agentToken,
		AgentTokenPrefix: tokenRecord.Prefix,
	}
	if provider != nil {
		result.ProviderID = provider.PublicID.String()
	}
	if environmentRecord != nil {
		result.EnvironmentID = environmentRecord.PublicID.String()
	}
	if resourceProfile != nil {
		result.ResourceProfileID = resourceProfile.PublicID.String()
	}
	return result, nil
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
	if strings.TrimSpace(input.Project.Name) == "" || !slugPattern.MatchString(strings.ToLower(strings.TrimSpace(input.Project.Slug))) {
		return invalid("project name and a 3-64 character lowercase slug are required")
	}
	if err := projectpolicy.Validate(projectpolicy.Limits{
		MonthlyBudgetMilli: input.Project.MonthlyBudgetMilli, MaxExperimentMilli: input.Project.MaxExperimentMilli,
		MaxConcurrency: input.Project.MaxConcurrency, MaxRuntimeSeconds: input.Project.MaxRuntimeSeconds,
		TimeoutExtensionSeconds: input.Project.TimeoutExtensionSeconds, TerminationGraceSeconds: input.Project.TerminationGraceSeconds,
	}); err != nil {
		var validation *projectpolicy.ValidationError
		if errors.As(err, &validation) {
			return invalid(validation.Message)
		}
		return err
	}
	if input.SkipProvider {
		return nil
	}
	providerURL, err := url.Parse(strings.TrimSpace(input.Provider.BaseURL))
	if err != nil || providerURL.Scheme != "https" || providerURL.Host == "" || providerURL.User != nil || providerURL.RawQuery != "" || providerURL.Fragment != "" {
		return invalid("provider base URL must be a credential-free HTTPS URL")
	}
	normalizedProviderURL := strings.TrimRight(providerURL.String(), "/")
	if normalizedProviderURL != autodl.DefaultBaseURL && normalizedProviderURL != autodl.PrivateBaseURL {
		return invalid("provider base URL must be an official AutoDL API URL")
	}
	if _, _, err := backendsForProvider(normalizedProviderURL, input.Provider.Backend); err != nil {
		return err
	}
	if strings.TrimSpace(input.Provider.Name) == "" || strings.TrimSpace(input.Provider.Token) == "" {
		return invalid("provider name and token are required")
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
	if normalizedProviderURL == autodl.PrivateBaseURL {
		if profile.Region != "private" || profile.CUDAFrom != profile.CUDATo {
			return invalid("AutoDL Private Cloud requires region private and one CUDA version")
		}
	} else if profile.Region == "private" {
		return invalid("AutoDL Public Elastic requires a public region")
	}
	return nil
}

func backendsForProvider(baseURL, requestedBackend string) (provideraccount.Backend, environment.Backend, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	requestedBackend = strings.ToLower(strings.TrimSpace(requestedBackend))
	if requestedBackend == "" {
		if baseURL == autodl.PrivateBaseURL {
			requestedBackend = "private"
		} else {
			requestedBackend = "elastic"
		}
	}
	switch requestedBackend {
	case "private":
		if baseURL != autodl.PrivateBaseURL {
			return "", "", invalid("AutoDL Private Cloud must use https://private.autodl.com")
		}
		return provideraccount.BackendPrivate, environment.BackendAutodlPrivate, nil
	case "elastic":
		if baseURL != autodl.DefaultBaseURL {
			return "", "", invalid("AutoDL Public Elastic must use https://api.autodl.com")
		}
		return provideraccount.BackendElastic, environment.BackendAutodlElastic, nil
	default:
		return "", "", invalid("provider backend must be private or elastic")
	}
}
