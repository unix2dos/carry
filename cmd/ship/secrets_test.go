package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSavedSecretsProtectLogsAndSourceUploads(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	e.Store.KeychainPut = func(string, []byte) error { return nil }
	e.Store.KeychainGet = func(string) ([]byte, error) { return []byte("saved-secret-from-an-old-version"), nil }
	if _, err := e.Store.saveSecret(p.Name, "CREDENTIAL", []byte("saved-secret-from-an-old-version")); err != nil {
		t.Fatal(err)
	}
	uploads := 0
	e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
		if tool == "railway" && args[0] == "up" {
			uploads++
		}
		if tool == "railway" && args[0] == "logs" {
			return []byte("saved-secret-from-an-old-version"), nil
		}
		return healthyProvider(tool, args)
	}
	logs, err := e.logs(context.Background(), p)
	if err != nil || len(logs) != 1 || logs[0] != "[REDACTED]" {
		t.Fatal("locally saved secret was exposed in logs")
	}
	if err = os.WriteFile(filepath.Join(p.Source, "app.txt"), []byte("saved-secret-from-an-old-version"), 0600); err != nil {
		t.Fatal(err)
	}
	op, unlock, err := e.begin(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if e.executePublish(context.Background(), p, op, false) == nil || uploads != 0 || op.State != "blocked" {
		t.Fatal("source containing a locally saved secret was submitted")
	}
}

func TestSecretStorageAndRotationKeepValuesOutOfState(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	items := map[string][]byte{}
	e.Store.KeychainPut = func(ref string, value []byte) error {
		if _, exists := items[ref]; exists {
			t.Fatal("rotation reused a Keychain item")
		}
		items[ref] = bytes.Clone(value)
		return nil
	}
	for _, value := range []string{"synthetic-old-secret", "synthetic-new-secret"} {
		info, err := e.Store.saveSecret(p.Name, "APP_CREDENTIAL", []byte(value))
		if err != nil || info.Status != "local_only_cloud_unverified" {
			t.Fatalf("local save failed: %v", err)
		}
	}
	reopened, err := newStore(e.Store.Root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.KeychainGet = func(ref string) ([]byte, error) {
		return bytes.Clone(items[ref]), nil
	}
	values, err := reopened.knownSecrets(p.Name)
	if err != nil || redact("synthetic-old-secret synthetic-new-secret", values) != "[REDACTED] [REDACTED]" {
		t.Fatal("saved versions did not protect historical logs")
	}
	info, err := reopened.secretInfo(p.Name)
	if err != nil || len(info) != 1 || info[0].Versions != 2 || info[0].Status != "local_only_cloud_unverified" {
		t.Fatal("local import was incorrectly treated as cloud verification")
	}
	err = filepath.Walk(e.Store.Root, func(path string, item os.FileInfo, err error) error {
		if err != nil || item.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if bytes.Contains(data, []byte("synthetic-old-secret")) || bytes.Contains(data, []byte("synthetic-new-secret")) {
			t.Fatal("plaintext secret entered a local record")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Root, "secrets", p.Name+".json")
	before, _ := os.ReadFile(path)
	e.Store.KeychainPut = func(string, []byte) error { return errors.New("keychain unavailable") }
	if _, err = e.Store.saveSecret(p.Name, "APP_CREDENTIAL", []byte("third-secret")); err == nil {
		t.Fatal("Keychain failure fell back to plaintext")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed Keychain write changed references")
	}
	reopened.KeychainGet = func(string) ([]byte, error) { return nil, errors.New("keychain unavailable") }
	if _, err = reopened.knownSecrets(p.Name); err == nil {
		t.Fatal("missing redaction context was silently ignored")
	}
}

func TestSecretInputBoundary(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	e.Store.KeychainPut = func(string, []byte) error { t.Fatal("invalid input reached Keychain"); return nil }
	for _, args := range [][]string{
		{"save", p.Name, "APP_CREDENTIAL", "value-on-command-line"},
		{"save", p.Name, "../bad", "--stdin"},
		{"save", "missing", "APP_CREDENTIAL", "--stdin"},
	} {
		if secretCommand(e.Store, args, strings.NewReader("dummy-value")) == nil {
			t.Fatal("invalid secret command accepted")
		}
	}
	for _, value := range []string{"", strings.Repeat("x", maxSecretSize+1)} {
		if secretCommand(e.Store, []string{"save", p.Name, "APP_CREDENTIAL", "--stdin"}, strings.NewReader(value)) == nil {
			t.Fatal("invalid secret size accepted")
		}
	}
}

func TestSystemKeychainRoundTrip(t *testing.T) {
	if os.Getenv("SHIP_TEST_KEYCHAIN") != "1" || runtime.GOOS != "darwin" {
		t.Skip("opt-in local macOS Keychain check with synthetic data only")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	ref := "key-" + hex.EncodeToString(random[:])
	if err := keychainPut(ref, []byte("ship-keychain-synthetic-value")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := keychainDelete(ref); err != nil {
			t.Error(err)
		}
	})
	cmd := exec.Command(os.Args[0], "-test.run=^TestSystemKeychainChild$")
	cmd.Env = append(os.Environ(), "SHIP_KEYCHAIN_TEST_REF="+ref)
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("separate-process Keychain access failed: %v; %s", err, result)
	}
}

func TestSystemKeychainChild(t *testing.T) {
	ref := os.Getenv("SHIP_KEYCHAIN_TEST_REF")
	if !secretRefPattern.MatchString(ref) || os.Getenv("SHIP_TEST_KEYCHAIN") != "1" {
		t.Skip("child of the opt-in native Keychain check")
	}
	value, err := keychainGet(ref)
	defer clear(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "ship-keychain-synthetic-value" {
		t.Fatal("Keychain did not preserve the synthetic value")
	}
}
