package common

const (
	PrivateKeyFlag = "private-key"
	KeyDirFlag     = "key-dir"
	PassphraseFlag = "passphrase"
)

type CommonParams struct {
	PrivateKey string
	KeyDir     string
	Passphrase string
}
