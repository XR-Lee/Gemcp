package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/projectpolicy"
	"github.com/gin-gonic/gin"
)

type ProjectHandlers struct {
	client *ent.Client
	policy *projectpolicy.Service
}

func NewProjectHandlers(client *ent.Client) *ProjectHandlers {
	return &ProjectHandlers{client: client, policy: projectpolicy.NewService(client)}
}

type projectView struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	Slug                    string `json:"slug"`
	Status                  string `json:"status"`
	MonthlyBudgetMilli      int64  `json:"monthly_budget_milli"`
	MaxExperimentMilli      int64  `json:"max_experiment_milli"`
	MaxConcurrency          int    `json:"max_concurrency"`
	MaxRuntimeSeconds       int    `json:"max_runtime_seconds"`
	TimeoutExtensionSeconds int    `json:"timeout_extension_seconds"`
	TerminationGraceSeconds int    `json:"termination_grace_seconds"`
	Timezone                string `json:"timezone"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

func (h *ProjectHandlers) List(c *gin.Context) {
	principal, ok := currentPrincipal(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	records, err := h.client.Project.Query().
		Where(project.TenantIDEQ(principal.TenantID)).
		Order(ent.Asc(project.FieldName)).
		All(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "PROJECT_LIST_FAILED", "project list failed")
		return
	}
	views := make([]projectView, 0, len(records))
	for _, record := range records {
		views = append(views, projectView{
			ID: record.PublicID.String(), Name: record.Name, Slug: record.Slug, Status: string(record.Status),
			MonthlyBudgetMilli: record.MonthlyBudgetMilli, MaxExperimentMilli: record.MaxExperimentMilli,
			MaxConcurrency: record.MaxConcurrency, MaxRuntimeSeconds: record.MaxRuntimeSeconds,
			TimeoutExtensionSeconds: record.TimeoutExtensionSeconds, TerminationGraceSeconds: record.TerminationGraceSeconds,
			Timezone:  record.Timezone,
			CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

func (h *ProjectHandlers) Update(c *gin.Context) {
	principal, ok := ownerPrincipal(c)
	if !ok {
		return
	}
	var input projectpolicy.UpdateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_PROJECT_POLICY", "project policy update must include at least one limit")
		return
	}
	record, err := h.policy.Update(c.Request.Context(), principal.TenantID, principal.UserPublicID, c.Param("id"), input)
	if err != nil {
		var validation *projectpolicy.ValidationError
		switch {
		case errors.As(err, &validation):
			writeError(c, http.StatusBadRequest, "INVALID_PROJECT_POLICY", validation.Message)
		case errors.Is(err, projectpolicy.ErrNotFound):
			writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "active Project not found")
		default:
			slog.Error("project policy update failed", "error", err)
			writeError(c, http.StatusInternalServerError, "PROJECT_UPDATE_FAILED", "project policy update failed")
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": projectView{
		ID: record.PublicID.String(), Name: record.Name, Slug: record.Slug, Status: string(record.Status),
		MonthlyBudgetMilli: record.MonthlyBudgetMilli, MaxExperimentMilli: record.MaxExperimentMilli,
		MaxConcurrency: record.MaxConcurrency, MaxRuntimeSeconds: record.MaxRuntimeSeconds,
		TimeoutExtensionSeconds: record.TimeoutExtensionSeconds, TerminationGraceSeconds: record.TerminationGraceSeconds,
		Timezone:  record.Timezone,
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
	}})
}
