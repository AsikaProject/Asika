# Security Policy

## Supported Versions

| Version         | Supported  |
| --------------- | ---------- |
| `stable tags`   | ✅         |
| `HF And CVE`    | ✅         |
| `main or DEV`   | ⚠️ [^1]     |
| Historical Tags | ❌ [^2]    |

## Reporting a Vulnerability

If you discover a vulnerability in Asika, please report it using one of the following methods:

1. **GitHub Security Advisories**: Submit a private report using [GitHub Security Advisories](https://github.com/minibp/asika/security/advisories).
2. **Email**: For sensitive vulnerabilities, contact the maintainers through the project's GitHub page.
3. Include the following details in your report:
   - Affected version.
   - Steps to reproduce the issue.
   - Description of the potential impact (e.g., token exposure, privilege escalation).

We aim to respond within 48 hours and resolve the issue as quickly as possible.

## Security Guidelines

To ensure the safe use of Asika:

1. **Use the least privilege principle**:
   - Configure platform tokens (`github`, `gitlab`, `gitea`) with the minimal required permissions.
   - Use a personal access token (PAT) only if strictly necessary.
   - Prefer fine-grained tokens over classic tokens with broad scopes.
2. **Pin your Asika to a specific version or tag** — avoid running `main` or `DEV` builds in production.
3. **Protect your config file** (`/etc/asika_config.toml` or `$ASIKA_CONFIG`) — it contains secrets like JWT keys, platform tokens, and SMTP passwords. Set file permissions to `0600`.
4. **Rotate secrets regularly** — especially `auth.jwt_secret` and platform tokens.
5. **Enable webhook signature verification** — set `events.webhook_secret` to prevent forged webhook payloads.
6. **Restrict admin bot IDs** — configure `telegram.admin_ids`, `feishu.admin_ids`, and `discord.admin_ids` to limit who can execute privileged bot commands.

## Note

[^1]: The `@main` tag and `DEV` versions may include experimental or unstable changes. Use stable tags in production.
[^2]: Due to limitations of git, we cannot make modifications on already-released versions.
