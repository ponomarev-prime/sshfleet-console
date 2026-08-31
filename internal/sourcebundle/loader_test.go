package sourcebundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/config"
)

var fixtureNow = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

type cryptoFixture struct {
	dir            string
	identity       []byte
	identityPath   string
	allowedSigners string
	signingKey     string
	recipient      string
}

func TestLocalEncryptedInventoryUsesSecretPipeAndRejectsTampering(t *testing.T) {
	fixture := newCryptoFixture(t, "secure-lab")
	bundleDir := filepath.Join(fixture.dir, "bundle")
	plaintext := []byte("version = 1\n[[hosts]]\nalias = \"encrypted-01\"\nhostname = \"192.0.2.40\"\n")
	inventoryPath := filepath.Join(fixture.dir, "inventory.toml")
	if err := os.WriteFile(inventoryPath, plaintext, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Pack(context.Background(), PackOptions{
		SourceID: "secure-lab", Revision: 2, InventoryPath: inventoryPath,
		OutputDir: bundleDir, Recipient: fixture.recipient, SigningKey: fixture.signingKey,
		CreatedAt: fixtureNow.Add(-time.Hour), ExpiresAt: fixtureNow.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	assertTreeDoesNotContain(t, bundleDir, "encrypted-01")

	lookups := 0
	loader := Loader{
		StateDir: filepath.Join(fixture.dir, "state"),
		Now:      func() time.Time { return fixtureNow },
		LookupSecret: func(_ context.Context, key string) ([]byte, error) {
			lookups++
			if key != "sshfleet/age/lab" {
				t.Fatalf("identity key = %q", key)
			}
			return append([]byte(nil), fixture.identity...), nil
		},
	}
	source := config.Source{
		Name:           "secure-lab",
		Kind:           config.SourceEncryptedInventory,
		Path:           bundleDir,
		SigningKey:     fixture.allowedSigners,
		AgeIdentityRef: "secret-service:sshfleet/age/lab",
	}
	inv, err := loader.Load(context.Background(), source, "")
	if err != nil {
		t.Fatal(err)
	}
	if lookups != 1 || len(inv.Hosts) != 1 || inv.Hosts[0].Alias != "encrypted-01" {
		t.Fatalf("lookups/inventory = %d/%#v", lookups, inv)
	}
	assertTreeDoesNotContain(t, loader.StateDir, "encrypted-01")

	payloadPath := filepath.Join(bundleDir, payloadName)
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 0xff
	if err := os.WriteFile(payloadPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(context.Background(), source, ""); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestEncryptedInventoryRejectsRollbackAndExpiredManifest(t *testing.T) {
	fixture := newCryptoFixture(t, "secure-lab")
	bundleDir := filepath.Join(fixture.dir, "bundle")
	plaintext := []byte("version = 1\n[[hosts]]\nalias = \"secure-01\"\n")
	loader := Loader{StateDir: filepath.Join(fixture.dir, "state"), Now: func() time.Time { return fixtureNow }}
	source := config.Source{Name: "secure-lab", Kind: config.SourceEncryptedInventory, Path: bundleDir, SigningKey: fixture.allowedSigners, AgeIdentityRef: "age-plugin:" + fixture.identityPath}

	fixture.writeBundle(t, bundleDir, source.Name, 5, plaintext)
	if _, err := loader.Load(context.Background(), source, ""); err != nil {
		t.Fatal(err)
	}
	fixture.writeBundle(t, bundleDir, source.Name, 4, plaintext)
	if _, err := loader.Load(context.Background(), source, ""); err == nil || !strings.Contains(err.Error(), "rolls back") {
		t.Fatalf("rollback error = %v", err)
	}

	fixture.writeBundleAt(t, bundleDir, source.Name, 6, plaintext, fixtureNow.Add(-2*time.Hour), fixtureNow.Add(-time.Hour))
	if _, err := loader.Load(context.Background(), source, ""); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expiry error = %v", err)
	}
}

func TestRemoteEncryptedInventoryUsesTLSBearerAndEncryptedCache(t *testing.T) {
	fixture := newCryptoFixture(t, "remote-lab")
	bundleDir := filepath.Join(fixture.dir, "bundle")
	plaintext := []byte("version = 1\n[[hosts]]\nalias = \"remote-01\"\nhostname = \"192.0.2.50\"\n")
	fixture.writeBundle(t, bundleDir, "remote-lab", 7, plaintext)

	requested := make(map[string]int)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-remote-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		requested[r.URL.Path]++
		name := filepath.Base(r.URL.Path)
		if name == manifestName {
			w.Header().Set("Content-Type", "application/toml")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		http.ServeFile(w, r, filepath.Join(bundleDir, name))
	}))
	defer server.Close()

	lookupKeys := make(map[string]int)
	loader := Loader{
		HTTPClient: server.Client(),
		StateDir:   filepath.Join(fixture.dir, "remote-state"),
		Now:        func() time.Time { return fixtureNow },
		LookupSecret: func(_ context.Context, key string) ([]byte, error) {
			lookupKeys[key]++
			switch key {
			case "sshfleet/http/remote":
				return []byte("test-remote-token"), nil
			case "sshfleet/age/remote":
				return append([]byte(nil), fixture.identity...), nil
			default:
				return nil, fmt.Errorf("unexpected secret key %q", key)
			}
		},
	}
	source := config.Source{
		Name:           "remote-lab",
		Kind:           config.SourceRemote,
		URL:            server.URL + "/fleet/",
		SigningKey:     fixture.allowedSigners,
		AgeIdentityRef: "secret-service:sshfleet/age/remote",
	}
	inv, err := loader.Load(context.Background(), source, "sshfleet/http/remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Hosts) != 1 || inv.Hosts[0].Alias != "remote-01" {
		t.Fatalf("inventory = %#v", inv)
	}
	for _, name := range []string{manifestName, signatureName, payloadName} {
		if requested["/fleet/"+name] != 1 {
			t.Fatalf("requests = %#v", requested)
		}
	}
	if lookupKeys["sshfleet/http/remote"] != 1 || lookupKeys["sshfleet/age/remote"] != 1 {
		t.Fatalf("secret lookups = %#v", lookupKeys)
	}
	assertTreeDoesNotContain(t, loader.StateDir, "remote-01")
}

func TestRemoteSourceRejectsRedirectAndOversize(t *testing.T) {
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.invalid/stolen", http.StatusFound)
	}))
	defer redirect.Close()
	loader := Loader{HTTPClient: redirect.Client(), MaxBytes: 8, StateDir: t.TempDir()}
	_, err := loader.Load(context.Background(), config.Source{Name: "remote", Kind: config.SourceRemote, URL: redirect.URL + "/", SigningKey: "/missing", AgeIdentityRef: "secret-service:test"}, "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirect error = %v", err)
	}

	oversize := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/toml")
		w.Header().Set("Content-Length", fmt.Sprint(manifestLimit+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer oversize.Close()
	loader.HTTPClient = oversize.Client()
	_, err = loader.Load(context.Background(), config.Source{Name: "remote", Kind: config.SourceRemote, URL: oversize.URL + "/", SigningKey: "/missing", AgeIdentityRef: "secret-service:test"}, "")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize error = %v", err)
	}
}

