package dispatch

import (
	"context"
	"testing"

	"github.com/jordanhubbard/loom/pkg/messages"
)

// MockSimpleMessageBus implements the MessageBus interface for testing
type MockSimpleMessageBus struct {
	publishedTasks []*messages.TaskMessage
	publishCount   int
}

func (m *MockSimpleMessageBus) PublishTask(ctx context.Context, projectID string, task *messages.TaskMessage) error {
	m.publishedTasks = append(m.publishedTasks, task)
	m.publishCount++
	return nil
}

// TestMessageBusInterface verifies the MessageBus interface contract
func TestMessageBusInterface(t *testing.T) {
	var _ MessageBus = (*MockSimpleMessageBus)(nil)

	t.Log("✅ MockSimpleMessageBus implements MessageBus interface")
}

// TestSetMessageBusOnDispatcher verifies message bus can be set
func TestSetMessageBusOnDispatcher(t *testing.T) {
	d := &Dispatcher{}

	mockBus := &MockSimpleMessageBus{}
	d.SetMessageBus(mockBus)

	if d.messageBus == nil {
		t.Fatal("MessageBus not set on dispatcher")
	}

	t.Log("✅ Message bus successfully set on dispatcher")
}

// TestTaskMessagePublishing verifies task messages can be published
func TestTaskMessagePublishing(t *testing.T) {
	mockBus := &MockSimpleMessageBus{}

	taskMsg := messages.TaskAssigned(
		"test-project",
		"test-bead",
		"test-agent",
		messages.TaskData{
			Title:       "Test Task",
			Description: "Test Description",
			Priority:    1,
			Type:        "task",
		},
		"test-correlation",
	)

	err := mockBus.PublishTask(context.Background(), "test-project", taskMsg)
	if err != nil {
		t.Fatalf("Failed to publish task: %v", err)
	}

	if mockBus.publishCount != 1 {
		t.Errorf("Expected 1 published task, got %d", mockBus.publishCount)
	}

	if len(mockBus.publishedTasks) != 1 {
		t.Fatalf("Expected 1 task in published list, got %d", len(mockBus.publishedTasks))
	}

	published := mockBus.publishedTasks[0]
	if published.BeadID != "test-bead" {
		t.Errorf("Expected bead ID 'test-bead', got %s", published.BeadID)
	}

	if published.Type != "task.assigned" {
		t.Errorf("Expected type 'task.assigned', got %s", published.Type)
	}

	t.Log("✅ Task message published successfully via MessageBus")
}

// TestCorrelationIDFlow verifies correlation IDs are preserved
func TestCorrelationIDFlow(t *testing.T) {
	correlationID := "test-correlation-12345"

	taskMsg := messages.TaskAssigned(
		"project",
		"bead",
		"agent",
		messages.TaskData{Title: "Test"},
		correlationID,
	)

	if taskMsg.CorrelationID != correlationID {
		t.Errorf("Expected correlation ID %s, got %s", correlationID, taskMsg.CorrelationID)
	}

	resultMsg := messages.TaskCompleted(
		"project",
		"bead",
		"agent",
		messages.ResultData{Status: "success"},
		correlationID,
	)

	if resultMsg.CorrelationID != correlationID {
		t.Errorf("Expected result correlation ID %s, got %s", correlationID, resultMsg.CorrelationID)
	}

	t.Logf("✅ Correlation ID preserved across task → result: %s", correlationID)
}

// TestMessageBusPublishPattern documents the publish pattern
func TestMessageBusPublishPattern(t *testing.T) {
	t.Log("MessageBus Publish Pattern:")
	t.Log("")
	t.Log("1. Dispatcher creates TaskMessage with:")
	t.Log("   - BeadID, ProjectID, AgentID")
	t.Log("   - TaskData (title, description, priority, type)")
	t.Log("   - CorrelationID (uuid for tracking)")
	t.Log("")
	t.Log("2. Dispatcher publishes to loom.tasks.{project_id}")
	t.Log("")
	t.Log("3. Agent subscribes to loom.tasks.{project_id}")
	t.Log("")
	t.Log("4. Agent executes task")
	t.Log("")
	t.Log("5. Agent publishes ResultMessage to loom.results.{project_id}")
	t.Log("")
	t.Log("6. Dispatcher subscribes to loom.results.*")
	t.Log("")
	t.Log("7. Dispatcher handleTaskResult() updates bead status")
	t.Log("")
	t.Log("✅ Full async communication flow documented")
}
