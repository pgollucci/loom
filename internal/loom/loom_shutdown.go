package loom

import (
	"log"
	"time"

	"github.com/jordanhubbard/loom/internal/messagebus"
)

func (a *Loom) Shutdown() {
	a.shutdownOnce.Do(func() {
		if a.shutdownCancel != nil {
			a.shutdownCancel()
		}

		if a.agentManager != nil {
			a.agentManager.StopAll()
		}

		if a.connectorManager != nil {
			if err := a.connectorManager.Close(); err != nil {
				log.Printf("[Loom] Warning: failed to close connector manager: %v", err)
			}
		}
		if a.pdaOrchestrator != nil {
			a.pdaOrchestrator.Close()
		}
		if a.swarmFederation != nil {
			a.swarmFederation.Close()
		}
		if a.swarmManager != nil {
			a.swarmManager.Close()
		}
		if a.bridge != nil {
			a.bridge.Close()
		}
		if a.openclawBridge != nil {
			a.openclawBridge.Close()
		}
		if a.doltCoordinator != nil {
			a.doltCoordinator.Shutdown()
		}
		if a.eventBus != nil {
			a.eventBus.Close()
		}
		if a.messageBus != nil {
			if mb, ok := a.messageBus.(*messagebus.NatsMessageBus); ok {
				if err := mb.Close(); err != nil {
					log.Printf("[Loom] Warning: failed to close message bus: %v", err)
				}
			}
		}
		if a.database != nil {
			if err := a.database.Close(); err != nil {
				log.Printf("[Loom] Warning: failed to close database: %v", err)
			}
		}

		done := make(chan struct{})
		go func() {
			a.goroutineWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Printf("[Loom] All goroutines completed gracefully")
		case <-time.After(5 * time.Second):
			log.Printf("[Loom] Warning: shutdown timeout waiting for goroutines (5s)")
		}
	})
}
