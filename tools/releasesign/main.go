// Command releasesign manages the ed25519 key that signs vmrc releases.
//
//	keygen -pub <path> [--force-tty]
//	    Generate a keypair. Writes the PUBLIC key to <path> (commit it, e.g.
//	    internal/selfupdate/keys/release.pub) and prints the PRIVATE key (base64
//	    32-byte seed) to stdout only, so it can be piped straight into a secret
//	    store and never touches disk:
//
//	        go run ./tools/releasesign keygen -pub internal/selfupdate/keys/release.pub \
//	            | gh secret set VMRC_RELEASE_SIGNING_KEY
//
//	    Refuses to print the private key to a terminal unless --force-tty is set,
//	    and refuses to overwrite an existing <path>.
//
//	sign -in <checksums.txt> -out <checksums.txt.sig>
//	    Sign <in> with the private key from $VMRC_RELEASE_SIGNING_KEY. Fails if
//	    the key's public half is not one of the keys embedded in the vmrc
//	    binaries, so a secret/committed-key mismatch breaks the release instead
//	    of shipping releases nobody can update to. Run by goreleaser.
//
//	verify -in <checksums.txt> -sig <checksums.txt.sig>
//	    Check a signature against the embedded keys (manual release audits).
//
// Stdlib only; the signature format lives in internal/selfupdate.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pddstudio/vmrc/internal/selfupdate"
)

const usage = `usage:
  releasesign keygen -pub <public-key-path> [--force-tty]
  releasesign sign   -in <checksums.txt> -out <checksums.txt.sig>
  releasesign verify -in <checksums.txt> -sig <checksums.txt.sig>
`

// env carries the process interfaces so tests can inject them.
type env struct {
	stdout      io.Writer
	stderr      io.Writer
	getenv      func(string) string
	stdoutIsTTY bool
	trusted     func() ([]ed25519.PublicKey, error)
}

func main() {
	e := &env{
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		getenv:      os.Getenv,
		stdoutIsTTY: isTerminal(os.Stdout),
		trusted:     selfupdate.EmbeddedKeys,
	}
	os.Exit(run(e, os.Args[1:]))
}

// isTerminal reports whether f is a character device (an interactive terminal).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func run(e *env, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(e.stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "keygen":
		err = keygen(e, args[1:])
	case "sign":
		err = sign(e, args[1:])
	case "verify":
		err = verify(e, args[1:])
	default:
		_, _ = fmt.Fprintf(e.stderr, "unknown command %q\n%s", args[0], usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "releasesign %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func newFlagSet(e *env, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	return fs
}

func keygen(e *env, args []string) error {
	fs := newFlagSet(e, "keygen")
	pubPath := fs.String("pub", "", "path to write the PUBLIC key to (e.g. internal/selfupdate/keys/release.pub)")
	forceTTY := fs.Bool("force-tty", false, "print the private key even when stdout is a terminal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pubPath == "" {
		return fmt.Errorf("-pub is required")
	}
	// Decide everything that can fail before generating or writing anything.
	if e.stdoutIsTTY && !*forceTTY {
		return fmt.Errorf("refusing to print the private key to a terminal; pipe it into a secret store, e.g.\n" +
			"  go run ./tools/releasesign keygen -pub internal/selfupdate/keys/release.pub | gh secret set " + selfupdate.SigningKeyEnv + "\n" +
			"(or pass --force-tty if you really want it on screen)")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*pubPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(*pubPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; refusing to overwrite a signing key (use a new file name to rotate)", *pubPath)
		}
		return err
	}
	if _, err := f.Write(selfupdate.MarshalPublicKey(pub)); err != nil {
		_ = f.Close()
		_ = os.Remove(*pubPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(*pubPath)
		return err
	}

	seed := base64.StdEncoding.EncodeToString(priv.Seed())
	if e.stdoutIsTTY {
		seed += "\n"
	}
	if _, err := io.WriteString(e.stdout, seed); err != nil {
		return fmt.Errorf("writing private key to stdout: %w (the public key at %s is now orphaned; delete it and retry)", err, *pubPath)
	}
	_, _ = fmt.Fprintf(e.stderr, "wrote public key %s (key id %s); private key printed to stdout only.\n"+
		"Commit the public key; store the private key as the %s release secret.\n",
		*pubPath, selfupdate.KeyID(pub), selfupdate.SigningKeyEnv)
	return nil
}

func sign(e *env, args []string) error {
	fs := newFlagSet(e, "sign")
	in := fs.String("in", "", "file to sign (checksums.txt)")
	out := fs.String("out", "", "signature file to write (checksums.txt.sig)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *out == "" {
		return fmt.Errorf("-in and -out are required")
	}
	raw := e.getenv(selfupdate.SigningKeyEnv)
	if raw == "" {
		return fmt.Errorf("$%s is not set; it must hold the base64 release signing key (see CONTRIBUTING.md, Release signing key)", selfupdate.SigningKeyEnv)
	}
	priv, err := selfupdate.ParsePrivateKey(raw)
	if err != nil {
		return fmt.Errorf("$%s is invalid: %w", selfupdate.SigningKeyEnv, err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	trusted, err := e.trusted()
	if err != nil {
		return fmt.Errorf("loading embedded keys: %w", err)
	}
	found := false
	for _, k := range trusted {
		if k.Equal(pub) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("the key in $%s (key id %s) is not one of the embedded release keys in internal/selfupdate/keys/*.pub; "+
			"binaries could not verify this release", selfupdate.SigningKeyEnv, selfupdate.KeyID(pub))
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, selfupdate.SignChecksums(priv, data), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(e.stderr, "signed %s -> %s (key id %s)\n", *in, *out, selfupdate.KeyID(pub))
	return nil
}

func verify(e *env, args []string) error {
	fs := newFlagSet(e, "verify")
	in := fs.String("in", "", "signed file (checksums.txt)")
	sigPath := fs.String("sig", "", "signature file (checksums.txt.sig)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *sigPath == "" {
		return fmt.Errorf("-in and -sig are required")
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(*sigPath)
	if err != nil {
		return err
	}
	trusted, err := e.trusted()
	if err != nil {
		return fmt.Errorf("loading embedded keys: %w", err)
	}
	if err := selfupdate.VerifyChecksumsSignature(data, sig, trusted); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(e.stdout, "OK: %s is signed by a trusted release key\n", *in)
	return nil
}
