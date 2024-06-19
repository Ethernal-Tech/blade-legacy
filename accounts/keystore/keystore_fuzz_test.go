package keystore

import (
	"testing"

	"github.com/hashicorp/go-hclog"
)

func FuzzPassword(f *testing.F) {
	f.Fuzz(func(t *testing.T, password string) {
		ks, err := NewKeyStore(t.TempDir(), LightScryptN, LightScryptP, hclog.NewNullLogger())
		if err != nil {
			t.Fatal(err)
		}

		a, err := ks.NewAccount(password)
		if err != nil {
			t.Fatal(err)
		}

		if err := ks.Unlock(a, password); err != nil {
			t.Fatal(err)
		}
	})
}
