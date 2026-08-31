// Package sourcebundle loads authenticated encrypted restricted inventories.
package sourcebundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/credential"
	"github.com/ponomarev-prime/sshfleet-console/internal/platform"
)

const (
	manifestName   = "manifest.toml"
	signatureName  = "manifest.sig"
	payloadName    = "inventory.toml.age"
	namespace      = "sshfleet-inventory-v1"
	manifestLimit  = 64 << 10
	signatureLimit = 64 << 10
)

type Manifest struct {
	Version          int       `toml:"version"`
	SourceID         string    `toml:"source_id"`
	Revision         uint64    `toml:"revision"`
	CreatedAt        time.Time `toml:"created_at"`
	ExpiresAt        time.Time `toml:"expires_at"`
	CiphertextSHA256 string    `toml:"ciphertext_sha256"`
}

type Loader struct {
	HTTPClient      *http.Client
	LookupSecret    func(context.Context, string) ([]byte, error)
	AgeBinary       string
	SSHKeygenBinary string
	StateDir        string
	MaxBytes        int64
	Now             func() time.Time
}

type PackOptions struct {
	SourceID        string
	Revision        uint64
	InventoryPath   string
	OutputDir       string
	Recipient       string
	SigningKey      string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	AgeBinary       string
	SSHKeygenBinary string
	MaxBytes        int64
}

type bundle struct {
	manifest  []byte
	signature []byte
	payload   []byte
	origin    string
	remote    bool
}

