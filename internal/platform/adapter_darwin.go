//go:build darwin

package platform

func nativeCapabilities() Capabilities {
	return Capabilities{
		PTYBackend:                "unix-pty",
		EmbeddedTerminalAvailable: true,
		CredentialStore:           "keychain",
		LocalProbeAvailable:       false,
	}
}
