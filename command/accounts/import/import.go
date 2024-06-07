package accounts

import (
	"encoding/hex"
	"fmt"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/spf13/cobra"
)

var (
	params importParams
)

func GetCommand() *cobra.Command {
	importCmd := &cobra.Command{
		Use:    "import",
		Short:  "Import existing account with private key and auth passphrase",
		PreRun: runPreRun,
		Run:    runCommand,
	}

	helper.RegisterJSONRPCFlag(importCmd)
	setFlags(importCmd)

	return importCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.PrivateKey,
		PrivateKeyFlag,
		"",
		"privateKey for import account",
	)

	cmd.Flags().StringVar(
		&params.KeyDir,
		KeyDirFlag,
		"",
		"dir for document that contains private key",
	)

	cmd.Flags().StringVar(
		&params.Passphrase,
		PassphraseFlag,
		"",
		"passphrase for access to private key",
	)

	cmd.Flags().StringVar(
		&params.ConfigDir,
		ConfigDirFlag,
		"",
		"dir of config file",
	)

	_ = cmd.MarkFlagRequired(PassphraseFlag)
}

func runPreRun(cmd *cobra.Command, _ []string) {
	return
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	scryptN := keystore.StandardScryptN
	scryptP := keystore.StandardScryptP
	if false {
		scryptN = keystore.LightScryptN
		scryptP = keystore.LightScryptP
	}

	am := accounts.NewManager(&accounts.Config{}, nil)

	am.AddBackend(keystore.NewKeyStore(params.KeyDir, scryptN, scryptP, nil))

	if params.PrivateKey == "" {
		outputter.SetError(fmt.Errorf("private key empty"))
	}

	dec, err := hex.DecodeString(params.PrivateKey)
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to decode private key"))
	}

	privKey, err := crypto.BytesToECDSAPrivateKey(dec)
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to initialize private key"))
	}

	backends := am.Backends(keystore.KeyStoreType)
	if len(backends) == 0 {
		outputter.SetError(fmt.Errorf("keystore is not available"))
	}

	ks := backends[0].(*keystore.KeyStore) //nolint:forcetypeassert

	acct, err := ks.ImportECDSA(privKey, params.Passphrase)
	if err != nil {
		outputter.SetError(fmt.Errorf("cannot import private key"))
	}

	outputter.SetCommandResult(command.Results{&importResult{Address: acct.Address}})
}
