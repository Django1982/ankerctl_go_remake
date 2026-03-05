# ankerctl Documentation

This directory contains the project documentation for the Go reimplementation of ankerctl.

## Structure

```
docs/
  architecture/     Architecture decisions, package design, dependency graph
  api/              REST API reference, WebSocket protocol, OctoPrint compat
  protocols/        MQTT, PPPP, and HTTP auth protocol documentation
  development/      Developer onboarding, build instructions, testing guide
```

## Quick Links

- [Architecture Overview](architecture/README.md) -- Package layout, dependency rules, service lifecycle
- [API Reference](api/README.md) -- REST endpoints, WebSocket streams, auth rules
- [Protocol Documentation](protocols/README.md) -- MQTT encryption, PPPP UDP, HTTP auth flow
- [Development Guide](development/README.md) -- Getting started, building, testing, contributing

## Related Files

- [`CLAUDE.md`](../CLAUDE.md) -- AI assistant instructions and project context
- [`MIGRATION_PLAN.md`](../MIGRATION_PLAN.md) -- 16-phase migration roadmap from Python to Go
