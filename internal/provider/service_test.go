package provider

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/provideraccount"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/internal/autodl"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
)

type fakeAPI struct {
	err            error
	systemImageErr error
}

func (f *fakeAPI) WalletBalance(context.Context) (autodl.WalletBalance, string, error) {
	if f.err != nil {
		return autodl.WalletBalance{}, "", f.err
	}
	return autodl.WalletBalance{Assets: 12345, Accumulate: 67890, VoucherBalance: 500}, "req-wallet", nil
}

func (f *fakeAPI) ElasticImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error) {
	if f.err != nil {
		return autodl.Page[autodl.Image]{}, "", f.err
	}
	return autodl.Page[autodl.Image]{List: []autodl.Image{{UUID: "image-private", FallbackName: "private image", Status: "finished"}}}, "req-private", nil
}

func (f *fakeAPI) ElasticGPUStock(_ context.Context, region string, _ map[string]any) (autodl.GPUStock, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	return autodl.GPUStock{"RTX 4090": {Idle: 3, Total: 8}}, "req-stock-" + region, nil
}

func (f *fakeAPI) PrivateSystemImages(context.Context, int, int) (autodl.Page[autodl.Image], string, error) {
	if f.systemImageErr != nil {
		return autodl.Page[autodl.Image]{}, "", f.systemImageErr
	}
	return autodl.Page[autodl.Image]{List: []autodl.Image{{UUID: "base-image-1", FallbackName: "torch", CUDAVersion: "11.8", ChipCorp: "nvidia", CPUArch: "x86"}}}, "req-system", nil
}

func (f *fakeAPI) PrivateElasticGPUStock(context.Context) (autodl.GPUStock, string, error) {
	return autodl.GPUStock{"NVIDIA GeForce RTX 3090": {Idle: 2, Total: 9}}, "req-stock", nil
}

func (f *fakeAPI) ElasticDeployments(context.Context, int, int, string) (autodl.Page[autodl.Deployment], string, error) {
	now := time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)
	return autodl.Page[autodl.Deployment]{List: []autodl.Deployment{{
		UUID: "deployment-1", Name: "experiment", Type: "Job", Status: "running", ReplicaNum: 1,
		ParallelismNum: 1, RunningNum: 1, ImageUUID: "base-image-1", ReuseContainer: true,
		CreatedAt: &now, UpdatedAt: &now,
	}}}, "req-deployments", nil
}

func (f *fakeAPI) ElasticContainersWithReleased(_ context.Context, deploymentID string, released bool, _, _ int) (autodl.Page[autodl.Container], string, error) {
	now := time.Date(2026, 7, 17, 1, 0, 1, 0, time.UTC)
	status := "running"
	if released {
		status = "in_cache"
	}
	return autodl.Page[autodl.Container]{List: []autodl.Container{{
		UUID: "container-1", DeploymentUUID: deploymentID, MachineID: "machine-1", Status: status,
		GPUName: "NVIDIA GeForce RTX 3090", GPUNum: 1, CPUNum: 8, MemoryBytes: 32 << 30,
		ImageUUID: "base-image-1", PriceMilli: 1000, CreatedAt: &now,
	}}}, "req-containers", nil
}

func (f *fakeAPI) ElasticEvents(context.Context, string, int, int, int) (autodl.Page[autodl.ContainerEvent], string, error) {
	return autodl.Page[autodl.ContainerEvent]{List: []autodl.ContainerEvent{{
		ContainerUUID: "container-1", Status: "running", CreatedAt: time.Date(2026, 7, 17, 1, 0, 2, 0, time.UTC),
	}}}, "req-events", nil
}

type providerFixture struct {
	client    *ent.Client
	box       *secrets.Box
	tenantID  int
	record    *ent.ProviderAccount
	fake      *fakeAPI
	service   *Service
	seenURL   string
	seenToken string
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	client := enttest.Open(t, dialect.SQLite, "file:"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { client.Close() })
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tenant, err := client.Tenant.Create().SetName("Test").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := EncryptCredential(box, "old-provider-token-abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatal(err)
	}
	record, err := client.ProviderAccount.Create().
		SetTenantID(tenant.ID).
		SetName("AutoDL Private Cloud").
		SetBaseURL(autodl.PrivateBaseURL).
		SetCredentialCiphertext(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &providerFixture{client: client, box: box, tenantID: tenant.ID, record: record, fake: &fakeAPI{}}
	fixture.service = NewService(client, box, "test", WithClientFactory(func(baseURL, token string) (api, error) {
		fixture.seenURL = baseURL
		fixture.seenToken = token
		return fixture.fake, nil
	}))
	fixture.service.now = func() time.Time { return time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC) }
	return fixture
}

