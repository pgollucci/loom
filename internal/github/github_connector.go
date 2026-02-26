// Package github provides integration with GitHub for Loom.
package github

import (
	"log"
	"net/http"
)

// StartWebhookServer starts an HTTP server to receive GitHub webhooks.
func StartWebhookServer() {
	http.HandleFunc("/webhook", webhookHandler)
	log.Println("Starting GitHub webhook server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// webhookHandler handles incoming GitHub webhooks.
func webhookHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement webhook handling logic
	log.Println("Received a webhook event")
	w.WriteHeader(http.StatusOK)
}
