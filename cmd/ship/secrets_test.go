package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedSecretsProtectLogsAndSourceUploads(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
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

func TestSecretStorageAndRotationKeepValuesOutOfOrdinaryRecords(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
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
	values, err := reopened.knownSecrets(p.Name)
	if err != nil || redact("synthetic-old-secret synthetic-new-secret", values) != "[REDACTED] [REDACTED]" {
		t.Fatal("saved versions did not protect historical logs")
	}
	info, err := reopened.secretInfo(p.Name)
	if err != nil || len(info) != 1 || info[0].Versions != 2 || info[0].Status != "local_only_cloud_unverified" {
		t.Fatal("local save was incorrectly treated as cloud verification")
	}
	secretPath := filepath.Join(e.Store.Root, "secrets", p.Name+".json")
	err = filepath.Walk(e.Store.Root, func(path string, item os.FileInfo, err error) error {
		if err != nil || item.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		contains := bytes.Contains(data, []byte("synthetic-old-secret")) || bytes.Contains(data, []byte("synthetic-new-secret"))
		if path == secretPath {
			if !contains || item.Mode().Perm() != 0600 {
				t.Fatal("private secret file is missing or has unsafe permissions")
			}
		} else if contains {
			t.Fatal("plaintext secret entered an ordinary project record")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(secretPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.saveSecret(p.Name, "APP_CREDENTIAL", []byte("third-secret")); err == nil {
		t.Fatal("insecure secret file was accepted")
	}
	if _, err = reopened.knownSecrets(p.Name); err == nil {
		t.Fatal("insecure secret file was read")
	}
	after, err := os.ReadFile(secretPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed write changed existing secrets")
	}
	if err = os.Chmod(secretPath, 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSecretFileChild$")
	cmd.Env = append(os.Environ(), "SHIP_TEST_SECRET_DIR="+e.Store.Root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("separate-process read failed: %v", err)
	}
	if bytes.Contains(output, []byte("synthetic-old-secret")) || bytes.Contains(output, []byte("synthetic-new-secret")) {
		t.Fatal("CLI printed secret values")
	}
}

func TestSecretFileChild(t *testing.T) {
	root := os.Getenv("SHIP_TEST_SECRET_DIR")
	if root == "" {
		t.Skip("child of private-file persistence check")
	}
	store, err := newStore(root)
	if err != nil {
		t.Fatal(err)
	}
	values, err := store.knownSecrets("demo")
	if err != nil || redact("synthetic-new-secret", values) != "[REDACTED]" {
		t.Fatal("persisted secret could not be read")
	}
	for _, action := range []string{"list", "check"} {
		if err = secretCommand(store, []string{action, "demo"}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyMissingValueAndSecretSymlinkAreRejected(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.saveSecret(p.Name, "TOKEN", []byte("synthetic-value")); err != nil {
		t.Fatal(err)
	}
	records, err := e.Store.secretRecords(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	records["TOKEN"][0].Value = ""
	records["TOKEN"][0].SyncState = "unknown"
	path := filepath.Join(e.Store.Root, "secrets", p.Name+".json")
	if err = atomicJSON(path, records); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.secretValue(p.Name, records["TOKEN"][0].Ref); err == nil {
		t.Fatal("missing legacy value was treated as available")
	}
	info, err := e.Store.secretInfo(p.Name)
	if err != nil || info[0].Status != "write_outcome_unknown" {
		t.Fatal("storage transition erased an unresolved cloud write")
	}
	outside := filepath.Join(t.TempDir(), "external.json")
	if err = os.Rename(path, outside); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Store.secretRecords(p.Name); err == nil {
		t.Fatal("symlinked secret file was followed")
	}
}

func TestSecretInputBoundary(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"save", p.Name, "APP_CREDENTIAL", "value-on-command-line"},
		{"save", p.Name, "../bad", "--stdin"},
		{"save", "missing", "APP_CREDENTIAL", "--stdin"},
	} {
		if secretCommand(e.Store, args, strings.NewReader("dummy-value")) == nil {
			t.Fatal("invalid secret command accepted")
		}
	}
	for _, value := range []string{"", string([]byte{0xff}), strings.Repeat("x", maxSecretSize+1)} {
		if secretCommand(e.Store, []string{"save", p.Name, "APP_CREDENTIAL", "--stdin"}, strings.NewReader(value)) == nil {
			t.Fatal("invalid secret size accepted")
		}
	}
}
