package client

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"google.golang.org/grpc"

	"github.com/jordanhubbard/loom/pkg/config"
)

// Client wraps the Temporal client with loom-specific functionality
type Client struct {
	temporal  client.Client
	config    *config.TemporalConfig
	namespace string
}

// New creates a new Temporal client instance with retry logic
func New(cfg *config.TemporalConfig) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("temporal config cannot be nil")
	}

	// Retry connection with exponential backoff
	maxRetries := 5
	baseDelay := 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<uint(attempt-1)) // Exponential backoff: 2s, 4s, 8s, 16s
			log.Printf("Retrying Temporal connection in %v (attempt %d/%d)...", delay, attempt+1, maxRetries)
			time.Sleep(delay)
		}

		// Create context with timeout for this attempt
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

		// Create Temporal client options
		clientOptions := client.Options{
			HostPort:  cfg.Host,
			Namespace: cfg.Namespace,
			Logger:    &temporalLogger{},
			ConnectionOptions: client.ConnectionOptions{
				TLS: nil,
				DialOptions: []grpc.DialOption{
					grpc.WithBlock(),
					grpc.FailOnNonTempDialError(false),
				},
			},
		}

		// Attempt connection
		c, err := client.DialContext(ctx, clientOptions)
		cancel()

		if err == nil {
			log.Printf("Connected to Temporal server at %s (namespace: %s)", cfg.Host, cfg.Namespace)
			return &Client{
				temporal:  c,
				config:    cfg,
				namespace: cfg.Namespace,
			}, nil
		}

		log.Printf("Temporal connection attempt %d failed: %v", attempt+1, err)
	}

	return nil, fmt.Errorf("failed to create temporal client after %d retries", maxRetries)
}

// Close closes the Temporal client connection
func (c *Client) Close() {
	if c.temporal != nil {
		c.temporal.Close()
	}
}

// GetClient returns the underlying Temporal client
func (c *Client) GetClient() client.Client {
	return c.temporal
}

// GetNamespace returns the configured namespace
func (c *Client) GetNamespace() string {
	return c.namespace
}

// GetTaskQueue returns the configured task queue
func (c *Client) GetTaskQueue() string {
	return c.config.TaskQueue
}

// GetConfig returns the temporal configuration
func (c *Client) GetConfig() *config.TemporalConfig {
	return c.config
}

// ExecuteWorkflow starts a new workflow execution
func (c *Client) ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error) {
	return c.temporal.ExecuteWorkflow(ctx, options, workflow, args...)
}

// SignalWorkflow sends a signal to a running workflow
func (c *Client) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	return c.temporal.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// QueryWorkflow sends a query to a running workflow
func (c *Client) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (converter.EncodedValue, error) {
	return c.temporal.QueryWorkflow(ctx, workflowID, runID, queryType, args...)
}

// CancelWorkflow requests cancellation of a workflow execution
func (c *Client) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	return c.temporal.CancelWorkflow(ctx, workflowID, runID)
}

// GetWorkflow returns a handle to an existing workflow
func (c *Client) GetWorkflow(ctx context.Context, workflowID, runID string) client.WorkflowRun {
	return c.temporal.GetWorkflow(ctx, workflowID, runID)
}

// temporalLogger implements Temporal's Logger interface
type temporalLogger struct{}

func (l *temporalLogger) Debug(msg string, keyvals ...interface{}) {
	log.Printf("[Temporal DEBUG] %s %v", msg, keyvals)
}

func (l *temporalLogger) Info(msg string, keyvals ...interface{}) {
	log.Printf("[Temporal INFO] %s %v", msg, keyvals)
}

func (l *temporalLogger) Warn(msg string, keyvals ...interface{}) {
	log.Printf("[Temporal WARN] %s %v", msg, keyvals)
}

func (l *temporalLogger) Error(msg string, keyvals ...interface{}) {
	log.Printf("[Temporal ERROR] %s %v", msg, keyvals)
}
