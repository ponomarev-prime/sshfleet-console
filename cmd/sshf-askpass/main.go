// sshf-askpass is retained for compatibility. Current sshfleet binaries can
// execute the same bridge internally when OpenSSH starts a second process.
package main

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/ponomarev-prime/sshfleet-console/internal/askpass"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run(ctx, os.Stdout, os.Getenv); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, output io.Writer, getenv func(string) string) error {
	return askpass.Run(ctx, output, getenv)
}
