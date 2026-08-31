//go:build linux

package platform

func nativeCapabilities() Capabilities {
	return Capabilities{
		PTYBackend:                 "unix-pty",
		EmbeddedTerminalAvailable:  true,
		CredentialStore:            "secret-service",
		CredentialStoreImplemented: true,
		LocalProbeAvailable:        true,
	}
}
