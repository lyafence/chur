package keeper

import "context"

// Backend fetches secret bytes from a keeper backend.
type Backend interface {
	Name() string
	GetSecret(ctx context.Context, ref string) ([]byte, error)
}
