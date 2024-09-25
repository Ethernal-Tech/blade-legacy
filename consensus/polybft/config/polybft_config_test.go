package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/0xPolygon/polygon-edge/types"
)

// TestBridgeConfig_getInternalContractAddrs ensures that all fields in BridgeConfig whose name starts with "Internal" are returned.
func TestBridgeConfig_getInternalContractAddrs(t *testing.T) {
	// Mock addresses for testing
	mockAddr := types.Address{0x1}

	// Initialize a BridgeConfig struct with all Internal fields set to mockAddr
	config := &Bridge{
		InternalGatewayAddr:                  mockAddr,
		InternalERC20PredicateAddr:           mockAddr,
		InternalERC721PredicateAddr:          mockAddr,
		InternalERC1155PredicateAddr:         mockAddr,
		InternalMintableERC20PredicateAddr:   mockAddr,
		InternalMintableERC721PredicateAddr:  mockAddr,
		InternalMintableERC1155PredicateAddr: mockAddr,
	}

	// Get internal contract addresses from the function
	internalAddrs := config.getInternalContractAddrs()

	// Use reflection to find all fields that start with "Internal" and are of type `types.Address`
	var (
		expectedAddrs []types.Address
		val           = reflect.ValueOf(config).Elem()
		typ           = reflect.TypeOf(*config)
	)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if strings.HasPrefix(field.Name, "Internal") && field.Type == reflect.TypeOf(types.Address{}) {
			expectedAddrs = append(expectedAddrs, val.Field(i).Interface().(types.Address))
		}
	}

	// Ensure that the function output matches the expected addresses derived from reflection
	assert.ElementsMatch(t, expectedAddrs, internalAddrs, "The internal contract addresses should match the expected internal addresses")
}
