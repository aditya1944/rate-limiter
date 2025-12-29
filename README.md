Fast local implementation of rate limiter using token bucket algorithm written 100% in go. Use like this:

```
rateLimiter := ratelimiter.New(0.0167, 10) // 1 token per minute, 10 tokens burst size

if rateLimiter.Allow("10.23.34.11") {
  // your code
}
```
