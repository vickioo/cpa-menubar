# Security Policy

## Supported Version

Only the latest commit on the default branch receives security updates.

## Reporting a Vulnerability

Use GitHub's private vulnerability reporting feature when it is enabled for the repository. Do not publish credentials, auth files, provider account data, or reproducible secret material in a public issue.

## Deployment Checklist

- Replace all example hostnames, paths, and placeholder values.
- Keep the bridge bound to `127.0.0.1` unless you have an equivalent network boundary.
- Put the public endpoint behind HTTPS and require the desktop bearer token.
- Store secret files outside the repository with mode `0600`.
- Restrict the bridge service to the minimum auth directory it must update.
- Review nginx and systemd configuration before enabling the service.
- Never commit CLIProxyAPI auth JSON, OAuth tokens, xAI management keys, router keys, or desktop tokens.
