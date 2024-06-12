package keystore

import (
	"testing"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/hashicorp/go-hclog"
)

func FuzzPassword(f *testing.F) {
	f.Fuzz(func(t *testing.T, password string) {
		ks := NewKeyStore(t.TempDir(), LightScryptN, LightScryptP, hclog.NewNullLogger(), chain.AllForksEnabled.At(0))

		a, err := ks.NewAccount(password)
		if err != nil {
			t.Fatal(err)
		}

		if err := ks.Unlock(a, password); err != nil {
			t.Fatal(err)
		}
	})
}
