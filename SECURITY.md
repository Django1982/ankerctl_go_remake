# Security Policy

Thank you for responsibly reporting security issues in **ankerctl (Go Rewrite)**.

This policy explains:
- how to report vulnerabilities,
- what is considered security-relevant,
- how coordinated disclosure and fixes are handled.

## Supported Versions

Security fixes are provided for actively maintained versions only:

| Version | Supported |
| --- | --- |
| `main` (latest commit) | ✅ |
| Latest GitHub release | ✅ |
| Older releases | ⚠️ Best effort only; patches are generally not guaranteed |

> Note: For protocol/crypto issues (MQTT/PPPP), fixes are prioritized on `main` first and then released in a new tagged version.

## Security Contact / Vulnerability Reporting

Please **do not** report vulnerabilities publicly via Issues or Discussions.

Preferred channels:

1. **GitHub Private Vulnerability Report** (Security Advisory)
   - Open the repository → `Security` → `Report a vulnerability`
2. If unavailable: contact the maintainer directly on GitHub and clearly label it as a “Security Report”.

Helpful report contents:
- affected version / commit / deployment type (binary, Docker),
- step-by-step reproduction or PoC,
- expected vs. actual behavior,
- impact (e.g., auth bypass, RCE, information disclosure),
- relevant logs (without secrets),
- optional CVSS assessment.

## Coordinated Disclosure Process

The project follows coordinated disclosure best practices:

- **Triage acknowledgement:** within 72 hours
- **Initial technical assessment:** within 7 days
- **Status updates:** at least weekly until resolution
- **Target for critical issues:** patch as quickly as possible (typically ≤ 14 days, depending on reproducibility and complexity)

After a fix is available, maintainers may publish:
- a Security Advisory,
- mitigation guidance,
- references to affected commits/tags.

## Scope: What qualifies as a security issue?

Typical in-scope classes for this project:

- Authentication and authorization flaws (API key, session, WebSocket auth)
- Bypass of write-protection/API-key enforcement
- Secret exposure (`auth_token`, `mqtt_key`, `api_key`, session secret)
- Path traversal / unsafe file path handling (uploads, timelapse, logs)
- Command injection / unsafe process execution
- Cryptographic weaknesses in MQTT/PPPP implementation (beyond protocol constraints)
- Resource exhaustion / denial-of-service (e.g., unbounded upload/stream paths)
- Supply-chain risks (compromised dependencies, CI/CD pipeline integrity)

### Out of Scope (generally)

- Pure functional bugs without security impact
- Third-party environment misconfiguration outside project control
- Purely theoretical attacks without realistic exploitability
- Known upstream issues in external components without an available fix

## Current Security Baseline

The project already includes multiple security controls:

- API-key protection for write operations (HTTP + relevant WebSocket paths)
- Session/cookie secret handling with file permissions (`0600`) and protected config directory (`0700`)
- Upload limits / rate limiting / security headers in the web stack
- Redaction of sensitive values in logs
- Regular CI checks (`go test`, `go vet`, Dependabot)
- Emphasis on bit-exact protocol/crypto compatibility with the Python reference implementation

## Hardening & Deployment Recommendations

For secure production operation:

1. **Always set an API key** (minimum 16 chars, random, unique)
2. Do **not** expose the service directly to the public internet; place it behind VPN and/or TLS reverse proxy
3. Keep `ANKERCTL_DEV_MODE` **disabled** in production
4. Update regularly to current releases/commits
5. Manage secrets via secure environment variables or a secret store
6. Monitor logs, but never share plaintext secrets
7. Run Docker with least privilege wherever practical (read-only/rootless where feasible)

## Safe Harbor

Good-faith security research is welcome. If you:
- report responsibly,
- avoid data exfiltration or destruction,
- respect privacy and service availability,

your research is considered aligned with this policy.

---

Thank you for helping improve the security of ankerctl.
