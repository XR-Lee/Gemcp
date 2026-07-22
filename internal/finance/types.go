package finance

import "time"

type Totals struct {
	BaseBudgetMilli int64 `json:"base_budget_milli"`
	ReservedMilli   int64 `json:"reserved_milli"`
	ChargedMilli    int64 `json:"charged_milli"`
	CreditsMilli    int64 `json:"credits_milli"`
	DebitsMilli     int64 `json:"debits_milli"`
	CommittedMilli  int64 `json:"committed_milli"`
	AvailableMilli  int64 `json:"available_milli"`
}

type ProjectSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Timezone string `json:"timezone"`
	Totals
}

type DailyPoint struct {
	Date          string `json:"date"`
	ReservedMilli int64  `json:"reserved_milli"`
	ChargedMilli  int64  `json:"charged_milli"`
	CreditsMilli  int64  `json:"credits_milli"`
	DebitsMilli   int64  `json:"debits_milli"`
}

type BackendSummary struct {
	Backend       string `json:"backend"`
	Experiments   int    `json:"experiments"`
	ReservedMilli int64  `json:"reserved_milli"`
	ChargedMilli  int64  `json:"charged_milli"`
}

type LedgerEntry struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	ProjectName        string    `json:"project_name"`
	ExperimentID       string    `json:"experiment_id,omitempty"`
	Backend            string    `json:"backend,omitempty"`
	Period             string    `json:"period"`
	Kind               string    `json:"kind"`
	Direction          string    `json:"direction,omitempty"`
	AmountMilli        int64     `json:"amount_milli"`
	BalanceEffectMilli int64     `json:"balance_effect_milli"`
	Description        string    `json:"description"`
	CreatedAt          time.Time `json:"created_at"`
}

type AuditEntry struct {
	ID         string    `json:"id"`
	ActorType  string    `json:"actor_type"`
	ActorID    string    `json:"actor_id,omitempty"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Dashboard struct {
	Period      string           `json:"period"`
	AuditScope  string           `json:"audit_scope"`
	Totals      Totals           `json:"totals"`
	Projects    []ProjectSummary `json:"projects"`
	Daily       []DailyPoint     `json:"daily"`
	Backends    []BackendSummary `json:"backends"`
	Ledger      []LedgerEntry    `json:"ledger"`
	Audit       []AuditEntry     `json:"audit"`
	GeneratedAt time.Time        `json:"generated_at"`
}

type AdjustmentInput struct {
	Direction      string `json:"direction"`
	AmountMilli    int64  `json:"amount_milli"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

type AdjustmentResult struct {
	Entry      LedgerEntry `json:"entry"`
	Idempotent bool        `json:"idempotent"`
}
