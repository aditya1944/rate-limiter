package ratelimiter

import (
	"errors"
	"math"
	"sync"
	"time"
)

const maxCASRetries = 100

var now = time.Now

type bucket struct {
	tokens       uint
	lastRefill   time.Time
	lastActivity time.Time
}

type rateLimiter struct {
	tokenRate float64
	burstSize uint

	m    sync.Map
	done chan struct{}
}

// When burstSize = 0, then all requests will be rejected
// When tokenRate = 0, then for every unique key, only "burstSize" number of requests
// will be let through for one session(~1 hour).
func New(tokenRate float64, burstSize uint) (*rateLimiter, error) {

	// (tokenRate * 5000 + burstSize) <= 2 ^ (arch size)
	// 5000 seconds is time elapsed, if key were to remain until that time(taking worst case)

	if err := validate(tokenRate, burstSize); err != nil {
		return nil, err
	}

	r := &rateLimiter{
		tokenRate: tokenRate,
		burstSize: burstSize,

		m:    sync.Map{},
		done: make(chan struct{}),
	}

	go func() {
		// this goroutine will iterate over map every 5 minutes and
		// delete those keys which have lastactivity older than equal
		// to 1 hour.
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.m.Range(func(key, val any) bool {
					buck := val.(bucket)
					t := now()
					if t.Sub(buck.lastActivity) >= time.Hour {
						r.m.Delete(key)
					}
					return true
				})
			case <-r.done:
				return
			}
		}
	}()

	return r, nil
}

func (r *rateLimiter) Allow(key string) bool {
	if r.burstSize == 0 {
		// no capacity, reject all request
		return false
	}
	for range maxCASRetries {
		t := now()
		val, ok := r.m.Load(key)
		if !ok {
			// Try to be the first to create this key
			b := bucket{
				tokens:       r.burstSize - 1, // -1 is to consume one token for current request
				lastRefill:   t,
				lastActivity: t,
			}
			actual, loaded := r.m.LoadOrStore(key, b)
			if !loaded {
				// this means, this was the first time `key` is inserted
				return true
			}
			// some other goroutine created entry with `key`
			val = actual
		}

		// flow will reach here when key is not inserted for
		// the first time. we will need to update the value
		t = now()

		buck, ok := val.(bucket)
		if !ok {
			panic("val should be of bucket type")
		}

		// first, fill the bucket with desired token rate
		timeElapsed := t.Sub(buck.lastRefill)

		newTokens := min(r.burstSize, uint(r.tokenRate*timeElapsed.Seconds())+buck.tokens)
		if buck.tokens != newTokens {
			buck.tokens = newTokens
			buck.lastRefill = t
		}

		if buck.tokens > 0 {
			// lastactivity updation is not outside of this `if` block
			// because a malicious attacker can keep the
			// rate limited key active and hence prevent it
			// from cleanup.
			buck.lastActivity = t
			// consume a token
			buck.tokens -= 1
			if swapped := r.m.CompareAndSwap(key, val, buck); swapped {
				return true
			}
			// some other goroutine modified the entry with that key
			// retry again
			continue
		}
		// flow will reach here when there are no tokens left
		return false
	}
	// retry limit exhausted
	return false
}

func (r *rateLimiter) Close() {
	close(r.done)
}

func validate(tokenRate float64, burstSize uint) error {

	if tokenRate < 0 {
		return errors.New("token rate should not be negative")
	}

	// tokenRate * 5000 should not be over uint limit as it will overflow at line 105
	// every 3600 seconds, cleanup goroutine cleanup keys which have lastactivity greater than
	// 3600 seconds.
	// there can be such case where cleanup activity takes more time, hence 5000 is used
	if tokenRate*5000 > math.MaxUint {
		return errors.New("token rate limit overflow")
	}

	// check if a * 5000 + b <= 2 ^ maxIntSize
	// => a * 5000 <= (2 ^ maxIntSize) - b
	// -> a <= ((2 ^ maxIntSize) - b) / 5000

	var maxValue uint = math.MaxUint

	if tokenRate > float64(maxValue-burstSize)/5000.0 {
		return errors.New("limit overflow")
	}
	return nil
}
