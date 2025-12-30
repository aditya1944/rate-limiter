package ratelimiter

import (
	"fmt"
	"math"
	"testing"
	"testing/synctest"
	"time"
)

func TestInput(t *testing.T) {
	t.Parallel()

	tcs := []struct {
		name        string
		tokenRate   float64
		burstSize   uint
		shouldError bool
	}{
		{
			name:        "token rate is lower than burst size",
			tokenRate:   21,
			burstSize:   32,
			shouldError: false,
		},
		{
			name:        "when tokenrate and burstSize is MaxUint",
			tokenRate:   math.MaxUint,
			burstSize:   math.MaxUint,
			shouldError: true,
		},
		{
			name:        "boundary condition for code to not panic",
			tokenRate:   math.MaxUint / 5000,
			burstSize:   0,
			shouldError: false,
		},
		{

			name:        "boundary condition for code to panic",
			tokenRate:   (math.MaxUint / 5000) + 1,
			burstSize:   0,
			shouldError: true,
		},
		{
			name:        "token rate is higher than burst size",
			tokenRate:   23,
			burstSize:   20,
			shouldError: false,
		},
		{
			name:        "token rate is negative",
			tokenRate:   -2.34,
			burstSize:   23,
			shouldError: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(tc.tokenRate, tc.burstSize)

			if tc.shouldError && err == nil {
				t.Errorf("expected error, but got nil error")
			}

			if !tc.shouldError && err != nil {
				t.Errorf("not expected error but got error")
			}
		})
	}
}

func TestAllow(t *testing.T) {
	t.Parallel()

	tcs := []struct {
		name      string
		tokenRate float64
		burstSize uint
		requests  int
		allowed   int
	}{
		{
			name:      "when tokenrate is 10 and burstsize is 10",
			tokenRate: 10,
			burstSize: 10,
			requests:  10,
			allowed:   10,
		},
		{
			name:      "when token rate is greater than burstsize",
			tokenRate: 11,
			burstSize: 10,
			requests:  11,
			allowed:   10,
		},
		{
			name:      "when burst size is 0",
			tokenRate: 10,
			burstSize: 0,
			requests:  5,
			allowed:   0,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rateLimiter, _ := New(tc.tokenRate, tc.burstSize)
			defer rateLimiter.Close()

			allowedRequests := 0
			for range tc.requests {
				if allowed := rateLimiter.Allow("key"); allowed {
					allowedRequests += 1
				}
			}

			if allowedRequests != tc.allowed {
				t.Errorf("expected allowed requests: %d, got : %v", allowedRequests, tc.allowed)
			}
		})
	}
}

func TestAllowWhenKeyIsEvictedFromCache(t *testing.T) {

	synctest.Test(t, func(t *testing.T) {

		rateLimiter, _ := New(0.0167, 1) // one token per minute; burst size of 1
		defer rateLimiter.Close()

		if allowed := rateLimiter.Allow("user1"); !allowed {
			t.Fatal("expected allowed to be true, got false")
		}

		if allowed := rateLimiter.Allow("user1"); allowed {
			t.Fatal("expected allowed to be false, got true")
		}

		time.Sleep(1*time.Hour + 5*time.Minute)

		synctest.Wait()

		if allowed := rateLimiter.Allow("user1"); !allowed {
			t.Fatal("expected key to be evicted, but it wasn't")
		}
	})
}

func TestAllowTokenRateFill(t *testing.T) {

	synctest.Test(t, func(t *testing.T) {

		rateLimiter, _ := New(1, 10) // one token per second; burst size of 10
		defer rateLimiter.Close()

		for range 10 {

			if allowed := rateLimiter.Allow("user1"); !allowed {
				t.Fatal("expected allowed to be true, got false")
			}
		}

		if allowed := rateLimiter.Allow("user1"); allowed {
			t.Fatal("expected allowed to be false, got true")
		}

		time.Sleep(5 * time.Second)

		synctest.Wait()

		for range 5 {
			if allowed := rateLimiter.Allow("user1"); !allowed {
				t.Fatal("expected allowed to be true, got false")
			}
		}

		if allowed := rateLimiter.Allow("user1"); allowed {
			t.Fatal("expected allowed to be false, got true")
		}
	})
}

func TestAllowSteadyTraffic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rateLimiter, _ := New(10, 10)
		defer rateLimiter.Close()

		for range 10 {
			if !rateLimiter.Allow("key") {
				t.Error("expected ratelimiter to allow, but not allowed")
			}
		}

		if rateLimiter.Allow("key") {
			t.Error("expected 11th request to not be allowed, but allowed")
		}

		time.Sleep(500 * time.Millisecond)
		synctest.Wait()

		for range 5 {
			if !rateLimiter.Allow("key") {
				t.Error("expected ratelimiter to allow, but not allowed")
			}
		}

		if rateLimiter.Allow("key") {
			t.Error("expected 6th request to be not allowed, but allowed")
		}

	})
}

func BenchmarkAllow(b *testing.B) {
	rateLimiter, _ := New(1000, 10000)
	defer rateLimiter.Close()

	i := 0

	for b.Loop() {
		rateLimiter.Allow(fmt.Sprintf("key%d", i))
		i++
		i = i % 10
	}
}

func BenchmarkAllowParallel(b *testing.B) {
	rateLimiter, _ := New(1000, 10000)
	defer rateLimiter.Close()

	b.ResetTimer()

	b.RunParallel(func(p *testing.PB) {
		i := 0
		for p.Next() {
			rateLimiter.Allow(fmt.Sprintf("key%d", i))
			i++
			i = i % 10
		}
	})
}
