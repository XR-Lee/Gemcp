package nodeagent

import (
	"testing"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/google/uuid"
)

func TestStorePersistsEventOutboxAndCommandResults(t *testing.T) {
	filename := t.TempDir() + "/state.db"
	store, err := OpenStore(filename)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	first, err := store.AppendEvent("heartbeat", map[string]any{"state": "online"}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendEvent("heartbeat", map[string]any{"state": "online"}, now.Add(time.Second))
	if err != nil || first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("events=%+v %+v err=%v", first, second, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events, err := store.PendingEvents(10)
	if err != nil || len(events) != 2 || events[0].ID != first.ID || events[1].ID != second.ID {
		t.Fatalf("pending events=%+v err=%v", events, err)
	}
	if err := store.AckEvents(1); err != nil {
		t.Fatal(err)
	}
	events, _ = store.PendingEvents(10)
	if len(events) != 1 || events[0].Sequence != 2 {
		t.Fatalf("events after ack=%+v", events)
	}
	command := nodeprotocol.Command{ID: uuid.NewString(), Sequence: 1, Kind: "reconcile", Payload: map[string]any{"reason": "test"}}
	if err := store.SaveCommands([]nodeprotocol.Command{command, command}); err != nil {
		t.Fatal(err)
	}
	commands, err := store.UnprocessedCommands()
	if err != nil || len(commands) != 1 || commands[0].ID != command.ID {
		t.Fatalf("commands=%+v err=%v", commands, err)
	}
	acknowledgement := nodeprotocol.CommandAcknowledgement{CommandID: command.ID, Status: "completed", Result: map[string]any{"ok": true}}
	if err := store.CompleteCommand(command, acknowledgement); err != nil {
		t.Fatal(err)
	}
	acks, err := store.PendingAcknowledgements()
	if err != nil || len(acks) != 1 || acks[0].CommandID != command.ID {
		t.Fatalf("acks=%+v err=%v", acks, err)
	}
	if sequence, err := store.LastCommandSequence(); err != nil || sequence != 1 {
		t.Fatalf("last sequence=%d err=%v", sequence, err)
	}
	if err := store.AckCommands([]string{command.ID}); err != nil {
		t.Fatal(err)
	}
	acks, _ = store.PendingAcknowledgements()
	if len(acks) != 0 {
		t.Fatalf("acknowledgements remain after server confirmation: %+v", acks)
	}
}