func newCryptoFixture(t *testing.T, sourceID string) cryptoFixture {
	t.Helper()
	for _, binary := range []string{"age", "age-keygen", "ssh-keygen"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is required for encrypted source integration tests", binary)
		}
	}
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "age-identity.txt")
	run(t, nil, "age-keygen", "-o", identityPath)
	if err := os.Chmod(identityPath, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	recipient := ""
	for _, line := range strings.Split(string(identity), "\n") {
		if value, ok := strings.CutPrefix(line, "# public key: "); ok {
			recipient = strings.TrimSpace(value)
		}
	}
	if recipient == "" {
		t.Fatalf("age identity has no public key: %s", identity)
	}
	signingKey := filepath.Join(dir, "signing")
	run(t, nil, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", signingKey)
	publicKey, err := os.ReadFile(signingKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowedSigners := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(allowedSigners, []byte(sourceID+" "+strings.TrimSpace(string(publicKey))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cryptoFixture{dir: dir, identity: identity, identityPath: identityPath, allowedSigners: allowedSigners, signingKey: signingKey, recipient: recipient}
}

func (f cryptoFixture) writeBundle(t *testing.T, dir, sourceID string, revision uint64, plaintext []byte) {
	t.Helper()
	f.writeBundleAt(t, dir, sourceID, revision, plaintext, fixtureNow.Add(-time.Hour), fixtureNow.Add(time.Hour))
}

func (f cryptoFixture) writeBundleAt(t *testing.T, dir, sourceID string, revision uint64, plaintext []byte, created, expires time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := run(t, plaintext, "age", "-r", f.recipient)
	digest := sha256.Sum256(payload)
	manifest, err := toml.Marshal(Manifest{Version: 1, SourceID: sourceID, Revision: revision, CreatedAt: created, ExpiresAt: expires, CiphertextSHA256: hex.EncodeToString(digest[:])})
	if err != nil {
		t.Fatal(err)
	}
	signature := run(t, manifest, "ssh-keygen", "-Y", "sign", "-f", f.signingKey, "-n", namespace)
	for name, data := range map[string][]byte{manifestName: manifest, signatureName: signature, payloadName: payload} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func run(t *testing.T, input []byte, binary string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", binary, args, err, stderr.String())
	}
	return output
}

func assertTreeDoesNotContain(t *testing.T, root, secret string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), secret) {
				t.Fatalf("plaintext %q leaked into %s", secret, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
