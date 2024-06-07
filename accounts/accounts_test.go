package accounts

import (
	"bytes"
	"testing"

	"github.com/0xPolygon/polygon-edge/types"
)

func TestTextHash(t *testing.T) {
	t.Parallel()
	hash := TextHash([]byte("Hello Joe"))
	want := types.StringToBytes("0xa080337ae51c4e064c189e113edd0ba391df9206e2f49db658bb32cf2911730b")
	if !bytes.Equal(hash, want) {
		t.Fatalf("wrong hash: %x", hash)
	}
}
