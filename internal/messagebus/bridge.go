package messagebus

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/internal/temporal/eventbus"
	"github.com/jordanhubbard/loom/pkg/messages"
	"github.com/nats-io/nats.go"
)

// BridgedMessageBus bridges the in-process EventBus with external NATS messaging.
// Local events are forwarded to NATS so remote containers receive them; incoming
// NATS messages are injected into the EventBus so local subscribers receive them.
type BridgedMessageBus struct {
	nats     *NatsMessageBus
	eventBus *eventbus.EventBus

	containerID string
	mu          sync.RWMutex
	started     bool
	cancel      context.CancelFunc
}

// NewBridgedMessageBus creates a bridge between the local EventBus and NATS.
func NewBridgedMessageBus(natsBus *NatsMessageBus, eb *eventbus.EventBus, containerID string) *BridgedMessageBus {
	return &BridgedMessageBus{
		nats:        natsBus,
		eventBus:    eb,
		containerID: containerID,
	}
}

// Start begins bridging events in both directions.
func (b *BridgedMessageBus) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	ctx, b.cancel = context.WithCancel(ctx)
	b.mu.Unlock()

	if err := b.bridgeLocalToNATS(ctx); err != nil {
		return err
	}

	if err := b.bridgeNATSToLocal(); err != nil {
		return err
	}

	log.Printf("[Bridge] Started bidirectional bridge (container=%s)", b.containerID)
	return nil
}

// bridgeLocalToNATS subscribes to the local EventBus and forwards relevant events to NATS.
func (b *BridgedMessageBus) bridgeLocalToNATS(ctx context.Context) error {
	sub := b.eventBus.Subscribe("nats-bridge-out", func(event *eventbus.Event) bool {
		switch {
		case isAgentMessageEvent(event):
			return true
		case isSignificantEvent(event):
			return true
		default:
			return false
		}
	})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-sub.Channel:
				if !ok {
					return
				}
				// Skip events that came from NATS to avoid echo loops
				if event.Data != nil {
					if _, fromNats := event.Data["from_nats"]; fromNats {
						continue
					}
				}
				b.forwardEventToNATS(ctx, event)
			}
		}
	}()

	return nil
}

// forwardEventToNATS translates an EventBus event to a NATS message and publishes it.
func (b *BridgedMessageBus) forwardEventToNATS(ctx context.Context, event *eventbus.Event) {
	if isAgentMessageEvent(event) {
		b.forwardAgentMessageToNATS(ctx, event)
		return
	}

	eventMsg := &messages.EventMessage{
		Type:      string(event.Type),
		Source:    event.Source,
		ProjectID: event.ProjectID,
		Event: messages.EventData{
			Action:   "forwarded",
			Category: "bridge",
			Data:     event.Data,
		},
		Timestamp: event.Timestamp,
		Metadata: map[string]interface{}{
			"source_container": b.containerID,
		},
	}

	if err := b.nats.PublishEvent(ctx, string(event.Type), eventMsg); err != nil {
		log.Printf("[Bridge] Failed to forward event %s to NATS: %v", event.Type, err)
	}
}

func (b *BridgedMessageBus) forwardAgentMessageToNATS(ctx context.Context, event *eventbus.Event) {
	msgData, ok := event.Data["message"]
	if !ok {
		return
	}

	raw, err := json.Marshal(msgData)
	if err != nil {
		return
	}

	var agentMsg messages.AgentCommunicationMessage
	if err := json.Unmarshal(raw, &agentMsg); err != nil {
		return
	}

	agentMsg.SourceContainer = b.containerID
	if agentMsg.Timestamp.IsZero() {
		agentMsg.Timestamp = time.Now()
	}

	if err := b.nats.PublishAgentMessage(ctx, &agentMsg); err != nil {
		log.Printf("[Bridge] Failed to forward agent message to NATS: %v", err)
	}
}

