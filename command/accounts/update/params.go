package accounts

const (
	AddressFlag    = "address"
	PassphraseFlag = "passphrase"
)

type updateParams struct {
	Address    string
	Passphrase string
}
