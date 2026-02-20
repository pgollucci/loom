package swarm

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jordanhubbard/loom/pkg/messages"
)

type mockFederationBus struct {
	mu           sync.Mutex
	published    []*messages.SwarmMessage
	handlers     []func(*messages.SwarmMessage)
	publishErr   error
	subscribeErr error
	closed       bool
}

func (m *mockFederationBus) PublishSwarm(_ context.Context, msg *messages.SwarmMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.published = append(m.published, msg)
	return nil
}

func (m *mockFederationBus) SubscribeSwarm(handler func(*messages.SwarmMessage)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subscribeErr != nil {
		return m.subscribeErr
	}
	m.handlers = append(m.handlers, handler)
	return nil
}

func (m *mockFederationBus) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func TestNewFederation_DefaultGatewayName(t *testing.T) {
	bus := &mockFederationBus{}
	f := NewFederation(bus, FederationConfig{})
	if f.config.GatewayName != "loom-default" {
		t.Errorf("expected default gateway name, got %q", f.config.GatewayName)
	}
}

func TestNewFederation_CustomGatewayName(t *testing.T) {
	bus := &mockFederationBus{}
	f := NewFederation(bus, FederationConfig{GatewayName: "custom"})
	if f.config.GatewayName != "custom" {
		t.Errorf("expected custom, got %q", f.config.GatewayName)
	}
}

func TestNewFederation_DefaultAllowedSubjects(t *testing.T) {
	f := NewFederation(&mockFederationBus{}, FederationConfig{})
	if len(f.config.AllowedSubjects) != 3 {
		t.Errorf("expected 3 default allowed subjects, got %d", len(f.config.AllowedSubjects))
	}
	expected := map[string]bool{
		"loom.swarm.>":  true,
		"loom.plans.>":  true,
		"loom.events.>": true,
	}
	for _, subj := range f.config.AllowedSubjects {
		if !expected[subj] {
			t.Errorf("unexpected allowed subject: %q", subj)
		}
	}
}

func TestNewFederation_CustomAllowedSubjects(t *testing.T) {
	f := NewFederation(&mockFederationBus{}, FederationConfig{
		AllowedSubjects: []string{"custom.>"},
	})
	if len(f.config.AllowedSubjects) != 1 {
		t.Errorf("expected 1 custom subject, got %d", len(f.config.AllowedSubjects))
	}
}

func TestFederation_PeerCountEmpty(t *testing.T) {
	f := NewFederation(&mockFederationBus{}, FederationConfig{})
	if f.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", f.PeerCount())
	}
}

func TestFederation_CloseNilCancel(t *testing.T) {
	f := NewFederation(&mockFederationBus{}, FederationConfig{})
	f.Close()
}

func TestFederation_CloseWithCancel(t *testing.T) {
	called := false
	f := NewFederation(&mockFederationBus{}, FederationConfig{})
	f.cancel = func() { called = true }
	f.Close()
	if !called {
		t.Error("cancel should be called")
	}
}

func TestFederation_CloseWithPeers(t *testing.T) {
	peer := &mockFederationBus{}
	f := NewFederation(&mockFederationBus{}, FederationConfig{})
	f.peerBuses["nats://peer1:4222"] = peer
	f.Close()

	peer.mu.Lock()
	if !peer.closed {
		t.Error("peer bus should be closed")
	}
	peer.mu.Unlock()

	if f.PeerCount() != 0 {
		t.Errorf("expected 0 peers after close, got %d", f.PeerCount())
	}
}

func TestFederationConfig_PeerNATSURLs(t *testing.T) {
	cfg := FederationConfig{
		PeerNATSURLs: []string{"nats://peer1:4222", "nats://peer2:4222"},
		GatewayName:  "loom-1",
	}
	if len(cfg.PeerNATSURLs) != 2 {
		t.Errorf("expected 2 peer URLs, got %d", len(cfg.PeerNATSURLs))
	}
}

func TestFederation_Start_NoPeers(t *testing.T) {
	localBus := &mockFederationBus{}
	f := NewFederation(localBus, FederationConfig{})

	err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start with no peers should succeed: %v", err)
	}
	defer f.Close()

	if f.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", f.PeerCount())
	}
}

func TestFederation_Start_WithPeers(t *testing.T) {
	localBus := &mockFederationBus{}
	peerBus := &mockFederationBus{}

	f := NewFederation(localBus, FederationConfig{
		PeerNATSURLs: []string{"nats://peer1:4222"},
	})
	f.busFactory = func(url string) (FederationBus, error) {
		return peerBus, nil
	}

	err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer f.Close()

	if f.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", f.PeerCount())
	}

	// Both local and peer buses should have swarm subscriptions
	localBus.mu.Lock()
	localHandlerCount := len(localBus.handlers)
	localBus.mu.Unlock()
	if localHandlerCount == 0 {
		t.Error("expected local bus to have swarm handler for forwarding")
	}

	peerBus.mu.Lock()
	peerHandlerCount := len(peerBus.handlers)
	peerBus.mu.Unlock()
	if peerHandlerCount == 0 {
		t.Error("expected peer bus to have swarm handler for forwarding")
	}
}

