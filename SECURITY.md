# Security policy

Strata sits in front of files people care about, so security reports get
priority over everything else.

## Reporting a vulnerability

Please report privately through GitHub: open the repository's **Security**
tab and choose **Report a vulnerability**. Don't open a public issue for
anything that could put someone's files at risk.

Include what you found, how to reproduce it, and which version or commit you
tested. You'll get an acknowledgement within a few days, and a fix or a plan
for one as soon as the problem is understood. Reporters are credited in the
release notes unless they'd rather not be.

## Supported versions

Only the latest release gets fixes. Images are published to
`ghcr.io/ah410/strata` and signed with cosign; check one with:

```bash
cosign verify ghcr.io/ah410/strata:1.0.0 \
  --certificate-identity-regexp '^https://github.com/ah410/strata/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## What's in scope

Anything that lets someone without the passphrase read, change or delete
files; escapes from the served directory (path traversal, symlinks, the
`.strata` internal folder); script execution through uploaded files or
Markdown; request forgery; bypassing the sign-in throttle; and denial of
service from a single small request (image bombs, zip bombs, unbounded
walks).

Out of scope: attacks that need the passphrase or the container's logs (the
first-run setup code is printed there by design), and deployments with
`STRATA_AUTH=none` on an untrusted network.

## How Strata defends itself

All file access goes through Go's `os.Root`, so no request path or symlink
can leave the served directory. The image is distroless, runs as non-root,
and works with a read-only root filesystem and all capabilities dropped.
Uploaded files are served with a sandboxing Content-Security-Policy, and
anything a browser would execute is served as plain text. State-changing
requests need a custom header and a same-origin fetch, and the `Host` header
is checked against an allowlist to stop DNS rebinding. The passphrase is
hashed with argon2id, and sign-in attempts are throttled per client and
globally. CI runs CodeQL, govulncheck, gitleaks and Snyk, and every third-party
action is pinned to a commit.
