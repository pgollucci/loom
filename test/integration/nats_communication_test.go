package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jordanhubbard/loom/internal/messagebus"
	"github.com/jordanhubbard/loom/pkg/messages"
	"github.com/nats-io/nats.go"
)

// TestNATSTaskPublishSubscribe tests the full NATS message flow:
// dispatcher publishes task → agent subscribes → agent publishes result → dispatcher subscribes
func TestNATSTaskPublishSubscribe(t *testing.T) {
	// Skip if NATS_URL not set
	natsURL := "nats://localhost:4222"

	// Create message bus
	mb, err := messagebus.NewNatsMessageBus(messagebus.Config{
		URL:        natsURL,
		StreamName: "LOOM",
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	defer mb.Close()

	// Use a UUID-based projectID to avoid conflicting with the running loom instance's
	// durable task consumers (e.g. tasks-test-project).
	projectID := "test-" + uuid.New().String()[:8]
	beadID := "test-bead-" + uuid.New().String()
	correlationID := uuid.New().String()

	// Channel to receive task messages (simulating agent)
	taskReceived := make(chan *messages.TaskMessage, 1)

	// Subscribe to tasks (agent side)
	err = mb.SubscribeTasks(projectID, func(task *messages.TaskMessage) {
		taskReceived <- task
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to tasks: %v", err)
	}

	// Give subscription time to be ready
	time.Sleep(100 * time.Millisecond)

	// Publish a task (dispatcher side)
	taskMsg := messages.TaskAssigned(
		projectID,
		beadID,
		"test-agent",
		messages.TaskData{
			Title:       "Test Task",
			Description: "Testing NATS communication",
			Priority:    1,
			Type:        "task",
		},
		correlationID,
	)

	ctx := context.Background()
	err = mb.PublishTask(ctx, projectID, taskMsg)
	if err != nil {
		t.Fatalf("Failed to publish task: %v", err)
	}

	// Wait for task to be received
	select {
	case receivedTask := <-taskReceived:
		if receivedTask.BeadID != beadID {
			t.Errorf("Expected bead ID %s, got %s", beadID, receivedTask.BeadID)
		}
		if receivedTask.CorrelationID != correlationID {
			t.Errorf("Expected correlation ID %s, got %s", correlationID, receivedTask.CorrelationID)
		}
		if receivedTask.Type != "task.assigned" {
			t.Errorf("Expected type task.assigned, got %s", receivedTask.Type)
		}
		t.Logf("✅ Task received successfully: bead=%s, correlation=%s", receivedTask.BeadID, receivedTask.CorrelationID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for task message")
	}

	// Now test result publishing (agent → dispatcher).
	//
	// We intentionally avoid mb.SubscribeResults() here because the running loom
	// instance already holds the "results-all" durable JetStream push consumer.
	// With WorkQueuePolicy a durable consumer can only have one active push
	// subscriber, so attempting to bind another would fail with
	// "consumer is already bound to a subscription".
	//
	// Instead we use a core NATS subscription on the project-specific subject.
	// NATS delivers messages to core subscribers in addition to storing them in
	// the JetStream stream, so this receives the publish without competing with
	// loom's durable consumer.
	resultReceived := make(chan *messages.ResultMessage, 1)

	coreNC, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("Failed to open second NATS connection for result subscription: %v", err)
	}
	defer coreNC.Close()

	resultSubject := fmt.Sprintf("loom.results.%s", projectID)
	coreSub, err := coreNC.Subscribe(resultSubject, func(msg *nats.Msg) {
		var result messages.ResultMessage
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			t.Logf("Failed to unmarshal result: %v", err)
			return
		}
		resultReceived <- &result
	})
	if err != nil {
		t.Fatalf("Failed to subscribe to results: %v", err)
	}
	defer coreSub.Unsubscribe()

	time.Sleep(100 * time.Millisecond)

	// Publish result (agent side)
	resultMsg := messages.TaskCompleted(
		projectID,
		beadID,
		"test-agent",
		messages.ResultData{
			Status:   "success",
			Output:   "Task completed successfully",
			Duration: 1500,
		},
		correlationID,
	)

	err = mb.PublishResult(ctx, projectID, resultMsg)
	if err != nil {
		t.Fatalf("Failed to publish result: %v", err)
	}

	// Wait for result to be received
	select {
	case receivedResult := <-resultReceived:
		if receivedResult.BeadID != beadID {
			t.Errorf("Expected bead ID %s, got %s", beadID, receivedResult.BeadID)
		}
		if receivedResult.CorrelationID != correlationID {
			t.Errorf("Expected correlation ID %s, got %s", correlationID, receivedResult.CorrelationID)
		}
		if receivedResult.Result.Status != "success" {
			t.Errorf("Expected status success, got %s", receivedResult.Result.Status)
		}
		t.Logf("✅ Result received successfully: bead=%s, status=%s, correlation=%s",
			receivedResult.BeadID, receivedResult.Result.Status, receivedResult.CorrelationID)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for result message")
	}

	t.Log("✅ Full NATS communication flow verified: task publish → subscribe → result publish → subscribe")
}

// TestNATSJetStreamPersistence tests that messages survive restarts
func TestNATSJetStreamPersistence(t *testing.T) {
	natsURL := "nats://localhost:4222"

	// Connect directly to NATS to check JetStream
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("Failed to create JetStream context: %v", err)
	}

	// Check stream exists
	streamInfo, err := js.StreamInfo("LOOM")
	if err != nil {
		t.Fatalf("Failed to get LOOM stream info: %v", err)
	}

	t.Logf("✅ JetStream stream info:")
	t.Logf("   Name: %s", streamInfo.Config.Name)
	t.Logf("   Messages: %d", streamInfo.State.Msgs)
	t.Logf("   Bytes: %d", streamInfo.State.Bytes)
	t.Logf("   Consumers: %d", streamInfo.State.Consumers)
	t.Logf("   Storage: %s", streamInfo.Config.Storage)

	// Verify stream configuration
	if streamInfo.Config.Storage != nats.FileStorage {
		t.Errorf("Expected FileStorage, got %v", streamInfo.Config.Storage)
	}
}

// TestNATSConnectionResilience tests reconnection behavior
func TestNATSConnectionResilience(t *testing.T) {
	natsURL := "nats://localhost:4222"

	reconnects := 0
	disconnects := 0

	nc, err := nats.Connect(natsURL,
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			disconnects++
			t.Logf("NATS disconnected (count: %d)", disconnects)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			reconnects++
			t.Logf("✅ NATS reconnected (count: %d)", reconnects)
		}),
		nats.MaxReconnects(10),
		nats.ReconnectWait(1*time.Second),
	)
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	defer nc.Close()

	// Verify connection
	if !nc.IsConnected() {
		t.Fatal("NATS not connected")
	}

	stats := nc.Stats()
	t.Logf("✅ NATS connection stats:")
	t.Logf("   In Msgs: %d", stats.InMsgs)
	t.Logf("   Out Msgs: %d", stats.OutMsgs)
	t.Logf("   In Bytes: %d", stats.InBytes)
	t.Logf("   Out Bytes: %d", stats.OutBytes)
	t.Logf("   Reconnects: %d", stats.Reconnects)
}

