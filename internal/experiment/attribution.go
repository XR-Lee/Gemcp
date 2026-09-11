package experiment

import (
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
)

func proposalAuditActor(principal agentauth.Principal) (actorType auditevent.ActorType, actorID string) {
	if principal.TokenID != 0 {
		return auditevent.ActorTypeAgentToken, principal.TokenPublicID
	}
	if id := strings.TrimSpace(principal.UserPublicID); id != "" {
		return auditevent.ActorTypeUser, id
	}
	return auditevent.ActorTypeUser, "owner"
}

func scopedProposalQuery(query *ent.ExperimentProposalQuery, principal agentauth.Principal) *ent.ExperimentProposalQuery {
	query = query.Where(experimentproposal.ProjectIDEQ(principal.ProjectID))
	if principal.TokenID != 0 {
		return query.Where(experimentproposal.AgentTokenIDEQ(principal.TokenID))
	}
	return query
}
