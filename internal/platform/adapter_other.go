//go:build !linux && !darwin && !windows

package platform

func nativeCapabilities() Capabilities {
	return Capabilities{
		PTYBackend:          "native-pty",
		CredentialStore:     "native-store",
		LocalProbeAvailable: false,
	}
}
