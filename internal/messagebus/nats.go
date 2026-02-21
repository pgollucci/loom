package messagebus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jordanhubbard/loom/pkg/messages"
	"github.com/nats-io/nats.go"
)

func stripPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

// NatsMessageBus implements a message bus using NATS with JetStream
type NatsMessageBus struct {
	conn           *nats.Conn
	js             nats.JetStreamContext
	subscriptions  map[string]*nats.Subscription
	streamName     string
	url            string
	consumerPrefix string
}

// Config holds NATS configuration
type Config struct {
	URL            string        // NATS server URL (e.g., "nats://nats:4222")
	StreamName     string        // JetStream stream name (default: "LOOM")
	Timeout        time.Duration // Connection timeout
	ConsumerPrefix string        // Prefix for durable consumer names (for test isolation)
}

// NewNatsMessageBus creates a new NATS message bus with JetStream
func NewNatsMessageBus(cfg Config) (*NatsMessageBus, error) {
	if cfg.URL == "" {
		cfg.URL = "nats://localhost:4222"
	}
	if cfg.StreamName == "" {
		cfg.StreamName = "LOOM"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	// Connect to NATS
	nc, err := nats.Connect(cfg.URL,
		nats.Timeout(cfg.Timeout),
		nats.ReconnectWait(1*time.Second),
		nats.MaxReconnects(-1), // Unlimited reconnects
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				log.Printf("NATS disconnected: %v", err)
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS reconnected to %s", nc.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Create JetStream context
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	mb := &NatsMessageBus{
		conn:           nc,
		js:             js,
		subscriptions:  make(map[string]*nats.Subscription),
		streamName:     cfg.StreamName,
		url:            cfg.URL,
		consumerPrefix: cfg.ConsumerPrefix,
	}

	// Create or update the LOOM stream
	if err := mb.ensureStream(); err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to ensure stream: %w", err)
	}

	log.Printf("Connected to NATS at %s with JetStream stream %s", cfg.URL, cfg.StreamName)
	return mb, nil
}

// ensureStream creates or updates the JetStream stream.
// Uses LimitsPolicy (not WorkQueue) so that multiple consumers can
// subscribe to the same subjects—required for results/events fan-out.
func (mb *NatsMessageBus) ensureStream() error {
	streamConfig := &nats.StreamConfig{
		Name:      mb.streamName,
		Subjects:  []string{"loom.>"},
		Retention: nats.LimitsPolicy,
		MaxAge:    24 * time.Hour,
		MaxBytes:  1024 * 1024 * 1024, // 1GB
		Storage:   nats.FileStorage,
		Replicas:  1,
		Discard:   nats.DiscardOld,
	}

	info, err := mb.js.StreamInfo(mb.streamName)
	if err != nil {
		_, err = mb.js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create stream: %w", err)
		}
		log.Printf("Created JetStream stream: %s", mb.streamName)
	} else if info.Config.Retention != nats.LimitsPolicy {
		// Retention policy can't be changed on an existing stream—
		// delete and recreate if the old stream used WorkQueue.
		if err := mb.js.DeleteStream(mb.streamName); err != nil {
			return fmt.Errorf("failed to delete legacy stream: %w", err)
		}
		_, err = mb.js.AddStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to recreate stream: %w", err)
		}
		log.Printf("Recreated JetStream stream %s (migrated from WorkQueue to Limits)", mb.streamName)
	} else {
		_, err = mb.js.UpdateStream(streamConfig)
		if err != nil {
			return fmt.Errorf("failed to update stream: %w", err)
		}
		log.Printf("Updated JetStream stream: %s", mb.streamName)
	}

	return nil
}

// PublishTask publishes a task message to the message bus.
// If role is non-empty, publishes to loom.tasks.{projectID}.{role} for role-targeted delivery.
func (mb *NatsMessageBus) PublishTask(ctx context.Context, projectID string, task *messages.TaskMessage) error {
	subject := fmt.Sprintf("loom.tasks.%s", projectID)
	return mb.publish(subject, task)
}

// PublishTaskForRole publishes a task to a role-specific subject
func (mb *NatsMessageBus) PublishTaskForRole(ctx context.Context, projectID, role string, task *messages.TaskMessage) error {
	subject := fmt.Sprintf("loom.tasks.%s.%s", projectID, role)
	return mb.publish(subject, task)
}

// PublishResult publishes a result message to the message bus
func (mb *NatsMessageBus) PublishResult(ctx context.Context, projectID string, result *messages.ResultMessage) error {
	subject := fmt.Sprintf("loom.results.%s", projectID)
	return mb.publish(subject, result)
}

// PublishEvent publishes an event message to the message bus
func (mb *NatsMessageBus) PublishEvent(ctx context.Context, eventType string, event *messages.EventMessage) error {
	subject := fmt.Sprintf("loom.events.%s", eventType)
	return mb.publish(subject, event)
}

