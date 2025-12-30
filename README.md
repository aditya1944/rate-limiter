# Rate Limiter

A high-performance, thread-safe, per-key rate limiter implementation using the Token Bucket algorithm. Written in pure Go with zero external dependencies.

## Features

- **Token Bucket Algorithm**: Allows burst traffic while maintaining a steady average rate
- **Per-Key Rate Limiting**: Each key (user ID, IP address, API key) has its own independent bucket
- **Thread-Safe**: Lock-free implementation using `sync.Map` and Compare-And-Swap (CAS) operations
- **Memory Efficient**: Automatic cleanup of inactive keys after 1 hour
- **Zero Dependencies**: Uses only Go standard library

## Installation

```bash
go get github.com/aditya1944/rate-limiter
```

## Quick Start

```go
package main

import (
    ratelimiter "github.com/aditya1944/rate-limiter"
)

func main() {
    // Create a rate limiter: 10 tokens/second, burst size of 20
    limiter, err := ratelimiter.New(10, 20)
    if err != nil {
        panic(err)
    }
    defer limiter.Close()

    // Check if request is allowed
    if limiter.Allow("user-123") {
        // Request allowed, process it
    } else {
        // Request denied, return 429 Too Many Requests
    }
}
```

## API Reference

### `New(tokenRate float64, burstSize uint) (*rateLimiter, error)`

Creates a new rate limiter instance.

**Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `tokenRate` | `float64` | Number of tokens added per second. Use fractional values for slower rates (e.g., `0.0167` for 1 token per minute) |
| `burstSize` | `uint` | Maximum number of tokens a bucket can hold. This is also the initial token count for new keys |

**Returns:**
- `*rateLimiter`: The rate limiter instance
- `error`: Non-nil if validation fails

**Validation Errors:**
- `tokenRate` cannot be negative
- `tokenRate * 5000 + burstSize` must not overflow `uint` (prevents integer overflow during token calculation)

**Special Cases:**
| tokenRate | burstSize | Behavior |
|-----------|-----------|----------|
| `0` | `N` | Each key gets exactly `N` requests total (no refill) |
| `N` | `0` | All requests are rejected |

### `Allow(key string) bool`

Checks if a request for the given key should be allowed.

**Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `key` | `string` | Unique identifier for the rate limit bucket (e.g., user ID, IP address) |

**Returns:**
- `true`: Request is allowed, one token consumed
- `false`: Request denied (no tokens available or CAS retry limit exceeded)

**Thread Safety:** Safe to call concurrently from multiple goroutines.

### `Close()`

Stops the background cleanup goroutine. Always call this when the rate limiter is no longer needed to prevent goroutine leaks.

```go
limiter, _ := ratelimiter.New(10, 20)
defer limiter.Close()
```

## How It Works

### Token Bucket Algorithm

```
                    +------------------+
  Tokens added      |                  |      Tokens consumed
  at `tokenRate`    |     Bucket       |      by requests
  per second   ---> |  (max: burstSize)|--->
                    |                  |
                    +------------------+
```

1. Each key has a "bucket" that holds tokens
2. Tokens are added at `tokenRate` tokens per second
3. The bucket has a maximum capacity of `burstSize`
4. Each `Allow()` call consumes 1 token if available
5. If no tokens are available, the request is denied

### Concurrency Model

The implementation uses a **lock-free** approach with `sync.Map` and Compare-And-Swap (CAS):

```
Goroutine A                           Goroutine B
-----------                           -----------
1. Load bucket (tokens=5)
2.                                    Load bucket (tokens=5)
3. Decrement (tokens=4)
4. CAS(old=5, new=4) -> SUCCESS
5.                                    Decrement (tokens=4)
6.                                    CAS(old=5, new=4) -> FAIL (value changed!)
7.                                    Retry from step 1...
```

This ensures that under concurrent access:
- No tokens are "double spent"
- No updates are lost
- Maximum retry attempts: 100 (returns `false` if exceeded)

