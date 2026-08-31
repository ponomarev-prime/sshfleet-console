package credential

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

const Service = "sshfleet"

type SecretService struct{ Binary string }

func (s SecretService) binary() string {
	if strings.TrimSpace(s.Binary) == "" {
		return "secret-tool"
	}
	return s.Binary
}

func (s SecretService) Lookup(ctx context.Context, key string) ([]byte, error) {
	if err := supportedSecretService(platform.Current()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("credential key is empty")
	}
	cmd := exec.CommandContext(ctx, s.binary(), "lookup", "service", Service, "key", key)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	secret, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Secret Service lookup failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	secret = bytes.TrimSuffix(secret, []byte("\n"))
	if len(secret) == 0 {
		return nil, fmt.Errorf("Secret Service has no value for key %q", key)
	}
	return secret, nil
}

func (s SecretService) Set(ctx context.Context, key string, secret []byte) error {
	if err := supportedSecretService(platform.Current()); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("credential key is empty")
	}
	if len(secret) == 0 {
		return fmt.Errorf("secret is empty")
	}
	cmd := exec.CommandContext(ctx, s.binary(), "store", "--label=SSH Fleet Console: "+key, "service", Service, "key", key)
	cmd.Stdin = bytes.NewReader(secret)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Secret Service store failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func supportedSecretService(capabilities platform.Capabilities) error {
	if capabilities.CredentialStoreImplemented && capabilities.CredentialStore == "secret-service" {
		return nil
	}
	return fmt.Errorf("Secret Service credential provider is unavailable on %s; native %s adapter is not implemented", capabilities.Platform(), capabilities.CredentialStore)
}