// Pack validates and encrypts a restricted inventory, then signs its manifest.
// The output directory never receives a plaintext inventory.
func Pack(ctx context.Context, options PackOptions) error {
	if strings.TrimSpace(options.SourceID) == "" || options.Revision == 0 {
		return fmt.Errorf("source ID and positive revision are required")
	}
	if options.CreatedAt.IsZero() {
		options.CreatedAt = time.Now().UTC()
	}
	if options.ExpiresAt.IsZero() || !options.ExpiresAt.After(options.CreatedAt) {
		return fmt.Errorf("bundle expiry must be after creation time")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 4 << 20
	}
	if options.AgeBinary == "" {
		options.AgeBinary = "age"
	}
	if options.SSHKeygenBinary == "" {
		options.SSHKeygenBinary = "ssh-keygen"
	}
	inventoryPath, err := config.ExpandPath(options.InventoryPath)
	if err != nil {
		return err
	}
	if _, err := config.LoadInventory(inventoryPath); err != nil {
		return err
	}
	plaintext, err := readBoundedFile(inventoryPath, options.MaxBytes)
	if err != nil {
		return err
	}
	defer clear(plaintext)
	age := exec.CommandContext(ctx, options.AgeBinary, "-r", strings.TrimSpace(options.Recipient))
	age.Stdin = bytes.NewReader(plaintext)
	ciphertext := limitedBuffer{limit: options.MaxBytes}
	ageErrors := limitedBuffer{limit: manifestLimit}
	age.Stdout, age.Stderr = &ciphertext, &ageErrors
	if err := age.Run(); err != nil {
		return fmt.Errorf("encrypt inventory with age: %w: %s", err, strings.TrimSpace(ageErrors.String()))
	}
	digest := sha256.Sum256(ciphertext.Bytes())
	manifest, err := toml.Marshal(Manifest{
		Version:          1,
		SourceID:         options.SourceID,
		Revision:         options.Revision,
		CreatedAt:        options.CreatedAt.UTC(),
		ExpiresAt:        options.ExpiresAt.UTC(),
		CiphertextSHA256: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return err
	}
	signingKey, err := config.ExpandPath(options.SigningKey)
	if err != nil {
		return err
	}
	info, err := os.Lstat(signingKey)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("signing key must be a regular file: %s", signingKey)
	}
	sign := exec.CommandContext(ctx, options.SSHKeygenBinary, "-Y", "sign", "-f", signingKey, "-n", namespace)
	sign.Stdin = bytes.NewReader(manifest)
	signature := limitedBuffer{limit: signatureLimit}
	signErrors := limitedBuffer{limit: manifestLimit}
	sign.Stdout, sign.Stderr = &signature, &signErrors
	if err := sign.Run(); err != nil {
		return fmt.Errorf("sign inventory manifest: %w: %s", err, strings.TrimSpace(signErrors.String()))
	}
	outputDir, err := config.ExpandPath(options.OutputDir)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(outputDir); err == nil {
		return fmt.Errorf("bundle output already exists: %s", outputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".sshfleet-bundle-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return err
	}
	for name, data := range map[string][]byte{manifestName: manifest, signatureName: signature.Bytes(), payloadName: ciphertext.Bytes()} {
		if err := atomicWrite(filepath.Join(staging, name), data, 0o600); err != nil {
			return err
		}
	}
	return os.Rename(staging, outputDir)
}

func (l Loader) Load(ctx context.Context, source config.Source, authKey string) (config.Inventory, error) {
	l = l.defaults()
	var raw bundle
	var err error
	switch source.Kind {
	case config.SourceEncryptedInventory:
		raw, err = l.readLocal(source.Path)
	case config.SourceRemote:
		raw, err = l.readRemote(ctx, source.URL, authKey)
	default:
		return config.Inventory{}, fmt.Errorf("source %q is not an encrypted inventory", source.Name)
	}
	if err != nil {
		return config.Inventory{}, err
	}
	manifest, err := parseManifest(raw.manifest, source.Name, l.Now())
	if err != nil {
		return config.Inventory{}, err
	}
	if err := verifyHash(manifest, raw.payload); err != nil {
		return config.Inventory{}, err
	}
	if err := l.verifySignature(ctx, source.SigningKey, manifest.SourceID, raw.manifest, raw.signature); err != nil {
		return config.Inventory{}, err
	}
	if err := l.checkRevision(source.Name, raw.origin, manifest); err != nil {
		return config.Inventory{}, err
	}
	plaintext, err := l.decrypt(ctx, source.AgeIdentityRef, raw.payload)
	if err != nil {
		return config.Inventory{}, err
	}
	defer clear(plaintext)
	inv, err := config.ParseInventory(plaintext, source.Name+":decrypted")
	if err != nil {
		return config.Inventory{}, err
	}
	if raw.remote {
		if err := l.storeEncryptedCache(source.Name, manifest, raw); err != nil {
			return config.Inventory{}, err
		}
	}
	if err := l.storeRevision(source.Name, raw.origin, manifest); err != nil {
		return config.Inventory{}, err
	}
	return inv, nil
}

func (l Loader) defaults() Loader {
	if l.HTTPClient == nil {
		l.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if l.LookupSecret == nil {
		l.LookupSecret = (credential.SecretService{}).Lookup
	}
	if l.AgeBinary == "" {
		l.AgeBinary = "age"
	}
	if l.SSHKeygenBinary == "" {
		l.SSHKeygenBinary = "ssh-keygen"
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = 4 << 20
	}
	if l.Now == nil {
		l.Now = time.Now
	}
	if l.StateDir == "" {
		if dir, err := platform.UserCacheDir(); err == nil {
			l.StateDir = filepath.Join(dir, "sshfleet", "sources")
		}
	}
	return l
}

func (l Loader) readLocal(path string) (bundle, error) {
	dir, err := config.ExpandPath(path)
	if err != nil {
		return bundle{}, err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return bundle{}, fmt.Errorf("encrypted inventory path must be a directory: %s", dir)
	}
	manifest, err := readBoundedFile(filepath.Join(dir, manifestName), manifestLimit)
	if err != nil {
		return bundle{}, err
	}
	signature, err := readBoundedFile(filepath.Join(dir, signatureName), signatureLimit)
	if err != nil {
		return bundle{}, err
	}
	payload, err := readBoundedFile(filepath.Join(dir, payloadName), l.MaxBytes)
	if err != nil {
		return bundle{}, err
	}
	return bundle{manifest: manifest, signature: signature, payload: payload, origin: dir}, nil
}

func (l Loader) readRemote(ctx context.Context, base, authKey string) (bundle, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return bundle{}, fmt.Errorf("remote source URL must be an HTTPS directory URL without query or fragment")
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	var token []byte
	if authKey != "" {
		token, err = l.LookupSecret(ctx, authKey)
		if err != nil {
			return bundle{}, fmt.Errorf("remote authentication: %w", err)
		}
		defer clear(token)
	}
	fetch := func(name string, limit int64, contentTypes ...string) ([]byte, error) {
		endpoint := parsed.ResolveReference(&url.URL{Path: name})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		if len(token) > 0 {
			req.Header.Set("Authorization", "Bearer "+string(token))
		}
		client := *l.HTTPClient
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", name, err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch %s: HTTP %s", name, response.Status)
		}
		if response.ContentLength > limit {
			return nil, fmt.Errorf("fetch %s: payload exceeds %d bytes", name, limit)
		}
		contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
		if contentType != "" && !contains(contentTypes, contentType) {
			return nil, fmt.Errorf("fetch %s: unexpected content type %q", name, contentType)
		}
		return readBounded(response.Body, limit, "remote "+name)
	}
	manifest, err := fetch(manifestName, manifestLimit, "application/toml", "text/plain", "application/octet-stream")
	if err != nil {
		return bundle{}, err
	}
	signature, err := fetch(signatureName, signatureLimit, "application/octet-stream", "text/plain")
	if err != nil {
		return bundle{}, err
	}
	payload, err := fetch(payloadName, l.MaxBytes, "application/octet-stream")
	if err != nil {
		return bundle{}, err
	}
	return bundle{manifest: manifest, signature: signature, payload: payload, origin: parsed.String(), remote: true}, nil
}

