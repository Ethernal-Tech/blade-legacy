package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// DefaultRootDerivationPath is the root path to which custom derivation endpoints
// are appended. As such, the first account will be at m/44'/60'/0'/0, the second
// at m/44'/60'/0'/1, etc.
var DefaultRootDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0}

// DefaultBaseDerivationPath is the base path from which custom derivation endpoints
// are incremented. As such, the first account will be at m/44'/60'/0'/0/0, the second
// at m/44'/60'/0'/0/1, etc.
var DefaultBaseDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0, 0}

// LegacyLedgerBaseDerivationPath is the legacy base path from which custom derivation
// endpoints are incremented. As such, the first account will be at m/44'/60'/0'/0, the
// second at m/44'/60'/0'/1, etc.
var LegacyLedgerBaseDerivationPath = DerivationPath{0x80000000 + 44, 0x80000000 + 60, 0x80000000 + 0, 0}

// DerivationPath represents the computer friendly version of a hierarchical
// deterministic wallet account derivation path.
//
// The BIP-32 spec https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki
// defines derivation paths to be of the form:
//
//	m / purpose' / coin_type' / account' / change / address_index
//
// The BIP-44 spec https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki
// defines that the `purpose` be 44' (or 0x8000002C) for crypto currencies, and
// SLIP-44 https://github.com/satoshilabs/slips/blob/master/slip-0044.md assigns
// the `coin_type` 60' (or 0x8000003C) to Ethereum.
//
// The root path for Ethereum is m/44'/60'/0'/0 according to the specification
// from https://github.com/ethereum/EIPs/issues/84, albeit it's not set in stone
// yet whether accounts should increment the last component or the children of
// that. We will go with the simpler approach of incrementing the last component.
type DerivationPath []uint32

func ParseDerivationPath(path string) (DerivationPath, error) {
	var result DerivationPath //nolint:prealloc

	components := strings.Split(path, "/")

	switch {
	case len(components) == 0:
		return nil, errors.New("empty derivation path")
	case strings.TrimSpace(components[0]) == "":
		return nil, errors.New("use 'm/' prefix for abolute path or no leading '/' for relative paths")
	case strings.TrimSpace(components[0]) == "m":
		components = components[1:]
	default:
		result = append(result, DefaultRootDerivationPath...)
	}

	if len(components) == 0 {
		return nil, errors.New("empty derivation path")
	}

	for _, component := range components {
		component = strings.TrimSpace(component)

		var value uint32

		if strings.HasSuffix(component, "'") {
			value = 0x80000000
			component = strings.TrimSpace(strings.TrimSuffix(component, "'"))
		}

		bigValue, ok := new(big.Int).SetString(component, 0)
		if !ok {
			return nil, fmt.Errorf("invalid component: %s", component)
		}

		max := math.MaxUint32 - value

		if bigValue.Sign() < 0 || bigValue.Cmp(big.NewInt(int64(max))) > 0 {
			if value == 0 {
				return nil, fmt.Errorf("component %v out of allowed range [0, %d]", bigValue, max)
			}

			return nil, fmt.Errorf("component %v out of allowed hardened range [0, %d]", bigValue, max)
		}

		value += uint32(bigValue.Uint64())

		result = append(result, value)
	}

	return result, nil
}

func (path DerivationPath) String() string {
	result := "m"

	for _, component := range path {
		var hardened bool

		if component >= 0x80000000 {
			component -= 0x80000000
			hardened = true
		}

		result = fmt.Sprintf("%s/%d", result, component)

		if hardened {
			result += "'"
		}
	}

	return result
}

func (path DerivationPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(path.String())
}

func (path *DerivationPath) UnmarshalJSON(b []byte) error {
	var dp string
	var err error

	if err = json.Unmarshal(b, &dp); err != nil {
		return err
	}

	*path, err = ParseDerivationPath(dp)
	return err
}

func DefaultIterator(base DerivationPath) func() DerivationPath {
	path := make(DerivationPath, len(base))

	copy(path[:], base[:])
	path[len(path)-1]--

	return func() DerivationPath {
		path[len(path)-1]++
		return path
	}
}

func LedgerLiveIterator(base DerivationPath) func() DerivationPath {
	path := make(DerivationPath, len(base))

	copy(path[:], base[:])
	path[2]--

	return func() DerivationPath {
		path[2]++
		return path
	}
}
