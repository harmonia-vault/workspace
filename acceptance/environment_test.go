package acceptance

import (
	"context"
	"github.com/harmonia-vault/core-go/syncclient"
	"testing"
)

func environmentValue[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func submitSealedGrant(t *testing.T, ctx context.Context, tx *syncclient.GrantUpdateTransaction) syncclient.Acceptance {
	t.Helper()
	save, _ := recoveryNativeSeal(t, "independent-grant-journal")
	return environmentValue(tx.SubmitWithBarrier(ctx, func() error {
		raw, e := tx.ProtectedBytes()
		if e != nil {
			return e
		}
		defer clear(raw)
		return save(raw)
	}))
}