func TestQueryResourcesDecryptsCredentialAndNormalizesData(t *testing.T) {
	fixture := newProviderFixture(t)
	snapshot, err := fixture.service.QueryResources(context.Background(), fixture.tenantID, "")
	if err != nil {
		t.Fatalf("QueryResources() error = %v", err)
	}
	if fixture.seenURL != autodl.PrivateBaseURL || fixture.seenToken != "old-provider-token-abcdefghijklmnopqrstuvwxyz" {
		t.Fatal("Provider credential was not passed to the private client factory")
	}
	if snapshot.Provider.Backend != "private" || snapshot.Provider.Status != "active" || snapshot.Provider.LastValidatedAt == nil {
		t.Fatalf("unexpected Provider summary: %+v", snapshot.Provider)
	}
	if snapshot.Wallet != nil {
		t.Fatalf("private cloud snapshot should omit wallet: %+v", snapshot.Wallet)
	}
	if len(snapshot.GPUStock) != 1 || snapshot.GPUStock[0].Idle != 2 || snapshot.GPUStock[0].Region != "" {
		t.Fatalf("unexpected GPU stock: %+v", snapshot.GPUStock)
	}
	if len(snapshot.SystemImages) != 1 || snapshot.SystemImages[0].Name != "torch" {
		t.Fatalf("unexpected system images: %+v", snapshot.SystemImages)
	}
	if len(snapshot.ActiveContainers) != 1 || snapshot.ActiveContainers[0].Released {
		t.Fatalf("unexpected active containers: %+v", snapshot.ActiveContainers)
	}
	if len(snapshot.CachedContainers) != 1 || !snapshot.CachedContainers[0].Released {
		t.Fatalf("unexpected cached containers: %+v", snapshot.CachedContainers)
	}
}

func TestQueryResourcesPublicElasticUsesWalletAndRegionalStock(t *testing.T) {
	fixture := newProviderFixture(t)
	ctx := context.Background()
	updated, err := fixture.record.Update().
		SetName("AutoDL Public Cloud").
		SetBaseURL(autodl.DefaultBaseURL).
		SetBackend(provideraccount.BackendElastic).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixture.record = updated
	project, err := fixture.client.Project.Create().
		SetTenantID(fixture.tenantID).SetName("Research").SetSlug("research").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.client.ResourceProfile.Create().
		SetProjectID(project.ID).
		SetBackend(resourceprofile.BackendAutodlElastic).
		SetName("public-4090").
		SetRegion("westDC2").
		SetGpuNames([]string{"RTX 4090"}).
		SetGpuNum(1).SetCudaFrom(118).SetCudaTo(128).
		SetCPUFrom(1).SetCPUTo(128).SetMemoryFromGB(1).SetMemoryToGB(512).
		SetPriceFromMilli(10).SetPriceToMilli(3000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := fixture.service.QueryResources(ctx, fixture.tenantID, "elastic")
	if err != nil {
		t.Fatalf("QueryResources() error = %v", err)
	}
	if fixture.seenURL != autodl.DefaultBaseURL {
		t.Fatalf("seen URL = %q", fixture.seenURL)
	}
	if snapshot.Provider.Backend != "elastic" {
		t.Fatalf("backend = %q", snapshot.Provider.Backend)
	}
	if snapshot.Wallet == nil || snapshot.Wallet.Assets != 12345 || snapshot.Wallet.VoucherBalance != 500 {
		t.Fatalf("wallet = %+v", snapshot.Wallet)
	}
	if len(snapshot.GPUStock) != 1 || snapshot.GPUStock[0].Region != "westDC2" || snapshot.GPUStock[0].Name != "RTX 4090" || snapshot.GPUStock[0].Idle != 3 {
		t.Fatalf("gpu stock = %+v", snapshot.GPUStock)
	}
	if len(snapshot.SystemImages) != 0 {
		t.Fatalf("public Elastic should not query system images: %+v", snapshot.SystemImages)
	}
}

func TestConfigurePublicElasticProvisionsDefaultRuntime(t *testing.T) {
	fixture := newProviderFixture(t)
	ctx := context.Background()
	projectRecord, err := fixture.client.Project.Create().
		SetTenantID(fixture.tenantID).SetName("Local Trial").SetSlug("local-trial").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	privateEnv, err := fixture.client.Environment.Create().
		SetProjectID(projectRecord.ID).SetName("default").SetImageUUID("private-image").SetIsDefault(true).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	privateProfile, err := fixture.client.ResourceProfile.Create().
		SetProjectID(projectRecord.ID).SetName("default").SetRegion("private").
		SetGpuNames([]string{"NVIDIA GeForce RTX 3090"}).SetGpuNum(1).
		SetCudaFrom(118).SetCudaTo(118).SetCPUFrom(1).SetCPUTo(128).
		SetMemoryFromGB(1).SetMemoryToGB(512).SetPriceFromMilli(10).SetPriceToMilli(9000).
		SetIsDefault(true).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := fixture.service.Configure(ctx, fixture.tenantID, "owner-public-id", ConfigureInput{
		Name: "AutoDL Public Cloud", BaseURL: autodl.DefaultBaseURL, Backend: "elastic",
		Token: "new-provider-token-abcdefghijklmnopqrstuvwxyz",
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if result.Provider.Backend != "elastic" || result.Provider.Status != "active" {
		t.Fatalf("provider = %+v", result.Provider)
	}
	accounts, err := fixture.client.ProviderAccount.Query().All(ctx)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts = %d, %v", len(accounts), err)
	}
	privateAccount, _ := fixture.client.ProviderAccount.Get(ctx, fixture.record.ID)
	if privateAccount.CredentialCiphertext != fixture.record.CredentialCiphertext {
		t.Fatal("adding Public Elastic replaced the Private Cloud credential")
	}
	if privateAccount.BaseURL != autodl.PrivateBaseURL {
		t.Fatalf("private base URL = %q", privateAccount.BaseURL)
	}
	elasticEnv, err := fixture.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.BackendEQ(environment.BackendAutodlElastic),
	).Only(ctx)
	if err != nil || elasticEnv.IsDefault || elasticEnv.ImageUUID != "image-private" || elasticEnv.Name != publicElasticRuntimeName {
		t.Fatalf("elastic env = %+v, %v", elasticEnv, err)
	}
	elasticProfile, err := fixture.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.BackendEQ(resourceprofile.BackendAutodlElastic),
	).Only(ctx)
	if err != nil || elasticProfile.IsDefault || elasticProfile.Region != defaultPublicElasticRegion {
		t.Fatalf("elastic profile = %+v, %v", elasticProfile, err)
	}
	privateEnv, _ = fixture.client.Environment.Get(ctx, privateEnv.ID)
	privateProfile, _ = fixture.client.ResourceProfile.Get(ctx, privateProfile.ID)
	if !privateEnv.IsDefault || !privateProfile.IsDefault {
		t.Fatalf("private defaults were stolen: env=%v profile=%v", privateEnv.IsDefault, privateProfile.IsDefault)
	}
}

