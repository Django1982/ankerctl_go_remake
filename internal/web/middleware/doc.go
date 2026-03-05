// Package middleware implements HTTP middleware for the ankerctl web server.
//
// Middleware components:
//   - auth:      API key authentication (GET public, POST/DELETE protected)
//   - security:  Security headers (CSP, X-Frame-Options, X-Content-Type-Options)
//   - ratelimit: IP-based rate limiting
//   - logging:   Access request logging
//
// Auth rules (matching Python exactly):
//   - GET requests are unauthenticated by default
//   - POST/DELETE always require auth (session cookie, X-Api-Key header, or ?apikey= param)
//   - Protected GET paths (explicit list) also require auth
//   - All /api/debug/* paths require auth (prefix match)
//   - Setup paths exempt when no printer configured
//
// Python source: web/__init__.py (_check_api_key, security headers)
package middleware
