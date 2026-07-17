package phasezero

import (
	"context"
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/internal/autodl"
)

type fakeJobAPI struct {
	polls               int
	created             bool
	stopped             bool
	deleted             bool
	lastCreate          autodl.ElasticDeploymentCreate
	lastPrivateCreate   autodl.PrivateElasticDeploymentCreate
	runningWithFinished bool
}

func (f *fakeJobAPI) CreateElasticDeployment(_ context.Context, input autodl.ElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error) {
	f.created = true
	f.lastCreate = input
	return autodl.DeploymentCreateResult{DeploymentUUID: "deployment-1"}, "req-create", nil
}

func (f *fakeJobAPI) CreatePrivateElasticDeployment(_ context.Context, input autodl.PrivateElasticDeploymentCreate) (autodl.DeploymentCreateResult, string, error) {
	f.created = true
	f.lastPrivateCreate = input
	return autodl.DeploymentCreateResult{DeploymentUUID: "deployment-private"}, "req-create-private", nil
}

func (f *fakeJobAPI) ElasticDeployments(context.Context, int, int, string) (autodl.Page[autodl.Deployment], string, error) {
	f.polls++
	deployment := autodl.Deployment{UUID: "deployment-1", Status: "running", RunningNum: 1}
	if f.polls >= 2 {
		if !f.runningWithFinished {
			deployment.Status = "stopped"
		}
		deployment.RunningNum = 0
		deployment.FinishedNum = 1
	}
	return autodl.Page[autodl.Deployment]{List: []autodl.Deployment{deployment}}, "req-deployment", nil
}

func (f *fakeJobAPI) ElasticContainers(context.Context, string, int, int) (autodl.Page[autodl.Container], string, error) {
	return autodl.Page[autodl.Container]{List: []autodl.Container{{UUID: "container-1", Status: "running", PriceMilli: 2000}}}, "req-container", nil
}

func (f *fakeJobAPI) ElasticContainersWithReleased(context.Context, string, bool, int, int) (autodl.Page[autodl.Container], string, error) {
	return autodl.Page[autodl.Container]{List: []autodl.Container{{UUID: "container-1", Status: "in_cache", PriceMilli: 2000}}}, "req-released-container", nil
}

func (f *fakeJobAPI) ElasticEvents(context.Context, string, int, int, int) (autodl.Page[autodl.ContainerEvent], string, error) {
	return autodl.Page[autodl.ContainerEvent]{List: []autodl.ContainerEvent{{ContainerUUID: "container-1", Status: "running"}}}, "req-events", nil
}

func (f *fakeJobAPI) StopElasticDeployment(context.Context, string) (string, error) {
	f.stopped = true
	return "req-stop", nil
}

func (f *fakeJobAPI) DeleteElasticDeployment(context.Context, string) (string, error) {
	f.deleted = true
	return "req-delete", nil
}

func TestJobRunnerCompletesAndCleansUp(t *testing.T) {
	api := &fakeJobAPI{}
	clock := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	runner := NewJobRunner(api)
	runner.Now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	runner.Sleep = func(context.Context, time.Duration) error { return nil }

	report, err := runner.Run(context.Background(), validJobSpec(), 100)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !api.created || !api.stopped || !api.deleted {
		t.Fatalf("lifecycle calls: created=%v stopped=%v deleted=%v", api.created, api.stopped, api.deleted)
	}
	if report.DeploymentUUID != "deployment-1" || report.TerminalStatus != "stopped" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.RequestIDs["cleanup_delete"] != "req-delete" || report.RequestIDs["released_containers"] != "req-released-container" {
		t.Fatalf("cleanup request IDs missing: %+v", report.RequestIDs)
	}
	if len(report.ReleasedContainers) != 1 || report.ReleasedContainers[0].Status != "in_cache" {
		t.Fatalf("released containers missing: %+v", report.ReleasedContainers)
	}
	if api.lastCreate.DeploymentType != "Job" || api.lastCreate.ReuseContainer {
		t.Fatalf("unexpected deployment: %+v", api.lastCreate)
	}
	if api.lastCreate.ContainerTemplate.Command == "" {
		t.Fatal("probe command is empty")
	}
}

func TestJobRunnerTreatsFinishedCountAsTerminal(t *testing.T) {
	api := &fakeJobAPI{runningWithFinished: true}
	runner := NewJobRunner(api)
	runner.Sleep = func(context.Context, time.Duration) error { return nil }

	report, err := runner.Run(context.Background(), validJobSpec(), 100)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.ProviderStatus != "running" || report.TerminalStatus != "finished" || report.FinishedNum != 1 {
		t.Fatalf("unexpected terminal observation: %+v", report)
	}
}

func TestPrivateJobRunnerUsesPrivateContract(t *testing.T) {
	api := &fakeJobAPI{}
	runner := NewJobRunner(api)
	runner.Sleep = func(context.Context, time.Duration) error { return nil }
	spec := validJobSpec()
	spec.Backend = "private"
	spec.Region = ""
	spec.CUDAFrom = 0
	spec.CUDATo = 0
	spec.CUDAVersion = 118

	report, err := runner.Run(context.Background(), spec, 100)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Backend != "private" || report.DeploymentUUID != "deployment-private" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if api.lastPrivateCreate.ContainerTemplate.CUDAVersion != 118 {
		t.Fatalf("private deployment = %+v", api.lastPrivateCreate)
	}
	if api.lastCreate.Name != "" {
		t.Fatalf("public create path was used: %+v", api.lastCreate)
	}
}

func TestJobRunnerRejectsSpendBeforeProviderCall(t *testing.T) {
	api := &fakeJobAPI{}
	runner := NewJobRunner(api)
	_, err := runner.Run(context.Background(), validJobSpec(), 20)
	if err == nil {
		t.Fatal("Run() accepted insufficient spend cap")
	}
	if api.created {
		t.Fatal("provider was called before spend rejection")
	}
}
