package blockfrost

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

func ResetDBSync(
	t *testing.T,
	timeoutDuration time.Duration, startAfter time.Duration, dbSyncContainer string,
) error {
	t.Helper()
	time.Sleep(startAfter)

	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()

	ticker := time.NewTicker(time.Second * 20)
	defer ticker.Stop()

	const targetBlockNum = 50

	for {
		select {
		case <-timeout.C:
			return fmt.Errorf("timeout")
		case <-ticker.C:
		}

		t.Log("Check Db Sync logs")

		res, err := runCommand("docker", []string{"logs", dbSyncContainer})
		if err != nil {
			return err
		}

		logs := strings.Split(res, "\n")
		if len(logs) < 2 {
			continue
		}

		lastLog := logs[len(logs)-2] // last is empty string so we take one before last

		if strings.Contains(lastLog, "Creating Indexes. This may take a while.") {
			t.Log("Restarting db sync docker container")
			_, err := runCommand("docker", []string{"restart", dbSyncContainer}) //nolint

			if err != nil {
				t.Log(err.Error())
			}
		} else if strings.Contains(lastLog, "Insert Babbage Block") {
			blockNum, err := extractBlockNumber(lastLog)
			if err != nil {
				t.Log(err.Error())
			} else if blockNum >= targetBlockNum {
				break
			}
		}
	}

	return nil
}

func extractBlockNumber(inputString string) (int, error) {
	// Define the regular expression pattern to match the block number
	pattern := `block (\d+)`

	// Compile the regular expression pattern
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return 0, err
	}

	// Use FindStringSubmatch to find the first occurrence of the pattern in the input string
	match := regex.FindStringSubmatch(inputString)

	// If a match is found, extract and return the block number
	if len(match) > 1 {
		var blockNumber int
		_, err := fmt.Sscanf(match[1], "%d", &blockNumber)

		if err != nil {
			return 0, err
		}

		return blockNumber, nil
	}

	// If no match is found, return an error
	return 0, fmt.Errorf("no block number found in the input string")
}
