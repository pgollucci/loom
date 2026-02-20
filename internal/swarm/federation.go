package swarm

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/jordanhubbard/loom/internal/messagebus"
	"github.com/jordanhubbard/loom/pkg/messages"
)

// FederationBus combines swarm publish/subscribe with a close method.
type FederationBus interface {
	SwarmBus
	io.Closer
}

// BusFactory creates a new message bus from a NATS URL. Abstracted for testability.
type BusFactory func(url string) (FederationBus, error)

// FederationConfig configures cross-Loom federation
type FederationConfig struct {
	PeerNATSURLs    []string
	GatewayName     string
	AllowedSubjects []string
}

// Federation manages cross-Loom instance communication via NATS gateways/leaf nodes.
type Federation struct {
	config     FederationConfig
	localBus   FederationBus
	peerBuses  map[string]FederationBus
	busFactory BusFactory
	mu         sync.RWMutex
	cancel     context.CancelFunc
}

func defaultBusFactory(url string) (FederationBus, error) {
	return messagebus.NewNatsMessageBus(messagebus.Config{
		URL:        url,
		StreamName: "LOOM-FEDERATED",
		Timeout:    10 * time.Second,
	})
}

// NewFederation creates a new federation manager
func NewFederation(localBus FederationBus, config FederationConfig) *Federation {
	if config.GatewayName == "" {
		config.GatewayName = "loom-default"
	}
	if len(config.AllowedSubjects) == 0 {
		config.AllowedSubjects = []string{
			"loom.swarm.>",
			"loom.plans.>",
			"loom.events.>",
		}
	}

	return &Federation{
		config:     config,
		localBus:   localBus,
		peerBuses:  make(map[string]FederationBus),
		busFactory: defaultBusFactory,
	}
}

// Start connects to all configured peer NATS instances and begins
// forwarding swarm messages between them.
func (f *Federation) Start(ctx context.Context) error {
	ctx, f.cancel = context.WithCancel(ctx)

	for _, peerURL := range f.config.PeerNATSURLs {
		if err := f.connectPeer(ctx, peerURL); err != nil {
			log.Printf("[Federation] Warning: Failed to connect to peer %s: %v", peerURL, err)
			// Non-fatal: continue with other peers
		}
	}

	log.Printf("[Federation] Started with %d peers (gateway=%s)", len(f.peerBuses), f.config.GatewayName)
	return nil
}

func (f *Federation) connectPeer(ctx context.Context, peerURL string) error {
	peerBus, err := f.busFactory(peerURL)
	if err != nil {
		return fmt.Errorf("failed to connect to peer NATS at %s: %w", peerURL, err)
	}

	f.mu.Lock()
	f.peerBuses[peerURL] = peerBus
	f.mu.Unlock()

	if err := peerBus.SubscribeSwarm(func(msg *messages.SwarmMessage) {
		if msg.Metadata == nil {
			msg.Metadata = make(map[string]interface{})
		}
		msg.Metadata["federation_source"] = peerURL
		msg.Metadata["federation_gateway"] = f.config.GatewayName

		if err := f.localBus.PublishSwarm(ctx, msg); err != nil {
			log.Printf("[Federation] Failed to forward peer swarm message: %v", err)
		}
	}); err != nil {
		log.Printf("[Federation] Warning: Failed to subscribe to peer %s swarm: %v", peerURL, err)
	}

	if err := f.localBus.SubscribeSwarm(func(msg *messages.SwarmMessage) {
		if msg.Metadata != nil {
			if _, isFederated := msg.Metadata["federation_source"]; isFederated {
				return
			}
		}

		if msg.Metadata == nil {
			msg.Metadata = make(map[string]interface{})
		}
		msg.Metadata["federation_source"] = f.config.GatewayName

		if err := peerBus.PublishSwarm(ctx, msg); err != nil {
			log.Printf("[Federation] Failed to forward local swarm message to peer %s: %v", peerURL, err)
		}
	}); err != nil {
		log.Printf("[Federation] Warning: Failed to subscribe to local swarm for peer forwarding: %v", err)
	}

	log.Printf("[Federation] Connected to peer at %s", peerURL)
	return nil
}

// PeerCount returns the number of connected federation peers.
func (f *Federation) PeerCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.peerBuses)
}

// Close disconnects from all peers.
func (f *Federation) Close() {
	if f.cancel != nil {
		f.cancel()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for url, bus := range f.peerBuses {
		if err := bus.Close(); err != nil {
			log.Printf("[Federation] Warning: Failed to close peer connection %s: %v", url, err)
		}
	}
	f.peerBuses = make(map[string]FederationBus)
	log.Printf("[Federation] Stopped")
}