func TestConfigurePublicElasticBecomesDefaultWithoutPrivateProvider(t *testing.T) {
	fixture := newProviderFixture(t)
	ctx := context.Background()
	if err := fixture.client.ProviderAccount.DeleteOneID(fixture.record.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	projectRecord, err := fixture.client.Project.Create().
		SetTenantID(fixture.tenantID).SetName("Local Trial").SetSlug("local-trial").
		SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result, err := fixture.service.Configure(ctx, fixture.tenantID, "owner-public-id", ConfigureInput{
		Name: "AutoDL Public Cloud", BaseURL: autodl.DefaultBaseURL, Backend: "elastic",
		Token: "new-provider-token-abcdefghijklmnopqrstuvwxyz",
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if result.Provider.Backend != "elastic" {
		t.Fatalf("provider = %+v", result.Provider)
	}
	count, err := fixture.client.ProviderAccount.Query().Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("account count = %d, %v", count, err)
	}
	elasticEnv, err := fixture.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.BackendEQ(environment.BackendAutodlElastic),
	).Only(ctx)
	if err != nil || !elasticEnv.IsDefault {
		t.Fatalf("elastic env = %+v, %v", elasticEnv, err)
	}
	elasticProfile, err := fixture.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.BackendEQ(resourceprofile.BackendAutodlElastic),
	).Only(ctx)
	if err != nil || !elasticProfile.IsDefault {
		t.Fatalf("elastic profile = %+v, %v", elasticProfile, err)
	}
}

