//nolint:ireturn // it's ok here
package floxy

import (
	"context"
)

type txKey struct{}

func TxFromContext(ctx context.Context) Tx {
	if tx, ok := ctx.Value(txKey{}).(Tx); ok {
		return tx
	}

	return nil
}
