package routes

import "testing"

// Setup panics on duplicate routes, so this catches conflicts before deploy.
func TestSetupDoesNotPanic(t *testing.T) {
	Setup()
}
