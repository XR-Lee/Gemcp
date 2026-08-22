package httpapi

import (
	"net/http"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/gin-gonic/gin"
)

type ProjectHandlers struct {
	client *ent.Client
}

func NewProjectHandlers(client *ent.Client) *ProjectHandlers {
	return &ProjectHandlers{client: client}
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
			Timezone: record.Timezone,
			CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}
