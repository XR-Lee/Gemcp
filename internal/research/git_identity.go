package research

import "strings"

// GitIdentity is the durable source pin for a prepared or submitted Experiment.
// DefaultBranch is informational only and is never a fallback live ref.
type GitIdentity struct {
	RepositoryID   string `json:"repository_id,omitempty"`
	RepositoryName string `json:"repository_name,omitempty"`
	Repository     string `json:"repository,omitempty"`
	RequestedRef   string `json:"requested_ref,omitempty"`
	CommitSHA      string `json:"commit_sha,omitempty"`
	DefaultBranch  string `json:"default_branch,omitempty"`
	HostProcess    bool   `json:"host_process,omitempty"`
}

func (g GitIdentity) Empty() bool {
	return strings.TrimSpace(g.RepositoryID) == "" &&
		strings.TrimSpace(g.Repository) == "" &&
		strings.TrimSpace(g.RequestedRef) == "" &&
		strings.TrimSpace(g.CommitSHA) == "" &&
		!g.HostProcess
}
