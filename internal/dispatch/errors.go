package dispatch

import "strings"

// isProviderError checks if the given error message indicates a provider-related error.
// These are transient infrastructure issues that will resolve on their own and should
// not trigger remediation bead creation.
func isProviderError(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	// Connection/network errors
	if strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "context canceled") ||
		strings.Contains(errMsg, "context deadline exceeded") ||
		strings.Contains(errMsg, "dial tcp") ||
		strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "i/o timeout") {
		return true
	}
	// HTTP status code errors (rate limits, auth, server errors)
	if strings.Contains(errMsg, "status code 401") ||
		strings.Contains(errMsg, "status code 403") ||
		strings.Contains(errMsg, "status code 429") ||
		strings.Contains(errMsg, "status code 500") ||
		strings.Contains(errMsg, "status code 502") ||
		strings.Contains(errMsg, "status code 503") ||
		strings.Contains(errMsg, "status code 504") {
		return true
	}
	// Provider-specific error patterns
	if strings.Contains(errMsg, "502 all providers failed") ||
		strings.Contains(errMsg, "429 budget exceeded") ||
		strings.Contains(errMsg, "authorization required") ||
		strings.Contains(errMsg, "rate limit") ||
		strings.Contains(errMsg, "quota exceeded") {
		return true
	}
	return false
}
