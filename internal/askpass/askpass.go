// Package askpass implements the password-only Secret Service bridge used by
// OpenSSH. It can run inside a second sshfleet process, so the application remains
// distributable as one binary while secrets still travel only over pipes.
package askpass

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ponomarev-prime/sshfleet-console/internal/credential"
)

const (
	ModeEnv     = "SSHF_INTERNAL_ASKPASS"
	ProviderEnv = "SSHF_CREDENTIAL_PROVIDER"
	KeyEnv      = "SSHF_CREDENTIAL_KEY"
)

func Run(ctx context.Context, output io.Writer, getenv func(string) string) error {
	if strings.EqualFold(getenv("SSH_ASKPASS_PROMPT"), "confirm") {
		return fmt.Errorf("confirmation prompts are forbidden")
	}
	if provider := getenv(ProviderEnv); provider != "secret-service" {
		return fmt.Errorf("unsupported credential provider")
	}
	secret, err := (credential.SecretService{}).Lookup(ctx, getenv(KeyEnv))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(secret))
	return err
}
