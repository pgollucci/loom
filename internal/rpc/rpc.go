// Package rpc provides the canonical NATS request/reply RPC mechanism for
// all internal Loom service-to-service communication.
//
// # Subject Convention
//
//	loom.rpc.{serviceType}.{instanceID}.{method}
//
// Callers that want any instance of a service type use a wildcard:
//
//	loom.rpc.agent-coder.*.task.assign
//
// # Usage (caller)
//
//	var resp MyResponse
//	err := rpc.Call(ctx, nc, "loom.rpc.agent-coder.*.task.assign", callerID, payload, &resp)
//
// # Usage (handler)
//
//	rpc.Register(nc, "loom.rpc.agent-coder.inst123.task.assign", func(req *rpc.Request) *rpc.Response {
//	    // process req.Payload
//	    return rpc.OK(responsePayload)
//	})
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

const defaultTimeout = 30 * time.Second

// Request is the canonical envelope for a NATS RPC call.
type Request struct {
	Method   string          `json:"method"`
	CallerID string          `json:"caller_id"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	TraceID  string          `json:"trace_id,omitempty"`
}

// Response is the canonical reply envelope for a NATS RPC call.
type Response struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// OK constructs a successful Response with the given payload marshalled to JSON.
func OK(payload interface{}) *Response {
	b, _ := json.Marshal(payload)
	return &Response{OK: true, Payload: b}
}

// Err constructs an error Response.
func Err(err error) *Response {
	return &Response{OK: false, Error: err.Error()}
}

// Call sends a NATS request to the given subject and decodes the response payload
// into out (which must be a pointer). Uses defaultTimeout if ctx has no deadline.
func Call(ctx context.Context, nc *nats.Conn, subject, callerID, method string, payload, out interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("rpc: marshal payload: %w", err)
	}

	req := &Request{
		Method:   method,
		CallerID: callerID,
		Payload:  payloadBytes,
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("rpc: marshal request: %w", err)
	}

	timeout := defaultTimeout
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < timeout {
			timeout = d
		}
	}

	msg, err := nc.Request(subject, reqBytes, timeout)
	if err != nil {
		return fmt.Errorf("rpc: request to %s: %w", subject, err)
	}

	var resp Response
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("rpc: unmarshal response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("rpc: remote error from %s: %s", subject, resp.Error)
	}
	if out != nil && len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, out); err != nil {
			return fmt.Errorf("rpc: unmarshal response payload: %w", err)
		}
	}
	return nil
}

// Register subscribes nc to subject and calls handler for each incoming request.
// The handler's returned *Response is serialised and sent as the NATS reply.
// subject should be the fully-qualified instance subject, e.g.:
//
//	loom.rpc.agent-coder.inst123.task.assign
func Register(nc *nats.Conn, subject string, handler func(*Request) *Response) (*nats.Subscription, error) {
	return nc.Subscribe(subject, func(msg *nats.Msg) {
		var req Request
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			resp := Err(fmt.Errorf("invalid request envelope: %w", err))
			b, _ := json.Marshal(resp)
			_ = msg.Respond(b)
			return
		}
		resp := handler(&req)
		if resp == nil {
			resp = OK(nil)
		}
		b, _ := json.Marshal(resp)
		_ = msg.Respond(b)
	})
}

// Subject returns the canonical RPC subject for a given service type, instance ID, and method.
func Subject(serviceType, instanceID, method string) string {
	return fmt.Sprintf("loom.rpc.%s.%s.%s", serviceType, instanceID, method)
}

// WildcardSubject returns a subject that matches any instance of the given service type and method.
func WildcardSubject(serviceType, method string) string {
	return fmt.Sprintf("loom.rpc.%s.*.%s", serviceType, method)
}
