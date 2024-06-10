package insert

import (
	"encoding/hex"
	"fmt"

	"github.com/0xPolygon/polygon-edge/accounts"
	"github.com/0xPolygon/polygon-edge/accounts/keystore"
	"github.com/0xPolygon/polygon-edge/command"
	"github.com/0xPolygon/polygon-edge/command/helper"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/cobra"
)

var (
	params insertParams
)

func GetCommand() *cobra.Command {
	importCmd := &cobra.Command{
		Use:     "insert",
		Short:   "Import existing account with private key and auth passphrase",
		PreRunE: runPreRun,
		Run:     runCommand,
	}

	helper.RegisterJSONRPCFlag(importCmd)
	setFlags(importCmd)

	return importCmd
}

func setFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&params.privateKey,
		PrivateKeyFlag,
		"",
		"privateKey for insert account",
	)

	cmd.Flags().StringVar(
		&params.keyDir,
		KeyDirFlag,
		"",
		"dir for document that contains private key",
	)

	cmd.Flags().StringVar(
		&params.passphrase,
		PassphraseFlag,
		"",
		"passphrase for access to private key",
	)

	_ = cmd.MarkFlagRequired(PassphraseFlag)
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	return nil
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	scryptN := keystore.LightScryptN
	scryptP := keystore.LightScryptP

	am := accounts.NewManager(&accounts.Config{}, nil)

	am.AddBackend(keystore.NewKeyStore(keystore.DefaultStorage, scryptN, scryptP, hclog.NewNullLogger()))

	if params.privateKey == "" {
		outputter.SetError(fmt.Errorf("private key empty"))
	}

	dec, err := hex.DecodeString(params.privateKey)
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

	acct, err := ks.ImportECDSA(privKey, params.passphrase)
	if err != nil {
		outputter.SetError(fmt.Errorf("cannot import private key"))
	}

	outputter.SetCommandResult(command.Results{&insertResult{Address: acct.Address}})
}
