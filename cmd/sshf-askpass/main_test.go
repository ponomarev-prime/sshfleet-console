package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunReturnsSecretAndRefusesConfirmation(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "secret-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'wallet-password\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	values := map[string]string{"SSHF_CREDENTIAL_PROVIDER": "secret-service", "SSHF_CREDENTIAL_KEY": "sshfleet/test"}
	getenv := func(key string) string { return values[key] }
	var output bytes.Buffer
	if err := run(context.Background(), &output, getenv); err != nil {
		t.Fatal(err)
	}
	if output.String() != "wallet-password\n" {
		t.Fatalf("output = %q", output.String())
	}
	values["SSH_ASKPASS_PROMPT"] = "confirm"
	output.Reset()
	if err := run(context.Background(), &output, getenv); err == nil {
		t.Fatal("expected confirmation refusal")
	}
	if output.Len() != 0 {
		t.Fatalf("confirmation wrote %q", output.String())
	}
}