// PublishAgentMessage publishes an agent-to-agent communication message
func (mb *NatsMessageBus) PublishAgentMessage(ctx context.Context, msg *messages.AgentCommunicationMessage) error {
	var subject string
	if msg.ToAgentID != "" {
		subject = fmt.Sprintf("loom.agent.messages.%s", msg.ToAgentID)
	} else {
		subject = "loom.agent.messages.broadcast"
	}
	return mb.publish(subject, msg)
}

// PublishPlan publishes a plan message
func (mb *NatsMessageBus) PublishPlan(ctx context.Context, projectID string, plan *messages.PlanMessage) error {
	subject := fmt.Sprintf("loom.plans.%s", projectID)
	return mb.publish(subject, plan)
}

// PublishReview publishes a review message
func (mb *NatsMessageBus) PublishReview(ctx context.Context, projectID string, review *messages.ReviewMessage) error {
	subject := fmt.Sprintf("loom.reviews.%s", projectID)
	return mb.publish(subject, review)
}

// PublishSwarm publishes a swarm protocol message
func (mb *NatsMessageBus) PublishSwarm(ctx context.Context, msg *messages.SwarmMessage) error {
	subject := fmt.Sprintf("loom.swarm.%s", stripPrefix(msg.Type, "swarm."))
	return mb.publish(subject, msg)
}

// publish is the internal method to publish messages
func (mb *NatsMessageBus) publish(subject string, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Publish to JetStream for durability
	_, err = mb.js.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish message to %s: %w", subject, err)
	}

	return nil
}

// SubscribeTasks subscribes to task messages for a specific project
func (mb *NatsMessageBus) SubscribeTasks(projectID string, handler func(*messages.TaskMessage)) error {
	subject := fmt.Sprintf("loom.tasks.%s", projectID)
	consumerName := fmt.Sprintf("tasks-%s", projectID)

	return mb.subscribe(subject, consumerName, func(msg *nats.Msg) {
		var task messages.TaskMessage
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			log.Printf("Failed to unmarshal task message: %v", err)
			msg.Nak() // Negative acknowledgment
			return
		}

		handler(&task)
		msg.Ack() // Acknowledge successful processing
	})
}

// SubscribeResults subscribes to result messages for all projects
func (mb *NatsMessageBus) SubscribeResults(handler func(*messages.ResultMessage)) error {
	subject := "loom.results.*"
	consumerName := "results-all"

	return mb.subscribe(subject, consumerName, func(msg *nats.Msg) {
		var result messages.ResultMessage
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			log.Printf("Failed to unmarshal result message: %v", err)
			msg.Nak()
			return
		}

		handler(&result)
		msg.Ack()
	})
}

// SubscribeEvents subscribes to event messages
func (mb *NatsMessageBus) SubscribeEvents(eventType string, handler func(*messages.EventMessage)) error {
	subject := fmt.Sprintf("loom.events.%s", eventType)
	consumerName := fmt.Sprintf("events-%s", eventType)

	return mb.subscribe(subject, consumerName, func(msg *nats.Msg) {
		var event messages.EventMessage
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("Failed to unmarshal event message: %v", err)
			msg.Nak()
			return
		}

		handler(&event)
		msg.Ack()
	})
}

// SubscribeTasksForRole subscribes to role-targeted task messages
func (mb *NatsMessageBus) SubscribeTasksForRole(projectID, role string, handler func(*messages.TaskMessage)) error {
	subject := fmt.Sprintf("loom.tasks.%s.%s", projectID, role)
	consumerName := fmt.Sprintf("tasks-%s-%s", projectID, role)

	return mb.subscribe(subject, consumerName, func(msg *nats.Msg) {
		var task messages.TaskMessage
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			log.Printf("Failed to unmarshal task message: %v", err)
			msg.Nak()
			return
		}

		handler(&task)
		msg.Ack()
	})
}