func TestFederation_Start_PeerConnectionFails(t *testing.T) {
	localBus := &mockFederationBus{}
	f := NewFederation(localBus, FederationConfig{
		PeerNATSURLs: []string{"nats://bad-peer:4222"},
	})
	f.busFactory = func(url string) (FederationBus, error) {
		return nil, fmt.Errorf("connection refused")
	}

	err := f.Start(context.Background())
	if err != nil {
		t.Fatalf("Start should succeed even with failed peers: %v", err)
	}
	defer f.Close()

	if f.PeerCount() != 0 {
		t.Errorf("expected 0 peers (connection failed), got %d", f.PeerCount())
	}
}

func TestFederation_ConnectPeer_ForwardsPeerToLocal(t *testing.T) {
	localBus := &mockFederationBus{}
	peerBus := &mockFederationBus{}

	f := NewFederation(localBus, FederationConfig{GatewayName: "loom-1"})
	f.busFactory = func(url string) (FederationBus, error) {
		return peerBus, nil
	}

	err := f.connectPeer(context.Background(), "nats://peer1:4222")
	if err != nil {
		t.Fatalf("connectPeer failed: %v", err)
	}

	// Simulate a message arriving from the peer
	peerBus.mu.Lock()
	handler := peerBus.handlers[0]
	peerBus.mu.Unlock()

	msg := &messages.SwarmMessage{
		Type:      "swarm.announce",
		ServiceID: "remote-svc",
	}
	handler(msg)

	// Should have been forwarded to local bus with federation metadata
	localBus.mu.Lock()
	if len(localBus.published) != 1 {
		t.Fatalf("expected 1 message forwarded to local, got %d", len(localBus.published))
	}
	forwarded := localBus.published[0]
	localBus.mu.Unlock()

	if forwarded.Metadata["federation_source"] != "nats://peer1:4222" {
		t.Errorf("expected federation_source tag, got %v", forwarded.Metadata)
	}
	if forwarded.Metadata["federation_gateway"] != "loom-1" {
		t.Errorf("expected federation_gateway tag, got %v", forwarded.Metadata)
	}
}

func TestFederation_ConnectPeer_ForwardsLocalToPeer(t *testing.T) {
	localBus := &mockFederationBus{}
	peerBus := &mockFederationBus{}

	f := NewFederation(localBus, FederationConfig{GatewayName: "loom-1"})
	f.busFactory = func(url string) (FederationBus, error) {
		return peerBus, nil
	}

	err := f.connectPeer(context.Background(), "nats://peer1:4222")
	if err != nil {
		t.Fatalf("connectPeer failed: %v", err)
	}

	// Simulate a local swarm message (should be forwarded to peer)
	localBus.mu.Lock()
	handler := localBus.handlers[0]
	localBus.mu.Unlock()

	msg := &messages.SwarmMessage{
		Type:      "swarm.announce",
		ServiceID: "local-svc",
	}
	handler(msg)

	peerBus.mu.Lock()
	if len(peerBus.published) != 1 {
		t.Fatalf("expected 1 message forwarded to peer, got %d", len(peerBus.published))
	}
	forwarded := peerBus.published[0]
	peerBus.mu.Unlock()

	if forwarded.Metadata["federation_source"] != "loom-1" {
		t.Errorf("expected federation_source tag, got %v", forwarded.Metadata)
	}
}

func TestFederation_ConnectPeer_NoEchoFederatedMessages(t *testing.T) {
	localBus := &mockFederationBus{}
	peerBus := &mockFederationBus{}

	f := NewFederation(localBus, FederationConfig{GatewayName: "loom-1"})
	f.busFactory = func(url string) (FederationBus, error) {
		return peerBus, nil
	}

	err := f.connectPeer(context.Background(), "nats://peer1:4222")
	if err != nil {
		t.Fatalf("connectPeer failed: %v", err)
	}

	localBus.mu.Lock()
	handler := localBus.handlers[0]
	localBus.mu.Unlock()

	// Send a message that already has federation_source (should not be re-forwarded)
	msg := &messages.SwarmMessage{
		Type:      "swarm.announce",
		ServiceID: "remote-svc",
		Metadata:  map[string]interface{}{"federation_source": "some-other-loom"},
	}
	handler(msg)

	peerBus.mu.Lock()
	forwarded := len(peerBus.published)
	peerBus.mu.Unlock()

	if forwarded != 0 {
		t.Errorf("federated message should not be re-forwarded, got %d messages", forwarded)
	}
}

func TestFederation_ConnectPeer_FactoryError(t *testing.T) {
	f := NewFederation(&mockFederationBus{}, FederationConfig{})
	f.busFactory = func(url string) (FederationBus, error) {
		return nil, fmt.Errorf("connection refused")
	}

	err := f.connectPeer(context.Background(), "nats://bad:4222")
	if err == nil {
		t.Error("expected error from bus factory")
	}
}
