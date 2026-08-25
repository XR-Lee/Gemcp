package sshcloud

import (
	"context"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/ent/cloudsshprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/google/uuid"
)

type EnsureResult struct {
	NodeID          string
	Status          string
	Probed          bool
	Authorized      bool
	NvidiaReady     bool
	DockerVersion   string
	GPUNames        []string
	EnvironmentName string
	ProfileName     string
	ResolvedImage   string
	Message         string
}

func (s *Service) EnsureForProject(ctx context.Context, tenantID int, actorID, projectPublicID, image string) (EnsureResult, error) {
	var result EnsureResult
	if !s.Enabled() {
		return result, ErrDisabled
	}
	nodes, err := s.client.CloudSSHNode.Query().Where(
		cloudsshnode.TenantIDEQ(tenantID),
		cloudsshnode.StatusIn(cloudsshnode.StatusPendingProbe, cloudsshnode.StatusActive),
	).Order(ent.Asc(cloudsshnode.FieldID)).All(ctx)
	if err != nil {
		return result, err
	}
	if len(nodes) == 0 {
		result.Message = "no Cloud SSH node is registered"
		return result, nil
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		actorID = "agent"
	}
	_ = image
	var last error
	for _, node := range nodes {
		current := node
		if sshCloudNeedsProbe(current) {
			if _, probeErr := s.Probe(ctx, tenantID, actorID, current.PublicID.String()); probeErr != nil {
				last = probeErr
				continue
			}
			result.Probed = true
			reloaded, loadErr := s.client.CloudSSHNode.Query().Where(cloudsshnode.IDEQ(current.ID)).Only(ctx)
			if loadErr != nil {
				return result, loadErr
			}
			current = reloaded
		}
		if current.Status != cloudsshnode.StatusActive {
			continue
		}
		inv := inventoryFromMap(current.Inventory)
		result.NodeID = current.PublicID.String()
		result.Status = string(current.Status)
		result.NvidiaReady = inv.NvidiaReady
		result.GPUNames = inventoryGPUNames(current.Inventory)
		existing, found, existingErr := s.existingRuntime(ctx, tenantID, projectPublicID, current)
		if existingErr != nil {
			last = existingErr
			continue
		}
		if found {
			result.EnvironmentName = existing.Name
			result.ProfileName = existing.Name
			result.ResolvedImage = HostImage
			result.Message = "Cloud SSH node is ready for prepare_experiment"
			return result, nil
		}
		hasDefault, defaultErr := s.projectHasDefault(ctx, tenantID, projectPublicID)
		if defaultErr != nil {
			return result, defaultErr
		}
		if _, err := s.Authorize(ctx, tenantID, actorID, current.PublicID.String(), AuthorizeInput{
			ProjectIDs: []string{projectPublicID}, Image: HostImage, MakeDefault: !hasDefault,
		}); err != nil {
			last = err
			continue
		}
		result.Authorized = true
		result.EnvironmentName = runtimeName(current.Label, current.PublicID)
		result.ProfileName = result.EnvironmentName
		result.ResolvedImage = HostImage
		result.Message = "Cloud SSH node is ready for prepare_experiment"
		return result, nil
	}
	if result.Message == "" && last != nil {
		return result, last
	}
	if result.Message == "" {
		result.Message = "Cloud SSH node is not reachable"
	}
	return result, nil
}

func sshCloudNeedsProbe(node *ent.CloudSSHNode) bool {
	if node == nil {
		return false
	}
	if node.Status == cloudsshnode.StatusPendingProbe {
		return true
	}
	if node.Status != cloudsshnode.StatusActive {
		return false
	}
	return node.LastProbedAt == nil || time.Since(node.LastProbedAt.UTC()) >= autoProbeQuietPeriod
}

func (s *Service) projectHasDefault(ctx context.Context, tenantID int, projectPublicID string) (bool, error) {
	id, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return false, err
	}
	projectRecord, err := s.client.Project.Query().Where(project.PublicIDEQ(id), project.TenantIDEQ(tenantID)).Only(ctx)
	if err != nil {
		return false, err
	}
	return s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.IsDefaultEQ(true), environment.StatusEQ(environment.StatusApproved),
	).Exist(ctx)
}

func (s *Service) existingRuntime(ctx context.Context, tenantID int, projectPublicID string, node *ent.CloudSSHNode) (*ent.Environment, bool, error) {
	id, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return nil, false, err
	}
	projectRecord, err := s.client.Project.Query().Where(project.PublicIDEQ(id), project.TenantIDEQ(tenantID)).Only(ctx)
	if err != nil {
		return nil, false, err
	}
	access, err := s.client.CloudSSHProjectAccess.Query().Where(
		cloudsshprojectaccess.NodeIDEQ(node.ID), cloudsshprojectaccess.ProjectIDEQ(projectRecord.ID),
		cloudsshprojectaccess.StatusEQ(cloudsshprojectaccess.StatusActive),
	).Exist(ctx)
	if err != nil {
		return nil, false, err
	}
	if !access {
		return nil, false, nil
	}
	record, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.BackendEQ(environment.BackendSSHCloud),
		environment.RecipeRefEQ(recipePrefix+node.PublicID.String()),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}
