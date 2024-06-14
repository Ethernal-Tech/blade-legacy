package insert

import (
	"encoding/hex"
	"errors"
	"fmt"

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
		Short:   "Insert existing account with private key and auth passphrase",
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
		privateKeyFlag,
		"",
		"privateKey key of new account",
	)

	cmd.Flags().StringVar(
		&params.passphrase,
		passphraseFlag,
		"",
		"passphrase for access to private key",
	)

	_ = cmd.MarkFlagRequired(passphraseFlag)
}

func runPreRun(cmd *cobra.Command, _ []string) error {
	return nil
}

func runCommand(cmd *cobra.Command, _ []string) {
	outputter := command.InitializeOutputter(cmd)

	ks := keystore.NewKeyStore(keystore.DefaultStorage,
		keystore.LightScryptN, keystore.LightScryptP, hclog.NewNullLogger())

	if params.privateKey == "" {
		outputter.SetError(errors.New("private key empty"))
	}

	dec, err := hex.DecodeString(params.privateKey)
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to decode private ke: %w", err))
	}

	privKey, err := crypto.BytesToECDSAPrivateKey(dec)
	if err != nil {
		outputter.SetError(fmt.Errorf("failed to initialize private key: %w", err))
	}

	acct, err := ks.ImportECDSA(privKey, params.passphrase)
	if err != nil {
		outputter.SetError(fmt.Errorf("cannot import private key: %w", err))
	}

	outputter.SetCommandResult(command.Results{&insertResult{Address: acct.Address}})
}
