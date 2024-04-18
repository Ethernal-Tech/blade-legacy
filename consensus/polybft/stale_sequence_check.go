package polybft

import (
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
)

type staleSequenceCheck struct {
	logger             hclog.Logger
	currentSequence    uint64
	mtx                *sync.Mutex
	checkFrequency     time.Duration
	sequenceShouldStop chan struct{}
	sequenceStopping   chan struct{}
	stop               chan struct{}
	stopped            chan struct{}
	getHeader          func() *types.Header
}

func newStaleSequenceCheck(logger hclog.Logger,
	getHeader func() *types.Header,
	checkDuration time.Duration,
) *staleSequenceCheck {
	return &staleSequenceCheck{
		logger:          logger,
		currentSequence: 0,
		mtx:             &sync.Mutex{},
		checkFrequency:  checkDuration,
		getHeader:       getHeader,
	}
}

func (s *staleSequenceCheck) startChecking() {
	s.sequenceShouldStop = make(chan struct{}, 1)
	s.sequenceStopping = make(chan struct{})
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})

	ticker := time.NewTicker(s.checkFrequency)

	go func() {
		defer close(s.stopped)

		for {
			select {
			case <-s.stop:
				close(s.sequenceShouldStop)
				ticker.Stop()

				return
			case <-s.sequenceStopping:
				ticker.Stop()
			case <-ticker.C:
				s.checkForStaleness()
			}
		}
	}()
}

func (s *staleSequenceCheck) stopChecking() {
	close(s.stop)
	<-s.stopped
}

func (s *staleSequenceCheck) setSequence(sequence uint64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.currentSequence = sequence
}

func (s *staleSequenceCheck) checkForStaleness() {
	s.logger.Info("[staleSequenceCheck] checking for stale sequence")
	header := s.getHeader()
	s.chainHeightUpdated(header.Number)
}

func (s *staleSequenceCheck) chainHeightUpdated(height uint64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.currentSequence == 0 {
		return
	}

	if height >= s.currentSequence {
		s.logger.Info("[staleSequenceCheck] stale sequence detected", "height", height, "currentSequence", s.currentSequence)
		s.sequenceShouldStop <- struct{}{}
		close(s.sequenceStopping)
	}
}
