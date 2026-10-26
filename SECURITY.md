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
- **File access is bounded** to `base_dir` (uploads to `uploads_dir`) and, for reads, to a configured glob
  allowlist; path-traversal escapes are treated as bugs.

- **Self-updates are signature-verified.** See [Release signing](#release-signing).

## Release signing

Releases are signed so that a compromised GitHub release, token, or account can't push
a malicious build through `update`:

- The release workflow signs `checksums.txt` with an **ed25519** key and publishes the
  detached signature as `checksums.txt.sig`. The private key exists only as the
  `VMRC_RELEASE_SIGNING_KEY` CI secret. It is never committed and never written to disk
  by the tooling.
- Both binaries embed the trusted **public** keys from
  [`internal/selfupdate/keys/`](./internal/selfupdate/keys/). `vmrc update --self`,
  `vmrc-agent update` and the agent's remote `/update` endpoint all verify the signature
  over the exact `checksums.txt` bytes *before* trusting any checksum in it. Only then is
  the archive's SHA-256 checked and the binary replaced.
- Verification **fails closed**. Each of these cases refuses the update and leaves the
  installed binary untouched: a missing `.sig` asset (so stripping the signature doesn't
  work), a malformed signature, a signature by an untrusted key, or a build with no
  embedded key. There is no override flag.
- `checksums.txt.sig` has two lines: `vmrc-signature-v1 ed25519 <key-id>`, then the
  base64 of the raw 64-byte signature. `<key-id>` is the first 8 bytes of SHA-256 of the
  public key, in hex. Every embedded key is trusted, so the maintainer can rotate keys
  without breaking existing installs (see CONTRIBUTING.md, "Release signing key").
- To check a downloaded release by hand from a clone:
  `go run ./tools/releasesign verify -in checksums.txt -sig checksums.txt.sig`.
- Known gap: the one-line install scripts (`scripts/install.sh`, `install.ps1`) still
  only check SHA-256, so the *first* install trusts the release. Every update after
  that is signature-verified.

A report that shows a way to make a binary accept an update not signed by an embedded
key is in scope.

## Reporting scope

A report that demonstrates a way to bypass any of the above — unauthenticated
access, escaping `base_dir`, leaking a secret, or reaching the agent off-tailnet
— is in scope and worth reporting.