// SubscribeAgentMessages subscribes to agent-to-agent messages.
// If agentID is non-empty, subscribes to messages addressed to that agent plus broadcasts.
func (mb *NatsMessageBus) SubscribeAgentMessages(agentID string, handler func(*messages.AgentCommunicationMessage)) error {
	// Subscribe to direct messages for this agent
	if agentID != "" {
		directSubject := fmt.Sprintf("loom.agent.messages.%s", agentID)
		directConsumer := fmt.Sprintf("agent-msg-%s", agentID)
		if err := mb.subscribe(directSubject, directConsumer, func(msg *nats.Msg) {
			var agentMsg messages.AgentCommunicationMessage
			if err := json.Unmarshal(msg.Data, &agentMsg); err != nil {
				log.Printf("Failed to unmarshal agent message: %v", err)
				msg.Nak()
				return
			}
			handler(&agentMsg)
			msg.Ack()
		}); err != nil {
			return err
		}
	}

	// Also subscribe to broadcast messages using a core NATS subscription
	// (broadcasts should be delivered to all agents, not work-queue style)
	broadcastSub, err := mb.conn.Subscribe("loom.agent.messages.broadcast", func(msg *nats.Msg) {
		var agentMsg messages.AgentCommunicationMessage
		if err := json.Unmarshal(msg.Data, &agentMsg); err != nil {
			log.Printf("Failed to unmarshal broadcast agent message: %v", err)
			return
		}
		handler(&agentMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to agent broadcast: %w", err)
	}
	mb.subscriptions["loom.agent.messages.broadcast."+agentID] = broadcastSub
	return nil
}

// SubscribePlans subscribes to plan messages for a project
func (mb *NatsMessageBus) SubscribePlans(projectID string, handler func(*messages.PlanMessage)) error {
	subject := fmt.Sprintf("loom.plans.%s", projectID)
	consumerName := fmt.Sprintf("plans-%s", projectID)

	return mb.subscribe(subject, consumerName, func(msg *nats.Msg) {
		var plan messages.PlanMessage
		if err := json.Unmarshal(msg.Data, &plan); err != nil {
			log.Printf("Failed to unmarshal plan message: %v", err)
			msg.Nak()
			return
		}
		handler(&plan)
		msg.Ack()
	})
}

// SubscribeReviews subscribes to review messages for a project
func (mb *NatsMessageBus) SubscribeReviews(projectID string, handler func(*messages.ReviewMessage)) error {
	subject := fmt.Sprintf("loom.reviews.%s", projectID)
	consumerName := fmt.Sprintf("reviews-%s", projectID)

	return mb.subscribe(subject, consumerName, func(msg *nats.Msg) {
		var review messages.ReviewMessage
		if err := json.Unmarshal(msg.Data, &review); err != nil {
			log.Printf("Failed to unmarshal review message: %v", err)
			msg.Nak()
			return
		}
		handler(&review)
		msg.Ack()
	})
}

// SubscribeSwarm subscribes to swarm protocol messages (uses core NATS for fan-out)
func (mb *NatsMessageBus) SubscribeSwarm(handler func(*messages.SwarmMessage)) error {
	sub, err := mb.conn.Subscribe("loom.swarm.>", func(msg *nats.Msg) {
		var swarmMsg messages.SwarmMessage
		if err := json.Unmarshal(msg.Data, &swarmMsg); err != nil {
			log.Printf("Failed to unmarshal swarm message: %v", err)
			return
		}
		handler(&swarmMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to swarm: %w", err)
	}
	mb.subscriptions["loom.swarm.>"] = sub
	log.Printf("Subscribed to swarm messages (loom.swarm.>)")
	return nil
}

// Conn returns the underlying NATS connection for advanced use
func (mb *NatsMessageBus) Conn() *nats.Conn {
	return mb.conn
}

// prefixConsumer adds the optional consumer prefix for namespace isolation
func (mb *NatsMessageBus) prefixConsumer(name string) string {
	if mb.consumerPrefix != "" {
		return mb.consumerPrefix + "-" + name
	}
	return name
}

// subscribe is the internal method to set up durable subscriptions
func (mb *NatsMessageBus) subscribe(subject, consumerName string, handler nats.MsgHandler) error {
	prefixed := mb.prefixConsumer(consumerName)
	sub, err := mb.js.Subscribe(subject, handler,
		nats.Durable(prefixed),
		nats.AckExplicit(),
		nats.MaxDeliver(3),
		nats.AckWait(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subject, err)
	}

	mb.subscriptions[subject] = sub
	log.Printf("Subscribed to %s with consumer %s", subject, prefixed)
	return nil
}

// Unsubscribe removes a subscription
func (mb *NatsMessageBus) Unsubscribe(subject string) error {
	sub, ok := mb.subscriptions[subject]
	if !ok {
		return fmt.Errorf("no subscription found for %s", subject)
	}

	if err := sub.Unsubscribe(); err != nil {
		return fmt.Errorf("failed to unsubscribe from %s: %w", subject, err)
	}

	delete(mb.subscriptions, subject)
	log.Printf("Unsubscribed from %s", subject)
	return nil
}

// Close closes all subscriptions and the NATS connection
func (mb *NatsMessageBus) Close() error {
	// Unsubscribe from all
	for subject := range mb.subscriptions {
		_ = mb.Unsubscribe(subject)
	}

	// Close connection
	mb.conn.Close()
	log.Printf("Closed NATS connection")
	return nil
}

// Health returns the health status of the NATS connection
func (mb *NatsMessageBus) Health() error {
	if mb.conn.IsClosed() {
		return fmt.Errorf("NATS connection is closed")
	}

	if !mb.conn.IsConnected() {
		return fmt.Errorf("NATS is not connected")
	}

	// Check stream health
	_, err := mb.js.StreamInfo(mb.streamName)
	if err != nil {
		return fmt.Errorf("JetStream stream %s is unhealthy: %w", mb.streamName, err)
	}

	return nil
}

// Stats returns statistics about the message bus
func (mb *NatsMessageBus) Stats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["url"] = mb.url
	stats["stream"] = mb.streamName
	stats["connected"] = mb.conn.IsConnected()
	stats["subscriptions"] = len(mb.subscriptions)

	// Get stream info
	streamInfo, err := mb.js.StreamInfo(mb.streamName)
	if err == nil {
		stats["stream_messages"] = streamInfo.State.Msgs
		stats["stream_bytes"] = streamInfo.State.Bytes
		stats["stream_consumers"] = streamInfo.State.Consumers
	}

	return stats
}
