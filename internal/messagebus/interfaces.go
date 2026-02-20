package messagebus

import (
	"context"

	"github.com/jordanhubbard/loom/pkg/messages"
)

// TaskPublisher abstracts task publishing for testability.
type TaskPublisher interface {
	PublishTask(ctx context.Context, projectID string, task *messages.TaskMessage) error
	PublishTaskForRole(ctx context.Context, projectID, role string, task *messages.TaskMessage) error
}

// ResultSubscriber abstracts result subscription for testability.
type ResultSubscriber interface {
	SubscribeResults(handler func(*messages.ResultMessage)) error
}

// PlanPublisher abstracts plan publishing for testability.
type PlanPublisher interface {
	PublishPlan(ctx context.Context, projectID string, plan *messages.PlanMessage) error
}

// ReviewPublisher abstracts review publishing for testability.
type ReviewPublisher interface {
	PublishReview(ctx context.Context, projectID string, review *messages.ReviewMessage) error
}

// EventPublisher abstracts event publishing for testability.
type EventPublisher interface {
	PublishEvent(ctx context.Context, eventType string, event *messages.EventMessage) error
}

// SwarmPublisher abstracts swarm message publishing for testability.
type SwarmPublisher interface {
	PublishSwarm(ctx context.Context, msg *messages.SwarmMessage) error
}

// SwarmSubscriber abstracts swarm message subscription for testability.
type SwarmSubscriber interface {
	SubscribeSwarm(handler func(*messages.SwarmMessage)) error
}

// Verify NatsMessageBus implements all interfaces at compile time.
var (
	_ TaskPublisher    = (*NatsMessageBus)(nil)
	_ ResultSubscriber = (*NatsMessageBus)(nil)
	_ PlanPublisher    = (*NatsMessageBus)(nil)
	_ ReviewPublisher  = (*NatsMessageBus)(nil)
	_ EventPublisher   = (*NatsMessageBus)(nil)
	_ SwarmPublisher   = (*NatsMessageBus)(nil)
	_ SwarmSubscriber  = (*NatsMessageBus)(nil)
)
