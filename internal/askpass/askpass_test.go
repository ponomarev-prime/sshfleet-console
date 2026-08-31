package askpass

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunUsesSecretServicePipeAndRefusesConfirmation(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "secret-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'wallet-value\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	values := map[string]string{ProviderEnv: "secret-service", KeyEnv: "sshfleet/test"}
	getenv := func(key string) string { return values[key] }
	var output bytes.Buffer
	if err := Run(context.Background(), &output, getenv); err != nil {
		t.Fatal(err)
	}
	if output.String() != "wallet-value\n" {
		t.Fatalf("output = %q", output.String())
	}
	values["SSH_ASKPASS_PROMPT"] = "confirm"
	output.Reset()
	if err := Run(context.Background(), &output, getenv); err == nil {
		t.Fatal("confirmation prompt accepted")
	}
	if output.Len() != 0 {
		t.Fatalf("confirmation wrote output: %q", output.String())
	}
}
