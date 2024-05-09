package cardanofw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Ethernal-Tech/cardano-infrastructure/wallet"
)

func ResolveCardanoCliBinary() string {
	bin := os.Getenv("CARDANO_CLI_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "cardano-cli"
}

func ResolveApexBridgeBinary() string {
	bin := os.Getenv("APEX_BRIDGE_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "apex-bridge"
}

func ResolveBladeBinary() string {
	bin := os.Getenv("BLADE_BINARY")
	if bin != "" {
		return bin
	}
	// fallback
	return "blade"
}

func RunCommandContext(
	ctx context.Context, binary string, args []string, stdout io.Writer, envVariables ...string,
) error {
	cmd := exec.CommandContext(ctx, binary, args...)

	return runCommand(cmd, stdout, envVariables...)
}

// runCommand executes command with given arguments
func RunCommand(binary string, args []string, stdout io.Writer, envVariables ...string) error {
	cmd := exec.Command(binary, args...)

	return runCommand(cmd, stdout, envVariables...)
}

func runCommand(cmd *exec.Cmd, stdout io.Writer, envVariables ...string) error {
	var stdErr bytes.Buffer

	cmd.Stderr = &stdErr
	cmd.Stdout = stdout

	cmd.Env = append(os.Environ(), envVariables...)
	// fmt.Printf("$ %s %s\n", binary, strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		if stdErr.Len() > 0 {
			return fmt.Errorf("failed to execute command: %s", stdErr.String())
		}

		return fmt.Errorf("failed to execute command: %w", err)
	}

	if stdErr.Len() > 0 {
		return fmt.Errorf("error during command execution: %s", stdErr.String())
	}

	return nil
}

func LoadJSON[TReturn any](path string) (*TReturn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %v. error: %w", path, err)
	}

	defer f.Close()

	var value TReturn

	if err = json.NewDecoder(f).Decode(&value); err != nil {
		return nil, fmt.Errorf("failed to decode %v. error: %w", path, err)
	}

	return &value, nil
}

func WaitUntil(
	t *testing.T,
	ctx context.Context, provider wallet.ITxProvider,
	timeoutDuration time.Duration,
	handler func(wallet.QueryTipData) bool,
) error {
	t.Helper()

	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()

	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout")
		case <-ticker.C:
		}

		tip, err := provider.GetTip(ctx)
		if err != nil {
			t.Log("error while retrieving tip", "err", err)
		} else if handler(tip) {
			return nil
		}
	}
}

func WaitUntilBlock(
	t *testing.T,
	ctx context.Context, provider wallet.ITxProvider,
	blockNum uint64, timeoutDuration time.Duration,
) error {
	t.Helper()

	return WaitUntil(t, ctx, provider, timeoutDuration, func(qtd wallet.QueryTipData) bool {
		return qtd.Block >= blockNum
	})
}

// SplitString splits large string into slice of substrings
func SplitString(s string, mxlen int) (res []string) {
	for i := 0; i < len(s); i += mxlen {
		end := i + mxlen
		if end > len(s) {
			end = len(s)
		}

		res = append(res, s[i:end])
	}

	return res
}
