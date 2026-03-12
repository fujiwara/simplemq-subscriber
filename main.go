package subscriber

import (
	"context"
)

// Run is the main entry point for the subscriber.
func Run(ctx context.Context) error {
	return RunCLI(ctx)
}