func TestQueryResourcesSelectsRequestedBackendWhenBothConfigured(t *testing.T) {
	fixture := newProviderFixture(t)
	ctx := context.Background()
	ciphertext, err := EncryptCredential(fixture.box, "public-provider-token-abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.client.ProviderAccount.Create().
		SetTenantID(fixture.tenantID).
		SetName("AutoDL Public Cloud").
		SetBaseURL(autodl.DefaultBaseURL).
		SetBackend(provideraccount.BackendElastic).
		SetCredentialCiphertext(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	private, err := fixture.service.QueryResources(ctx, fixture.tenantID, "private")
	if err != nil {
		t.Fatalf("private query error = %v", err)
	}
	if fixture.seenURL != autodl.PrivateBaseURL || fixture.seenToken != "old-provider-token-abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("private query used %s / %s", fixture.seenURL, fixture.seenToken)
	}
	if private.Provider.Backend != "private" || private.Wallet != nil {
		t.Fatalf("private snapshot = %+v", private.Provider)
	}

	public, err := fixture.service.QueryResources(ctx, fixture.tenantID, "elastic")
	if err != nil {
		t.Fatalf("public query error = %v", err)
	}
	if fixture.seenURL != autodl.DefaultBaseURL || fixture.seenToken != "public-provider-token-abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("public query used %s / %s", fixture.seenURL, fixture.seenToken)
	}
	if public.Provider.Backend != "elastic" || public.Wallet == nil {
		t.Fatalf("public snapshot = %+v wallet=%+v", public.Provider, public.Wallet)
	}

	_, err = fixture.service.QueryResources(ctx, fixture.tenantID, "")
	var validation *ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Message, "backend is required") {
		t.Fatalf("empty backend error = %v", err)
	}
}

