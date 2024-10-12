package jsonrpc

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThrottling(t *testing.T) {
	t.Parallel()

	const maxRequests = 5

	var requests atomic.Int32

	var attempts = []struct {
		duration time.Duration
		delay    time.Duration
	}{
		// 1st 5 starts immediately, no error, order of execution is irrelevant
		{300 * time.Millisecond, 0},
		{1200 * time.Millisecond, 0},
		{1200 * time.Millisecond, 0},
		{1200 * time.Millisecond, 0},
		{1200 * time.Millisecond, 0},

		// 6th & 8th attempt should fail, from now on order of execution is relevant, hence delay > 0
		{20 * time.Millisecond, 100 * time.Millisecond},
		{300 * time.Millisecond, 500 * time.Millisecond},
		{20 * time.Millisecond, 600 * time.Millisecond},
		{200 * time.Millisecond, 1000 * time.Millisecond},
	}

	th := NewThrottling(maxRequests, 20*time.Millisecond)
	sfn := func(value int, sleep time.Duration) func() (interface{}, error) {
		return func() (interface{}, error) {
			time.Sleep(sleep)

			return value, nil
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(len(attempts))

	for i := 0; i < len(attempts); i++ {
		go func(value int, duration time.Duration, delay time.Duration) {
			defer wg.Done()
			time.Sleep(delay)

			var isError bool
			if requests.Add(1) > maxRequests {
				isError = true
			}

			res, err := th.AttemptRequest(context.Background(), sfn(value, duration))

			requests.Add(-1)

			if isError {
				require.ErrorIs(t, err, errRequestLimitExceeded)
				assert.Nil(t, res)
			} else {
				require.NoError(t, err)
				assert.Equal(t, value, res.(int))
			}
		}(i, attempts[i].duration, attempts[i].delay)
	}

	wg.Wait()
}
