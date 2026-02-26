// Package provider implements provider management and health monitoring.
package provider

import (
	"context"
	"log"
	"time"
)

// HealthWatchdog periodically checks the health of registered providers.
type HealthWatchdog struct {
	registry *Registry
	interval time.Duration
	stopCh   chan struct{}
}

// NewHealthWatchdog creates a new HealthWatchdog.
func NewHealthWatchdog(registry *Registry, interval time.Duration) *HealthWatchdog {
	return &HealthWatchdog{
		registry: registry,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the health monitoring loop.
func (w *HealthWatchdog) Start() {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.checkProviders()
			case <-w.stopCh:
				return
			}
		}
	}()
}

// Stop halts the health monitoring loop.
func (w *HealthWatchdog) Stop() {
	close(w.stopCh)
}

// checkProviders checks the health of all registered providers.
func (w *HealthWatchdog) checkProviders() {
	providers := w.registry.List()
	for _, provider := range providers {
		if provider.Config == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := provider.Protocol.GetModels(ctx)
		if err != nil {
			log.Printf("[HealthWatchdog] Provider %s failed health check: %v", provider.Config.ID, err)
			w.registry.UpdateHeartbeatLatency(provider.Config.ID, -1)
		} else {
			log.Printf("[HealthWatchdog] Provider %s is healthy", provider.Config.ID)
			w.registry.UpdateHeartbeatLatency(provider.Config.ID, 0)
		}
	}
}