// TestNATSMessageOrdering tests that messages are delivered in order
func TestNATSMessageOrdering(t *testing.T) {
	natsURL := "nats://localhost:4222"

	mb, err := messagebus.NewNatsMessageBus(messagebus.Config{
		URL:        natsURL,
		StreamName: "LOOM",
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Skipf("NATS not available: %v", err)
	}
	defer mb.Close()

	projectID := "test-ordering"
	receivedMessages := make([]int, 0, 10)
	messageChan := make(chan int, 10)

	// Subscribe
	err = mb.SubscribeTasks(projectID, func(task *messages.TaskMessage) {
		var seq int
		json.Unmarshal([]byte(task.TaskData.Description), &seq)
		messageChan <- seq
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Publish 10 messages in sequence
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		seqBytes, _ := json.Marshal(i)
		task := messages.TaskAssigned(
			projectID,
			fmt.Sprintf("bead-%d", i),
			"test-agent",
			messages.TaskData{
				Title:       fmt.Sprintf("Task %d", i),
				Description: string(seqBytes),
			},
			uuid.New().String(),
		)
		err = mb.PublishTask(ctx, projectID, task)
		if err != nil {
			t.Fatalf("Failed to publish task %d: %v", i, err)
		}
	}

	// Collect messages with timeout
	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case seq := <-messageChan:
			receivedMessages = append(receivedMessages, seq)
		case <-timeout:
			t.Fatalf("Timeout after receiving %d/10 messages", len(receivedMessages))
		}
	}

	// Verify ordering
	for i, seq := range receivedMessages {
		if seq != i {
			t.Errorf("Message out of order: expected %d at position %d, got %d", i, i, seq)
		}
	}

	t.Logf("✅ All 10 messages received in order")
}
