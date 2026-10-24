package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/XR-Lee/Gemcp/internal/research"
	"github.com/gin-gonic/gin"
)

// Owner confirmation runs the same Graph bind check as the Agent's submit, so
// a full Study or a stale origin must read as the research API says it, not
// as an internal failure.
func TestExperimentErrorsKeepGraphRefusals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := &ExperimentHandlers{}
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "node limit", err: research.ErrNodeLimit, status: http.StatusConflict, code: "RESEARCH_NODE_LIMIT", message: "research graph node limit reached"},
		{name: "wrapped edge limit", err: fmt.Errorf("bind: %w", research.ErrEdgeLimit), status: http.StatusConflict, code: "RESEARCH_EDGE_LIMIT", message: "research graph edge limit reached"},
		{name: "stale origin", err: &research.ValidationError{Message: "from_node_id must belong to an active Study"}, status: http.StatusBadRequest, code: "INVALID_RESEARCH", message: "from_node_id must belong to an active Study"},
		{name: "internal", err: errors.New("database is locked"), status: http.StatusInternalServerError, code: "EXPERIMENT_API_FAILED", message: "experiment operation failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			handlers.writeError(context, test.err)
			body := response.Body.String()
			if response.Code != test.status || !strings.Contains(body, `"code":"`+test.code+`"`) || !strings.Contains(body, `"message":"`+test.message+`"`) {
				t.Fatalf("response status=%d body=%s", response.Code, body)
			}
		})
	}
}
