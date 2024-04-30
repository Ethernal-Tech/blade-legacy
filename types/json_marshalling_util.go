package types

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/valyala/fastjson"
)

var (
	DefaultArena fastjson.ArenaPool
	DefaultPool  fastjson.ParserPool
)

func UnmarshalJSONHash(v *fastjson.Value, key string) (Hash, error) {
	hash := Hash{}

	b := v.GetStringBytes(key)
	if len(b) == 0 {
		return ZeroHash, fmt.Errorf("field '%s' not found", key)
	}

	err := hash.UnmarshalText(b)

	return hash, err
}

func UnmarshalJSONAddr(v *fastjson.Value, key string) (Address, error) {
	b := v.GetStringBytes(key)
	if len(b) == 0 {
		return ZeroAddress, fmt.Errorf("field '%s' not found", key)
	}

	a := Address{}
	err := a.UnmarshalText(b)

	return a, err
}

func UnmarshalJSONBytes(v *fastjson.Value, key string, bits ...int) ([]byte, error) {
	vv := v.Get(key)
	if vv == nil {
		return nil, fmt.Errorf("field '%s' not found", key)
	}

	str := vv.String()
	str = strings.Trim(str, "\"")

	if !strings.HasPrefix(str, "0x") {
		return nil, fmt.Errorf("field '%s' does not have 0x prefix: '%s'", key, str)
	}

	str = str[2:]
	if len(str)%2 != 0 {
		str = "0" + str
	}

	buf, err := hex.DecodeString(str)
	if err != nil {
		return nil, err
	}

	if len(bits) > 0 && bits[0] != len(buf) {
		return nil, fmt.Errorf("field '%s' invalid length, expected %d but found %d: %s", key, bits[0], len(buf), str)
	}

	return buf, nil
}

func UnmarshalJSONUint64(v *fastjson.Value, key string) (uint64, error) {
	vv := v.Get(key)
	if vv == nil {
		return 0, fmt.Errorf("field '%s' not found", key)
	}

	str := vv.String()
	str = strings.Trim(str, "\"")

	return common.ParseUint64orHex(&str)
}

func UnmarshalJSONBigInt(v *fastjson.Value, key string) (*big.Int, error) {
	vv := v.Get(key)
	if vv == nil {
		return nil, fmt.Errorf("field '%s' not found", key)
	}

	str := vv.String()
	str = strings.Trim(str, "\"")

	return common.ParseUint256orHex(&str)
}

func UnmarshalJSONNonce(n *Nonce, v *fastjson.Value, key string) error {
	b := v.GetStringBytes(key)
	if len(b) == 0 {
		return fmt.Errorf("field '%s' not found", key)
	}

	return UnmarshalTextByte(n[:], b, 8)
}

func UnmarshalJSONBloom(bloom *Bloom, v *fastjson.Value, key string) error {
	b := v.GetStringBytes(key)
	if len(b) == 0 {
		return fmt.Errorf("field '%s' not found", key)
	}

	return UnmarshalTextByte(bloom[:], b, BloomByteLength)
}

func UnmarshalTextByte(dst, src []byte, size int) error {
	str := string(src)
	str = strings.Trim(str, "\"")

	b, err := hex.DecodeHex(str)
	if err != nil {
		return err
	}

	if len(b) != size {
		return fmt.Errorf("length %d is not correct, expected %d", len(b), size)
	}

	copy(dst, b)

	return nil
}

// HasJSONKey is a helper function for checking if given key exists in json
func HasJSONKey(v *fastjson.Value, key string) bool {
	value := v.Get(key)

	return value != nil && value.Type() != fastjson.TypeNull
}
