package recap

import (
	"context"
	"errors"
)

// ErrNilContext reports a missing cancellation owner at a recap boundary.
var ErrNilContext = errors.New("recap: nil context")

func requireContext(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return nil
}
