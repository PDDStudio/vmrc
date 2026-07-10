# Security Policy

`vmrc` drives remote machines: the agent exposes screenshot, file read/write,
and command-execution endpoints. Because of that, security reports are taken
seriously and the threat model below is part of the contract, not an
afterthought.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately through GitHub's
[private vulnerability reporting](https://github.com/PDDStudio/vmrc/security/advisories/new)
("Report a vulnerability" under the repository's **Security** tab). Include:

- affected version / commit,
- a description and, ideally, a minimal reproduction,
- the impact you believe it has.

You can expect an initial acknowledgement within a few days. Once a fix is
ready, a patched release is cut and the advisory is published with credit to
the reporter (unless you prefer to stay anonymous).

## Supported versions

Only the latest released version receives security fixes. There are no
long-term support branches; upgrade to the newest release (the built-in
`update` command makes this a one-liner).

## Security model (what the tool assumes)

Understanding these assumptions helps decide whether a report is a bug or
expected behavior:

- **The tailnet is the only ingress.** The agent binds to a Tailscale
  (`tsnet`) listener, never to a public `0.0.0.0` address. Reachability is
  gated by your Tailscale ACLs.
- **Requests are authorized by Tailscale identity** (`WhoIs` login / ACL tag
  allowlists), with an optional shared bearer token as a fallback. Anyone your
  ACLs let reach the agent, and who matches the allowlist, can run commands on
  that machine — that is by design.
- **Secrets are supplied at runtime** via environment variables (Tailscale auth
  key, shared token) and are never written to the config file or baked into the
  binary.
- **File access is bounded** to `base_dir` and, for reads, to a configured glob
  allowlist; path-traversal escapes are treated as bugs.

A report that demonstrates a way to bypass any of the above — unauthenticated
access, escaping `base_dir`, leaking a secret, or reaching the agent off-tailnet
— is in scope and worth reporting.
