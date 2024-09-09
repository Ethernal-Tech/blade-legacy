package types

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEIP55(t *testing.T) {
	t.Parallel()

	cases := []struct {
		address  string
		expected string
	}{
		{
			"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed",
			"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		},
		{
			"0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359",
			"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		},
		{
			"0xdbf03b407c01e7cd3cbea99509d93f8dddc8c6fb",
			"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		},
		{
			"0xd1220a0cf47c7b9be7a2e6ba89f429762e7b9adb",
			"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
		},
		{
			"0xde64a66c41599905950ca513fa432187a8c65679",
			"0xde64A66C41599905950ca513Fa432187a8C65679",
		},
		{
			"0xb41364957365228984ea8ee98e80dbed4b9ffcdc",
			"0xB41364957365228984eA8EE98e80DBED4B9fFcDC",
		},
		{
			"0xb529594951753de833b00865d7fe52cc4d8b0f63",
			"0xB529594951753DE833b00865D7FE52cC4d8B0f63",
		},
		{
			"0xb529594951753de833b00865",
			"0x0000000000000000B529594951753De833B00865",
		},
		{
			"0xeEd210D",
			"0x000000000000000000000000000000000eED210d",
		},
	}

	for _, c := range cases {
		c := c

		t.Run("", func(t *testing.T) {
			t.Parallel()

			addr := StringToAddress(c.address)
			assert.Equal(t, c.expected, addr.String())
		})
	}
}

func TestTransactionCopy(t *testing.T) {
	addrTo := StringToAddress("11")
	txn := NewTx(&DynamicFeeTx{
		GasTipCap: big.NewInt(11),
		GasFeeCap: big.NewInt(11),
		BaseTx: &BaseTx{
			Nonce: 0,
			Gas:   11,
			To:    &addrTo,
			Value: big.NewInt(1),
			Input: []byte{1, 2},
			V:     big.NewInt(25),
			S:     big.NewInt(26),
			R:     big.NewInt(27),
		},
	})
	newTxn := txn.Copy()

	if !reflect.DeepEqual(txn, newTxn) {
		t.Fatal("[ERROR] Copied transaction not equal base transaction")
	}
}

func TestIsValidAddress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		address      string
		isValid      bool
		expectedAddr Address
	}{
		{
			address: "0x123",
			isValid: false,
		},
		{
			address: "FooBar",
			isValid: false,
		},
		{
			address: "123FooBar",
			isValid: false,
		},
		{
			address:      "0x1234567890987654321012345678909876543210",
			isValid:      true,
			expectedAddr: StringToAddress("0x1234567890987654321012345678909876543210"),
		},
		{
			address:      "0x0000000000000000000000000000000000000000",
			isValid:      true,
			expectedAddr: StringToAddress("0x0000000000000000000000000000000000000000"),
		},
		{
			address:      "0x1000000000000000000000000000000000000000",
			isValid:      true,
			expectedAddr: StringToAddress("0x0000000000000000000000000000000000000000"),
		},
	}

	for _, c := range cases {
		addr, err := IsValidAddress(c.address, true)
		if c.isValid {
			require.NoError(t, err)
			require.Equal(t, StringToAddress(c.address), addr)
		} else {
			require.Error(t, err)
			require.Equal(t, addr, ZeroAddress)
		}
	}
}

func TestIncrementAddressBy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		address   string
		increment uint64
		expected  string
	}{
		{
			"0x0000000000000000000000000000000000000000",
			1,
			"0x0000000000000000000000000000000000000001",
		},
		{
			"0x0000000000000000000000000000000000000001",
			1,
			"0x0000000000000000000000000000000000000002",
		},
		{
			"0x0000000000000000000000000000000000000001",
			2,
			"0x0000000000000000000000000000000000000003",
		},
		{
			"0x00000000000000000000000000000000000000ff",
			1,
			"0x0000000000000000000000000000000000000100",
		},
		{
			"0x00000000000000000000000000000000ffffffff",
			1,
			"0x0000000000000000000000000000000100000000",
		},
		{
			"0x0000000000000000000000000000000000001001",
			30,
			"0x000000000000000000000000000000000000101f",
		},
		{
			"0xffffffffffffffffffffffffffffffffffffffff",
			1,
			"0x0000000000000000000000000000000000000000",
		},
	}

	for _, c := range cases {
		c := c

		t.Run("", func(t *testing.T) {
			t.Parallel()

			addr := StringToAddress(c.address)
			expectedAddr := StringToAddress(c.expected)
			incrementedAddr := IncrementAddressBy(addr, c.increment)

			assert.Equal(t, expectedAddr, incrementedAddr, "expected address %s, got %s", expectedAddr.String(), incrementedAddr.String())
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		address1 string
		address2 string
		expected int
	}{
		{
			"0x0000000000000000000000000000000000000001",
			"0x0000000000000000000000000000000000000002",
			-1,
		},
		{
			"0x0000000000000000000000000000000000000002",
			"0x0000000000000000000000000000000000000001",
			1,
		},
		{
			"0x0000000000000000000000000000000000000001",
			"0x0000000000000000000000000000000000000001",
			0,
		},
		{
			"0xffffffffffffffffffffffffffffffffffffffff",
			"0x0000000000000000000000000000000000000000",
			1,
		},
		{
			"0x0000000000000000000000000000000000000000",
			"0xffffffffffffffffffffffffffffffffffffffff",
			-1,
		},
	}

	for _, c := range cases {
		c := c

		t.Run("", func(t *testing.T) {
			t.Parallel()

			addr1 := StringToAddress(c.address1)
			addr2 := StringToAddress(c.address2)
			result := addr1.Compare(addr2)

			assert.Equal(t, c.expected, result, "expected comparison result %d, got %d", c.expected, result)
		})
	}
}
