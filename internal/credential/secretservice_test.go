package credential

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

func TestSecretServiceUsesPipeNotSecretArgv(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "secret-tool")
	logPath := filepath.Join(dir, "log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SSHF_TEST_LOG"
if [ "$1" = lookup ]; then printf 'from-wallet\n'; else IFS= read -r secret; printf 'stdin=%s\n' "$secret" >> "$SSHF_TEST_LOG"; fi
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSHF_TEST_LOG", logPath)
	store := SecretService{Binary: binary}
	if err := store.Set(context.Background(), "sshfleet/lab/demo", []byte("top-secret")); err != nil {
		t.Fatal(err)
	}
	secret, err := store.Lookup(context.Background(), "sshfleet/lab/demo")
	if err != nil {
		t.Fatal(err)
	}
	if string(secret) != "from-wallet" {
		t.Fatalf("secret = %q", secret)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if strings.Contains(lines[0], "top-secret") {
		t.Fatalf("secret leaked to argv: %q", lines[0])
	}
	if !strings.Contains(string(logData), "stdin=top-secret") {
		t.Fatalf("store did not receive secret via stdin: %s", logData)
	}
}

func TestSecretServiceFailsClosedWhenNativeStoreIsUnavailable(t *testing.T) {
	err := supportedSecretService(platform.Capabilities{OS: "windows", Arch: "amd64", CredentialStore: "credential-manager"})
	if err == nil || !strings.Contains(err.Error(), "native credential-manager adapter is not implemented") {
		t.Fatalf("unsupported store error = %v", err)
	}
}