// bridgeNATSToLocal subscribes to NATS agent messages and injects them into the local EventBus.
func (b *BridgedMessageBus) bridgeNATSToLocal() error {
	conn := b.nats.Conn()

	// Subscribe to all agent messages via core NATS (fan-out to all containers)
	agentSub, err := conn.Subscribe("loom.agent.messages.>", func(msg *nats.Msg) {
		var agentMsg messages.AgentCommunicationMessage
		if err := json.Unmarshal(msg.Data, &agentMsg); err != nil {
			log.Printf("[Bridge] Failed to unmarshal incoming NATS agent message: %v", err)
			return
		}

		if agentMsg.SourceContainer == b.containerID {
			return
		}

		b.injectAgentMessageLocally(&agentMsg)
	})
	if err != nil {
		return err
	}
	b.nats.subscriptions["bridge-agent-inbound"] = agentSub

	// Subscribe to events from other containers via core NATS
	eventSub, err := conn.Subscribe("loom.events.>", func(msg *nats.Msg) {
		var eventMsg messages.EventMessage
		if err := json.Unmarshal(msg.Data, &eventMsg); err != nil {
			return
		}

		if sc, ok := eventMsg.Metadata["source_container"]; ok {
			if sc == b.containerID {
				return
			}
		}

		b.injectEventLocally(&eventMsg)
	})
	if err != nil {
		return err
	}
	b.nats.subscriptions["bridge-event-inbound"] = eventSub

	return nil
}

func (b *BridgedMessageBus) injectAgentMessageLocally(msg *messages.AgentCommunicationMessage) {
	event := &eventbus.Event{
		Type:   eventbus.EventType("agent.message." + msg.Type),
		Source: "nats-bridge",
		Data: map[string]interface{}{
			"message":          msg,
			"from_nats":        true,
			"source_container": msg.SourceContainer,
		},
		Timestamp: msg.Timestamp,
	}

	if err := b.eventBus.Publish(event); err != nil {
		log.Printf("[Bridge] Failed to inject NATS agent message into local bus: %v", err)
	}
}

func (b *BridgedMessageBus) injectEventLocally(msg *messages.EventMessage) {
	data := msg.Event.Data
	if data == nil {
		data = make(map[string]interface{})
	}
	data["from_nats"] = true

	event := &eventbus.Event{
		Type:      eventbus.EventType(msg.Type),
		Source:    "nats-bridge:" + msg.Source,
		ProjectID: msg.ProjectID,
		Data:      data,
		Timestamp: msg.Timestamp,
	}

	if err := b.eventBus.Publish(event); err != nil {
		log.Printf("[Bridge] Failed to inject NATS event into local bus: %v", err)
	}
}

// NATS returns the underlying NATS message bus
func (b *BridgedMessageBus) NATS() *NatsMessageBus {
	return b.nats
}

// Close shuts down the bridge
func (b *BridgedMessageBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cancel != nil {
		b.cancel()
	}
	b.started = false
	if b.eventBus != nil {
		b.eventBus.Unsubscribe("nats-bridge-out")
	}
	log.Printf("[Bridge] Closed")
}

func isAgentMessageEvent(event *eventbus.Event) bool {
	t := string(event.Type)
	return len(t) > 14 && t[:14] == "agent.message."
}

func isSignificantEvent(event *eventbus.Event) bool {
	switch event.Type {
	case eventbus.EventTypeBeadCreated,
		eventbus.EventTypeBeadCompleted,
		eventbus.EventTypeBeadStatusChange,
		eventbus.EventTypeAgentSpawned,
		eventbus.EventTypeAgentCompleted,
		eventbus.EventTypeProviderRegistered,
		eventbus.EventTypeProviderDeleted,
		eventbus.EventTypeDecisionCreated,
		eventbus.EventTypeDecisionResolved,
		eventbus.EventTypeWorkflowStarted,
		eventbus.EventTypeWorkflowCompleted:
		return true
	default:
		return false
	}
}
