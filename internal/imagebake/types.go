package imagebake

import (
	"errors"
	"time"

	"github.com/XR-Lee/Gemcp/internal/validation"
)

const (
	BackendAutoDLPro = "autodl_pro"
	DefaultRecipe    = "requirements.gemcp.txt"
)

var (
	ErrNotFound       = errors.New("image bake not found")
	ErrProject        = errors.New("active Project was not found")
	ErrForbidden      = errors.New("forbidden")
	ErrBusy           = errors.New("an image bake is already in progress for this Project")
	ErrDigestMismatch = errors.New("image bake confirmation digest does not match")
	ErrNotRequested   = errors.New("only a requested image bake can be confirmed")
	ErrNotCancellable = errors.New("image bake cannot be cancelled in its current state")
	ErrProvider       = errors.New("AutoDL Pro bake backend is unavailable")
	ErrProviderCreate = errors.New("AutoDL Pro instance create failed")
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type RequestInput struct {
	Name          string `json:"name" jsonschema:"stable bake name using letters, numbers, dot, underscore, or hyphen"`
	Backend       string `json:"backend,omitempty" jsonschema:"autodl_pro"`
	BaseImageUUID string `json:"base_image_uuid" jsonschema:"Provider image UUID used as the bake base"`
	RepositoryID  string `json:"repository_id,omitempty" jsonschema:"active repository ID; optional when the Project has exactly one"`
	CommitSHA     string `json:"commit_sha" jsonschema:"full Git commit SHA for the bake recipe"`
	RecipePath    string `json:"recipe_path,omitempty" jsonschema:"relative recipe path; defaults to requirements.gemcp.txt"`
}

type GetInput struct {
	BakeID string `json:"bake_id" jsonschema:"image bake ID"`
}

type ConfirmInput struct {
	ConfirmationDigest string `json:"confirmation_digest"`
}

type Proposal struct {
	Backend       string `json:"backend"`
	Name          string `json:"name"`
	BaseImageUUID string `json:"base_image_uuid"`
	RepositoryID  string `json:"repository_id"`
	CommitSHA     string `json:"commit_sha"`
	RecipePath    string `json:"recipe_path"`
}

type View struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	RepositoryID       string     `json:"repository_id"`
	Name               string     `json:"name"`
	Backend            string     `json:"backend"`
	BaseImageUUID      string     `json:"base_image_uuid"`
	CommitSHA          string     `json:"commit_sha"`
	RecipePath         string     `json:"recipe_path"`
	Status             string     `json:"status"`
	ConfirmationDigest string     `json:"confirmation_digest"`
	RequestedBy        string     `json:"requested_by"`
	RequestedByType    string     `json:"requested_by_type"`
	ConfirmedBy        string     `json:"confirmed_by,omitempty"`
	ConfirmedAt        *time.Time `json:"confirmed_at,omitempty"`
	ImageUUID          string     `json:"image_uuid,omitempty"`
	InstanceUUID       string     `json:"instance_uuid,omitempty"`
	FailureReason      string     `json:"failure_reason,omitempty"`
	Proposal           Proposal   `json:"proposal"`
	EstimatedCostMilli int64      `json:"estimated_cost_milli"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type ListResult struct {
	Bakes []View `json:"bakes"`
}

type RepositoryOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

type ImageOption struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type Options struct {
	ProjectID    string             `json:"project_id"`
	Backend      string             `json:"backend"`
	RecipePath   string             `json:"default_recipe_path"`
	Repositories []RepositoryOption `json:"repositories"`
	BaseImages   []ImageOption      `json:"base_images"`
	GeneratedAt  time.Time          `json:"generated_at"`
}
