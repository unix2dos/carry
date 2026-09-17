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
)

const maxSecretSize = 16 << 10

var secretKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
var secretRefPattern = regexp.MustCompile(`^key-[a-f0-9]{32}$`)

type SecretVersion struct {
	Ref     string    `json:"ref"`
	SavedAt time.Time `json:"saved_at"`
}

type SecretInfo struct {
	Key      string    `json:"key"`
	Versions int       `json:"saved_versions"`
	SavedAt  time.Time `json:"saved_at"`
	Status   string    `json:"status"`
}

func (s *Store) secretRefs(name string) (map[string][]SecretVersion, error) {
	if !slugPattern.MatchString(name) {
		return nil, errors.New("invalid project name")
	}
	refs := map[string][]SecretVersion{}
	err := readJSON(filepath.Join(s.Root, "secrets", name+".json"), &refs)
	if os.IsNotExist(err) {
		return refs, nil
	}
	if err != nil || refs == nil {
		return nil, errors.New("local secret references could not be read")
	}
	for key, versions := range refs {
		if !secretKeyPattern.MatchString(key) || len(versions) == 0 {
			return nil, errors.New("invalid local secret reference")
		}
		for _, version := range versions {
			if !secretRefPattern.MatchString(version.Ref) || version.SavedAt.IsZero() {
				return nil, errors.New("invalid local secret reference")
			}
		}
	}
	return refs, nil
}

func (s *Store) saveSecret(name, key string, value []byte) (SecretInfo, error) {
	var info SecretInfo
	if !secretKeyPattern.MatchString(key) || len(value) == 0 || len(value) > maxSecretSize {
		return info, errors.New("secret requires an uppercase variable name and 1–16384 bytes")
	}
	if _, err := s.project(name); err != nil {
		return info, errors.New("registered project not found")
	}
	unlock, err := s.lock(name)
	if err != nil {
		return info, err
	}
	defer unlock()
	refs, err := s.secretRefs(name)
	if err != nil {
		return info, err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return info, err
	}
	version := SecretVersion{Ref: "key-" + hex.EncodeToString(id[:]), SavedAt: time.Now().UTC()}
	put := s.KeychainPut
	if put == nil {
		put = keychainPut
	}
	if err = put(version.Ref, value); err != nil {
		return info, err
	}
	// Retain old versions to redact historical logs after a rotation.
	refs[key] = append(refs[key], version)
	// Keep the Keychain item on an uncertain reference write: atomicJSON may have committed before a directory sync failure.
	if err = atomicJSON(filepath.Join(s.Root, "secrets", name+".json"), refs); err != nil {
		return info, errors.New("secret stored in Keychain but saving its local reference failed; no cloud changes were made")
	}
	return SecretInfo{key, len(refs[key]), version.SavedAt, "local_only_cloud_unverified"}, nil
}

func (s *Store) secretInfo(name string) ([]SecretInfo, error) {
	refs, err := s.secretRefs(name)
	if err != nil {
		return nil, err
	}
	result := []SecretInfo{}
	for key, versions := range refs {
		result = append(result, SecretInfo{key, len(versions), versions[len(versions)-1].SavedAt, "local_only_cloud_unverified"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

// Read every saved version for redaction; a saved value alone is never proof of the current cloud configuration.
func (s *Store) knownSecrets(name string) ([]string, error) {
	refs, err := s.secretRefs(name)
	if err != nil {
		return nil, err
	}
	get := s.KeychainGet
	if get == nil {
		get = keychainGet
	}
	var values []string
	for _, versions := range refs {
		for _, version := range versions {
			value, err := get(version.Ref)
			if err != nil {
				return nil, err
			}
			// Include explicitly saved values even when their variable name has no secret-like suffix.
			values = append(values, secretValues(map[string]string{"SECRET": string(value)})...)
			clear(value)
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
