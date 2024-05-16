package sanitycheck

import (
	"testing"
	"time"
)

func TestSanityCheck(t *testing.T) {
	t.Skip("this is only added for the sake of the example and running it in local")

	config := &SanityCheckTestConfig{
		Mnemonic:        "code code code code code code code code code code code quality",
		JSONRPCUrl:      "http://localhost:10002",
		ReceiptsTimeout: time.Minute,
		ValidatorKeys:   []string{"1a7626c5a1d89030f300ca5f63eecac3bae3e56f14033ea2d9ad471e7c93020e"},
		EpochSize:       10,
	}

	runner, err := NewSanityCheckTestRunner(config)
	if err != nil {
		t.Fatal(err)
	}

	defer runner.Close()

	runner.Run()
}
