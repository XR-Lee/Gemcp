package nodeagent

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

var (
	metadataBucket  = []byte("metadata")
	eventsBucket    = []byte("events")
	commandsBucket  = []byte("commands")
	workloadsBucket = []byte("workloads")
	nextEventKey    = []byte("next_event_sequence")
	lastCommandKey  = []byte("last_command_sequence")
)

type Store struct {
	db *bolt.DB
}

type storedCommand struct {
	Command         nodeprotocol.Command                 `json:"command"`
	Acknowledgement *nodeprotocol.CommandAcknowledgement `json:"acknowledgement,omitempty"`
}

func OpenStore(filename string) (*Store, error) {
	if !filepath.IsAbs(filename) {
		return nil, fmt.Errorf("node state path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return nil, fmt.Errorf("create node state directory: %w", err)
	}
	db, err := bolt.Open(filename, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open node state: %w", err)
	}
	store := &Store{db: db}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{metadataBucket, eventsBucket, commandsBucket, workloadsBucket} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize node state: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) AppendEvent(kind string, payload map[string]any, occurredAt time.Time) (nodeprotocol.Event, error) {
	var event nodeprotocol.Event
	if s == nil || s.db == nil {
		return event, fmt.Errorf("node state is not initialized")
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		metadata := tx.Bucket(metadataBucket)
		sequence := decodeUint64(metadata.Get(nextEventKey)) + 1
		event = nodeprotocol.Event{
			ID: uuid.NewString(), Sequence: int64(sequence), Kind: kind, OccurredAt: occurredAt.UTC(), Payload: payload,
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if err := tx.Bucket(eventsBucket).Put(uint64Key(sequence), encoded); err != nil {
			return err
		}
		return metadata.Put(nextEventKey, uint64Key(sequence))
	})
	if err != nil {
		return event, fmt.Errorf("append node event: %w", err)
	}
	return event, nil
}

func (s *Store) PendingEvents(limit int) ([]nodeprotocol.Event, error) {
	if limit <= 0 {
		return []nodeprotocol.Event{}, nil
	}
	result := make([]nodeprotocol.Event, 0, limit)
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(eventsBucket).Cursor()
		for _, value := cursor.First(); value != nil && len(result) < limit; _, value = cursor.Next() {
			var event nodeprotocol.Event
			if err := json.Unmarshal(value, &event); err != nil {
				return err
			}
			result = append(result, event)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read node event outbox: %w", err)
	}
	return result, nil
}

func (s *Store) AckEvents(sequence int64) error {
	if sequence <= 0 {
		return nil
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(eventsBucket)
		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil && int64(decodeUint64(key)) <= sequence; key, _ = cursor.Next() {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("acknowledge node events: %w", err)
	}
	return nil
}

func (s *Store) SaveCommands(commands []nodeprotocol.Command) error {
	if len(commands) == 0 {
		return nil
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(commandsBucket)
		metadata := tx.Bucket(metadataBucket)
		last := decodeUint64(metadata.Get(lastCommandKey))
		for _, command := range commands {
			if command.Sequence <= 0 || command.ID == "" {
				return fmt.Errorf("invalid command identity")
			}
			key := uint64Key(uint64(command.Sequence))
			if existing := bucket.Get(key); existing != nil {
				var stored storedCommand
				if err := json.Unmarshal(existing, &stored); err != nil {
					return err
				}
				if stored.Command.ID != command.ID {
					return fmt.Errorf("command sequence conflict")
				}
				continue
			}
			encoded, err := json.Marshal(storedCommand{Command: command})
			if err != nil {
				return err
			}
			if err := bucket.Put(key, encoded); err != nil {
				return err
			}
			if uint64(command.Sequence) > last {
				last = uint64(command.Sequence)
			}
		}
		return metadata.Put(lastCommandKey, uint64Key(last))
	}); err != nil {
		return fmt.Errorf("persist node commands: %w", err)
	}
	return nil
}

func (s *Store) UnprocessedCommands() ([]nodeprotocol.Command, error) {
	var result []nodeprotocol.Command
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(commandsBucket).ForEach(func(_, value []byte) error {
			var stored storedCommand
			if err := json.Unmarshal(value, &stored); err != nil {
				return err
			}
			if stored.Acknowledgement == nil {
				result = append(result, stored.Command)
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("read unprocessed node commands: %w", err)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result, nil
}

func (s *Store) CompleteCommand(command nodeprotocol.Command, acknowledgement nodeprotocol.CommandAcknowledgement) error {
	if acknowledgement.CommandID != command.ID {
		return fmt.Errorf("command acknowledgement identity mismatch")
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(commandsBucket)
		key := uint64Key(uint64(command.Sequence))
		value := bucket.Get(key)
		if value == nil {
			return fmt.Errorf("command is not persisted")
		}
		var stored storedCommand
		if err := json.Unmarshal(value, &stored); err != nil {
			return err
		}
		stored.Acknowledgement = &acknowledgement
		encoded, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		return bucket.Put(key, encoded)
	}); err != nil {
		return fmt.Errorf("complete node command locally: %w", err)
	}
	return nil
}

func (s *Store) PendingAcknowledgements() ([]nodeprotocol.CommandAcknowledgement, error) {
	var result []nodeprotocol.CommandAcknowledgement
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(commandsBucket).ForEach(func(_, value []byte) error {
			var stored storedCommand
			if err := json.Unmarshal(value, &stored); err != nil {
				return err
			}
			if stored.Acknowledgement != nil {
				result = append(result, *stored.Acknowledgement)
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("read node command acknowledgements: %w", err)
	}
	return result, nil
}

func (s *Store) AckCommands(commandIDs []string) error {
	if len(commandIDs) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(commandIDs))
	for _, id := range commandIDs {
		wanted[id] = true
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(commandsBucket)
		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var stored storedCommand
			if err := json.Unmarshal(value, &stored); err != nil {
				return err
			}
			if stored.Acknowledgement != nil && wanted[stored.Command.ID] {
				if err := cursor.Delete(); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("acknowledge delivered command results: %w", err)
	}
	return nil
}

func (s *Store) LastCommandSequence() (int64, error) {
	var value uint64
	if err := s.db.View(func(tx *bolt.Tx) error {
		value = decodeUint64(tx.Bucket(metadataBucket).Get(lastCommandKey))
		return nil
	}); err != nil {
		return 0, fmt.Errorf("read last command sequence: %w", err)
	}
	return int64(value), nil
}

func uint64Key(value uint64) []byte {
	result := make([]byte, 8)
	binary.BigEndian.PutUint64(result, value)
	return result
}

func decodeUint64(value []byte) uint64 {
	if len(value) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}
