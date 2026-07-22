package nodeagent

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

type WorkloadRecord struct {
	AssignmentID            string    `json:"assignment_id"`
	ExperimentID            string    `json:"experiment_id"`
	AttemptID               string    `json:"attempt_id"`
	ContainerID             string    `json:"container_id"`
	OutputRef               string    `json:"output_ref"`
	State                   string    `json:"state"`
	StartedAt               time.Time `json:"started_at"`
	DeadlineAt              time.Time `json:"deadline_at"`
	TerminationGraceSeconds int       `json:"termination_grace_seconds"`
	StopReason              string    `json:"stop_reason,omitempty"`
	LastHeartbeatAt         time.Time `json:"last_heartbeat_at,omitempty"`
	StartedReported         bool      `json:"started_reported"`
}

func (s *Store) SaveWorkload(record WorkloadRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("node state is not initialized")
	}
	if _, err := uuid.Parse(record.AssignmentID); err != nil || record.ContainerID == "" || record.OutputRef == "" {
		return fmt.Errorf("workload record is invalid")
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode workload record: %w", err)
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(workloadsBucket).Put([]byte(record.AssignmentID), encoded)
	}); err != nil {
		return fmt.Errorf("persist workload record: %w", err)
	}
	return nil
}

func (s *Store) Workloads() ([]WorkloadRecord, error) {
	var result []WorkloadRecord
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(workloadsBucket).ForEach(func(_, value []byte) error {
			var record WorkloadRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return err
			}
			result = append(result, record)
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("read workload records: %w", err)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AssignmentID < result[j].AssignmentID })
	return result, nil
}

func (s *Store) DeleteWorkload(assignmentID string) error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(workloadsBucket).Delete([]byte(assignmentID))
	}); err != nil {
		return fmt.Errorf("delete workload record: %w", err)
	}
	return nil
}