func parseManifest(data []byte, sourceName string, now time.Time) (Manifest, error) {
	var manifest Manifest
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse signed manifest: %w", err)
	}
	if manifest.Version != 1 || manifest.SourceID != sourceName || manifest.Revision == 0 {
		return Manifest{}, fmt.Errorf("manifest identity/version/revision does not match source %q", sourceName)
	}
	if manifest.CreatedAt.IsZero() || manifest.ExpiresAt.IsZero() || !manifest.ExpiresAt.After(manifest.CreatedAt) {
		return Manifest{}, fmt.Errorf("manifest has invalid validity interval")
	}
	if manifest.CreatedAt.After(now.Add(5 * time.Minute)) {
		return Manifest{}, fmt.Errorf("manifest creation time is in the future")
	}
	if !now.Before(manifest.ExpiresAt) {
		return Manifest{}, fmt.Errorf("manifest expired at %s", manifest.ExpiresAt.Format(time.RFC3339))
	}
	if len(manifest.CiphertextSHA256) != sha256.Size*2 {
		return Manifest{}, fmt.Errorf("manifest has invalid ciphertext_sha256")
	}
	if _, err := hex.DecodeString(manifest.CiphertextSHA256); err != nil {
		return Manifest{}, fmt.Errorf("manifest has invalid ciphertext_sha256")
	}
	return manifest, nil
}

func verifyHash(manifest Manifest, ciphertext []byte) error {
	digest := sha256.Sum256(ciphertext)
	if !strings.EqualFold(manifest.CiphertextSHA256, hex.EncodeToString(digest[:])) {
		return fmt.Errorf("encrypted inventory SHA-256 does not match signed manifest")
	}
	return nil
}

