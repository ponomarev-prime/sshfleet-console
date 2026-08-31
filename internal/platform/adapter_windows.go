//go:build windows

package platform

func nativeCapabilities() Capabilities {
	return Capabilities{
		PTYBackend:          "conpty",
		CredentialStore:     "credential-manager",
		LocalProbeAvailable: false,
	}
}
