package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"
)

const maxSecretSize = 16 << 10

var secretKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
var secretRefPattern = regexp.MustCompile(`^key-[a-f0-9]{32}$`)

type SecretVersion struct {
	Ref             string    `json:"ref"`
	Value           string    `json:"value,omitempty"`
	SavedAt         time.Time `json:"saved_at"`
	SyncState       string    `json:"sync_state,omitempty"`
	RemoteID        string    `json:"remote_id,omitempty"`
	RemoteUpdatedAt int64     `json:"remote_updated_at,omitempty"`
	Marker          string    `json:"marker,omitempty"`
	WriteErrorCode  string    `json:"write_error_code,omitempty"`
}

type SecretInfo struct {
	Key      string    `json:"key"`
	Versions int       `json:"saved_versions"`
	SavedAt  time.Time `json:"saved_at"`
	Status   string    `json:"status"`
}

func (s *Store) secretRecords(name string) (map[string][]SecretVersion, error) {
	if !slugPattern.MatchString(name) {
		return nil, errors.New("invalid project name")
	}
	refs := map[string][]SecretVersion{}
	path := filepath.Join(s.Root, "secrets", name+".json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return refs, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("secret file must be a regular private file accessible only to its owner")
	}
	err = readJSON(path, &refs)
	if err != nil || refs == nil {
		return nil, errors.New("local secret records could not be read")
	}
	for key, versions := range refs {
		if !secretKeyPattern.MatchString(key) || len(versions) == 0 {
			return nil, errors.New("invalid local secret reference")
		}
		for _, version := range versions {
			if !secretRefPattern.MatchString(version.Ref) || version.SavedAt.IsZero() || len(version.Value) > maxSecretSize {
				return nil, errors.New("invalid local secret reference")
			}
		}
	}
	return refs, nil
}

func (s *Store) saveSecret(name, key string, value []byte) (SecretInfo, error) {
	var info SecretInfo
	if !secretKeyPattern.MatchString(key) || len(value) == 0 || len(value) > maxSecretSize || !utf8.Valid(value) {
		return info, errors.New("secret requires an uppercase variable name and 1–16384 bytes of UTF-8 text")
	}
	if _, err := s.project(name); err != nil {
		return info, errors.New("registered project not found")
	}
	unlock, err := s.lock(name)
	if err != nil {
		return info, err
	}
	defer unlock()
	refs, err := s.secretRecords(name)
	if err != nil {
		return info, err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return info, err
	}
	version := SecretVersion{Ref: "key-" + hex.EncodeToString(id[:]), Value: string(value), SavedAt: time.Now().UTC()}
	// Retain old versions to redact historical logs after a rotation.
	refs[key] = append(refs[key], version)
	if err = atomicJSON(filepath.Join(s.Root, "secrets", name+".json"), refs); err != nil {
		return info, errors.New("saving the local private secret file failed; no cloud changes were made")
	}
	return SecretInfo{key, len(refs[key]), version.SavedAt, "local_only_cloud_unverified"}, nil
}

func (s *Store) secretInfo(name string) ([]SecretInfo, error) {
	refs, err := s.secretRecords(name)
	if err != nil {
		return nil, err
	}
	result := []SecretInfo{}
	for key, versions := range refs {
		result = append(result, secretInfoFor(key, versions))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func secretInfoFor(key string, versions []SecretVersion) SecretInfo {
	last := versions[len(versions)-1]
	status := "local_only_cloud_unverified"
	if last.SyncState == "synced" {
		status = "last_write_confirmed"
	}
	if last.SyncState == "applying" || last.SyncState == "unknown" {
		status = "write_outcome_unknown"
	}
	return SecretInfo{key, len(versions), last.SavedAt, status}
}

func (s *Store) secretValue(name, ref string) ([]byte, error) {
	if !secretRefPattern.MatchString(ref) {
		return nil, errors.New("invalid local secret reference")
	}
	records, err := s.secretRecords(name)
	if err != nil {
		return nil, err
	}
	for _, versions := range records {
		for _, version := range versions {
			if version.Ref == ref {
				if version.Value == "" {
					return nil, errors.New("legacy Secret has no local value; migrate the saved value without rewriting cloud configuration")
				}
				return []byte(version.Value), nil
			}
		}
	}
	return nil, errors.New("local secret value was not found")
}

// Read every saved version for redaction; a saved value alone is never proof of the current cloud configuration.
func (s *Store) knownSecrets(name string) ([]string, error) {
	refs, err := s.secretRecords(name)
	if err != nil {
		return nil, err
	}
	var values []string
	for _, versions := range refs {
		for _, version := range versions {
			if version.Value == "" {
				return nil, errors.New("legacy Secret has no local value; migrate it before reading logs or publishing")
			}
			// Include explicitly saved values even when their variable name has no secret-like suffix.
			values = append(values, secretValues(map[string]string{"SECRET": version.Value})...)
		}
	}
	return values, nil
}

func secretCommand(store *Store, args []string, input io.Reader) error {
	if len(args) < 2 {
		return errors.New("use secret save NAME KEY --stdin, secret list NAME, or secret check NAME")
	}
	action, name := args[0], args[1]
	if _, err := store.project(name); err != nil {
		return errors.New("registered project not found")
	}
	switch {
	case action == "save" && len(args) == 4 && args[3] == "--stdin":
		if file, ok := input.(*os.File); ok {
			stat, err := file.Stat()
			if err != nil || stat.Mode()&os.ModeCharDevice != 0 {
				return errors.New("pipe the secret or redirect a private file to stdin; terminal input would echo the value")
			}
		}
		value, err := io.ReadAll(io.LimitReader(input, maxSecretSize+1))
		defer clear(value)
		if err != nil {
			return errors.New("secret input could not be read")
		}
		info, err := store.saveSecret(name, args[2], value)
		if err == nil {
			output(info)
		}
		return err
	case action == "list" && len(args) == 2:
		info, err := store.secretInfo(name)
		if err == nil {
			output(info)
		}
		return err
	case action == "check" && len(args) == 2:
		values, err := store.knownSecrets(name)
		if err == nil {
			output(map[string]any{"local_values_accessible": true, "redaction_value_count": len(values), "cloud_configuration_verified": false})
		}
		return err
	default:
		return errors.New("invalid secret command; values are accepted only on stdin and never printed")
	}
}