func (l Loader) verifySignature(ctx context.Context, allowedSigners, identity string, manifest, signature []byte) error {
	path, err := securePublicFile(allowedSigners)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, l.SSHKeygenBinary, "-Y", "verify", "-f", path, "-I", identity, "-n", namespace, "-s", "/proc/self/fd/3")
	cmd.Stdin = bytes.NewReader(manifest)
	stderr := limitedBuffer{limit: manifestLimit}
	cmd.Stderr = &stderr
	if err := runWithExtraInput(cmd, signature); err != nil {
		return fmt.Errorf("verify SSHSIG: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (l Loader) decrypt(ctx context.Context, ref string, ciphertext []byte) ([]byte, error) {
	args := []string{"--decrypt"}
	var identity []byte
	var err error
	usePipe := false
	switch {
	case strings.HasPrefix(ref, "secret-service:"):
		key := strings.TrimPrefix(ref, "secret-service:")
		if key == "" {
			return nil, fmt.Errorf("empty Secret Service age identity reference")
		}
		identity, err = l.LookupSecret(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("load age identity: %w", err)
		}
		defer clear(identity)
		args = append(args, "-i", "/proc/self/fd/3")
		usePipe = true
	case strings.HasPrefix(ref, "age-plugin:"):
		path, err := secureIdentityFile(strings.TrimPrefix(ref, "age-plugin:"))
		if err != nil {
			return nil, err
		}
		args = append(args, "-i", path)
	default:
		return nil, fmt.Errorf("unsupported age identity reference")
	}
	cmd := exec.CommandContext(ctx, l.AgeBinary, args...)
	cmd.Stdin = bytes.NewReader(ciphertext)
	stdout := limitedBuffer{limit: l.MaxBytes}
	stderr := limitedBuffer{limit: manifestLimit}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if usePipe {
		err = runWithExtraInput(cmd, identity)
	} else {
		err = cmd.Run()
	}
	if err != nil {
		return nil, fmt.Errorf("decrypt age inventory: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func runWithExtraInput(cmd *exec.Cmd, data []byte) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.ExtraFiles = []*os.File{reader}
	if err := cmd.Start(); err != nil {
		reader.Close()
		writer.Close()
		return err
	}
	reader.Close()
	_, writeErr := writer.Write(data)
	closeErr := writer.Close()
	waitErr := cmd.Wait()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return waitErr
}

func (l Loader) checkRevision(name, origin string, manifest Manifest) error {
	previousRevision, previousHash, err := l.readRevision(name, origin)
	if err != nil {
		return err
	}
	if manifest.Revision < previousRevision {
		return fmt.Errorf("manifest revision %d rolls back accepted revision %d", manifest.Revision, previousRevision)
	}
	if manifest.Revision == previousRevision && previousHash != "" && !strings.EqualFold(previousHash, manifest.CiphertextSHA256) {
		return fmt.Errorf("manifest revision %d changed ciphertext", manifest.Revision)
	}
	return nil
}

func (l Loader) readRevision(name, origin string) (uint64, string, error) {
	path := l.statePath(name, origin)
	if path == "" {
		return 0, "", nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("read source revision state: %w", err)
	}
	parts := strings.Fields(string(data))
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid source revision state")
	}
	revision, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid source revision state")
	}
	return revision, parts[1], nil
}

func (l Loader) storeRevision(name, origin string, manifest Manifest) error {
	path := l.statePath(name, origin)
	if path == "" {
		return fmt.Errorf("source_state_dir is unavailable")
	}
	return atomicWrite(path, []byte(fmt.Sprintf("%d %s\n", manifest.Revision, manifest.CiphertextSHA256)), 0o600)
}

func (l Loader) storeEncryptedCache(name string, manifest Manifest, raw bundle) error {
	hash := sha256.Sum256([]byte(name + "\x00" + raw.origin))
	dir := filepath.Join(l.StateDir, fmt.Sprintf("cache-%s-r%d", hex.EncodeToString(hash[:8]), manifest.Revision))
	if _, err := os.Lstat(dir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(l.StateDir, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(l.StateDir, ".source-cache-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return err
	}
	for filename, data := range map[string][]byte{manifestName: raw.manifest, signatureName: raw.signature, payloadName: raw.payload} {
		if err := atomicWrite(filepath.Join(staging, filename), data, 0o600); err != nil {
			return fmt.Errorf("store encrypted source cache: %w", err)
		}
	}
	if err := os.Rename(staging, dir); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("store encrypted source cache: %w", err)
	}
	return nil
}

func (l Loader) statePath(name, origin string) string {
	if l.StateDir == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(name + "\x00" + origin))
	return filepath.Join(l.StateDir, "revision-"+hex.EncodeToString(hash[:16]))
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sshfleet-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func securePublicFile(path string) (string, error) {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(expanded)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o022 != 0 {
		return "", fmt.Errorf("allowed_signers must be a regular file not writable by group/others: %s", expanded)
	}
	return expanded, nil
}

func secureIdentityFile(path string) (string, error) {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(expanded)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("age plugin identity must be a private regular file: %s", expanded)
	}
	return expanded, nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle file %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bundle file is not regular: %s", path)
	}
	return readBounded(file, limit, path)
}

func readBounded(reader io.Reader, limit int64, label string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", label, limit)
	}
	return data, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - int64(b.Len())
	if remaining <= 0 || int64(len(data)) > remaining {
		return 0, fmt.Errorf("output exceeds %d bytes", b.limit)
	}
	return b.Buffer.Write(data)
}
