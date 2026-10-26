# Release signing public keys

Every `*.pub` file in this directory is compiled into both vmrc binaries (`//go:embed`) and
trusted to sign `checksums.txt` of a GitHub release. Self-update refuses any release whose
`checksums.txt.sig` doesn't verify against one of these keys.

**No `*.pub` file here means self-update is disabled.** It fails closed with "this build has no
release signing key embedded". To create the key, see CONTRIBUTING.md, section "Release signing
key".

- Only **public** keys go here. Never commit a private key.
- Use one key per file. Keep the old file while you rotate (see CONTRIBUTING.md).