func TestSummariesReturnsBothConfiguredProviders(t *testing.T) {
	fixture := newProviderFixture(t)
	ctx := context.Background()
	ciphertext, err := EncryptCredential(fixture.box, "public-provider-token-abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.client.ProviderAccount.Create().
		SetTenantID(fixture.tenantID).
		SetName("AutoDL Public Cloud").
		SetBaseURL(autodl.DefaultBaseURL).
		SetBackend(provideraccount.BackendElastic).
		SetCredentialCiphertext(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	summaries, err := fixture.service.Summaries(ctx, fixture.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].Name != "AutoDL Private Cloud" || summaries[1].Name != "AutoDL Public Cloud" {
		t.Fatalf("summaries = %+v", summaries)
	}
}

func TestConfigureRejectsPublicProBackend(t *testing.T) {
	fixture := newProviderFixture(t)
	_, err := fixture.service.Configure(context.Background(), fixture.tenantID, "owner", ConfigureInput{
		Name: "AutoDL Public Pro", BaseURL: autodl.DefaultBaseURL, Backend: "pro", Token: "new-provider-token-abcdefghijklmnopqrstuvwxyz",
	})
	var validation *ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Message, "private or elastic") {
		t.Fatalf("Configure() pro backend error = %v", err)
	}
}

func TestQueryResourcesTreatsWebOnlySystemImagesAsOptional(t *testing.T) {
	fixture := newProviderFixture(t)
	fixture.fake.systemImageErr = &autodl.ProviderError{HTTPStatus: 200, Code: "AuthorizeFailed", Message: "login expired"}
	snapshot, err := fixture.service.QueryResources(context.Background(), fixture.tenantID, "")
	if err != nil {
		t.Fatalf("QueryResources() error = %v", err)
	}
	if len(snapshot.SystemImages) != 0 || !slices.Contains(snapshot.Truncated, "system_images") {
		t.Fatalf("unexpected optional system images: images=%+v truncated=%+v", snapshot.SystemImages, snapshot.Truncated)
	}
	if len(snapshot.PrivateImages) != 1 || len(snapshot.GPUStock) != 1 || len(snapshot.Deployments) != 1 {
		t.Fatalf("official Developer API resources were not retained: %+v", snapshot)
	}
}

func TestConfigureValidatesBeforePersistingAndAuditsRotation(t *testing.T) {
	fixture := newProviderFixture(t)
	ctx := context.Background()
	originalCiphertext := fixture.record.CredentialCiphertext
	fixture.fake.err = errors.New("provider unavailable")
	_, err := fixture.service.Configure(ctx, fixture.tenantID, "owner-public-id", ConfigureInput{
		Name: "Private", BaseURL: autodl.PrivateBaseURL, Token: "new-provider-token-abcdefghijklmnopqrstuvwxyz",
	})
	if err == nil {
		t.Fatal("Configure() succeeded when validation failed")
	}
	unchanged, _ := fixture.client.ProviderAccount.Get(ctx, fixture.record.ID)
	if unchanged.CredentialCiphertext != originalCiphertext {
		t.Fatal("failed validation changed the stored credential")
	}

	fixture.fake.err = nil
	result, err := fixture.service.Configure(ctx, fixture.tenantID, "owner-public-id", ConfigureInput{
		Name: "Private", BaseURL: autodl.PrivateBaseURL, Token: "new-provider-token-abcdefghijklmnopqrstuvwxyz",
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if result.Provider.Status != "active" || result.Provider.Backend != "private" {
		t.Fatalf("unexpected result: %+v", result.Provider)
	}
	updated, _ := fixture.client.ProviderAccount.Get(ctx, fixture.record.ID)
	if updated.CredentialCiphertext == originalCiphertext || updated.CredentialCiphertext == "new-provider-token-abcdefghijklmnopqrstuvwxyz" {
		t.Fatal("Provider credential was not rotated as ciphertext")
	}
	plaintext, err := fixture.box.Decrypt(updated.CredentialCiphertext, CredentialAAD)
	if err != nil || string(plaintext) != "new-provider-token-abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("stored credential is not the validated token: %q, %v", plaintext, err)
	}
	audit, err := fixture.client.AuditEvent.Query().Where(auditevent.ActionEQ("provider.credential_rotated")).Only(ctx)
	if err != nil || audit.ActorID != "owner-public-id" || audit.TargetID != updated.PublicID.String() {
		t.Fatalf("unexpected audit event: %+v, %v", audit, err)
	}
}

func TestProviderTenantIsolationAndUnsupportedHost(t *testing.T) {
	fixture := newProviderFixture(t)
	other, _ := fixture.client.Tenant.Create().SetName("Other").Save(context.Background())
	if _, err := fixture.service.Summary(context.Background(), other.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Summary() cross-tenant error = %v", err)
	}
	_, err := fixture.service.Configure(context.Background(), fixture.tenantID, "owner", ConfigureInput{
		Name: "Bad", BaseURL: "https://example.com", Token: "new-provider-token-abcdefghijklmnopqrstuvwxyz",
	})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("Configure() unknown host error = %v", err)
	}
}

func TestDeploymentDetailsAreScopedAndSanitized(t *testing.T) {
	fixture := newProviderFixture(t)
	details, err := fixture.service.Deployment(context.Background(), fixture.tenantID, "deployment-1")
	if err != nil {
		t.Fatalf("Deployment() error = %v", err)
	}
	if details.Deployment.UUID != "deployment-1" || len(details.Events) != 1 {
		t.Fatalf("unexpected details: %+v", details)
	}
	if len(details.ActiveContainers) != 1 || details.ActiveContainers[0].MachineID != "machine-1" {
		t.Fatalf("unexpected containers: %+v", details.ActiveContainers)
	}
	if _, err := fixture.service.Deployment(context.Background(), fixture.tenantID, "../bad"); err == nil {
		t.Fatal("Deployment() accepted an unsafe ID")
	}
}

func TestOperationErrorExplainsElasticAccessDenied(t *testing.T) {
	err := (&OperationError{Operation: "deployment query", Cause: &autodl.ProviderError{
		Code: "BadRequest", Message: "无当前资源访问权限", RequestID: "req-denied",
	}}).Error()
	if !strings.Contains(err, "enterprise verification") || !strings.Contains(err, "弹性部署") || !strings.Contains(err, "req-denied") {
		t.Fatalf("unhelpful access-denied error: %q", err)
	}
	if strings.Contains(err, "BadRequest") {
		t.Fatalf("raw Provider code leaked into the access-denied error: %q", err)
	}
}

func TestOperationErrorBoundsProviderMessage(t *testing.T) {
	err := (&OperationError{Operation: "query", Cause: &autodl.ProviderError{
		Code: "Denied", Message: strings.Repeat("secret-like detail ", 40), RequestID: "req-1",
	}}).Error()
	if len([]rune(err)) > 320 || strings.Contains(err, "\n") || !strings.Contains(err, "Denied") {
		t.Fatalf("unsafe operation error: %q", err)
	}
}

func TestCollectPagesMarksSafetyLimit(t *testing.T) {
	calls := 0
	items, truncated, err := collectPages(context.Background(), func(page, _ int) (autodl.Page[int], string, error) {
		calls++
		return autodl.Page[int]{List: []int{page}, MaxPage: maxProviderPages + 2, ResultTotal: maxProviderPages + 2}, "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || calls != maxProviderPages || len(items) != maxProviderPages {
		t.Fatalf("items=%d calls=%d truncated=%v", len(items), calls, truncated)
	}
}

func TestSummaryDoesNotExposeCiphertext(t *testing.T) {
	fixture := newProviderFixture(t)
	summary, err := fixture.service.Summary(context.Background(), fixture.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.CredentialConfigured || summary.ID == "" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Status != string(provideraccount.StatusPendingValidation) {
		t.Fatalf("status = %q", summary.Status)
	}
}