### Memory Management

A background goroutine runs every 5 minutes to clean up inactive keys:

```
+-- Every 5 minutes --+
|                     |
|  For each key:      |
|    If lastActivity  |
|    >= 1 hour ago    |
|      -> Delete key  |
|                     |
+---------------------+
```

**Security Note:** The `lastActivity` timestamp is only updated on **successful** requests. This prevents attackers from keeping a rate-limited key alive indefinitely by sending blocked requests.

## Usage Examples

### HTTP Middleware

```go
func RateLimitMiddleware(limiter *ratelimiter.RateLimiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip := r.RemoteAddr
            if !limiter.Allow(ip) {
                http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### Per-User API Rate Limiting

```go
// 100 requests per minute, burst of 20
limiter, _ := ratelimiter.New(100.0/60.0, 20)
defer limiter.Close()

func handleRequest(userID string) error {
    if !limiter.Allow(userID) {
        return errors.New("rate limit exceeded")
    }
    // Process request...
    return nil
}
```

### Different Rates for Different Endpoints

```go
// Strict limit for login attempts: 5 per minute, burst of 3
loginLimiter, _ := ratelimiter.New(5.0/60.0, 3)

// Generous limit for API calls: 1000 per minute, burst of 100
apiLimiter, _ := ratelimiter.New(1000.0/60.0, 100)
```

## Configuration Guide

### Choosing `tokenRate`

| Use Case | Requests/min | tokenRate |
|----------|--------------|-----------|
| Login attempts | 5 | `5.0/60.0` = `0.0833` |
| API calls | 60 | `1.0` |
| High-traffic API | 6000 | `100.0` |

### Choosing `burstSize`

- **Low burst (1-5)**: Strict rate limiting, minimal spike tolerance
- **Medium burst (10-50)**: Allows reasonable traffic spikes
- **High burst (100+)**: Tolerates large traffic bursts

### Common Configurations

```go
// Strict: 1 request per second, no burst
ratelimiter.New(1, 1)

// Standard API: 10 req/sec, burst of 20
ratelimiter.New(10, 20)

// High-traffic: 100 req/sec, burst of 500
ratelimiter.New(100, 500)

// Login protection: 1 per minute, burst of 3
ratelimiter.New(0.0167, 3)
```

## Benchmarks

Benchmarks run on Apple M1 Pro (10 cores), Go 1.25.5:

```
goos: darwin
goarch: arm64
cpu: Apple M1 Pro
```

| Benchmark | ops/sec | ns/op | B/op | allocs/op |
|-----------|---------|-------|------|-----------|
| `Allow` (single goroutine) | ~6M | 170 | 6 | 1 |
| `Allow` (parallel, 10 cores) | ~28M | 43 | 4 | 1 |

**Key Takeaways:**

- **Single-threaded**: ~6 million `Allow()` calls per second (~170ns per call)
- **Multi-threaded**: ~28 million `Allow()` calls per second (~43ns per call)
- **Memory**: Only 4-6 bytes allocated per call (from `fmt.Sprintf` in benchmark, not the limiter itself)
- **Scalability**: Near-linear scaling with CPU cores due to lock-free design

Run benchmarks on your system:

```bash
go test -bench=. -benchmem -count=3
```

## Limitations

1. **Single-Instance Only**: This is an in-memory rate limiter. For distributed systems, use Redis-based solutions.

2. **Clock Dependency**: Token refill depends on system time. Clock skew or NTP adjustments may cause brief inconsistencies.

3. **Memory Usage**: Each active key consumes ~64 bytes. For millions of keys, monitor memory usage.

4. **No Persistence**: Rate limit state is lost on restart.

## Running Tests

```bash
# Run all tests
go test -v ./...

# Run tests with race detector
go test -race ./...

# Run benchmarks
go test -bench=. -benchmem
```

## License

MIT
