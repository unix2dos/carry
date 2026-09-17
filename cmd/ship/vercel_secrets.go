package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"unicode/utf8"
)

func productionSecret(envs []vercelEnv, key string) (*vercelEnv, error) {
	var found *vercelEnv
	for i := range envs {
		item := &envs[i]
		if item.Key != key || !item.production() {
			continue
		}
		if found != nil || len(item.Target) != 1 || item.GitBranch != "" || len(item.CustomEnvironmentIDs) > 0 || item.ConfigurationID != "" {
			return nil, errors.New("Secret is ambiguous, shared across environments, or integration-managed; review it in Vercel before proceeding")
		}
		if !idPattern.MatchString(item.ID) || item.UpdatedAt <= 0 {
			return nil, errors.New("Vercel Secret identity or version is unavailable")
		}
		found = item
	}
	return found, nil
}

func (e *Engine) syncVercelSecret(ctx context.Context, p Project, key string, apply bool) (SecretInfo, error) {
	var empty SecretInfo
	if p.Provider != "vercel" || !secretKeyPattern.MatchString(key) {
		return empty, errors.New("Secret sync requires a Vercel binding and an uppercase variable name")
	}
	unlock, err := e.Store.lock(p.Name)
	if err != nil {
		return empty, err
	}
	defer unlock()
	refs, err := e.Store.secretRecords(p.Name)
	if err != nil {
		return empty, err
	}
	versions := refs[key]
	if len(versions) == 0 {
		return empty, errors.New("save the Secret in the local private file first")
	}
	last := &versions[len(versions)-1]
	if _, plan, err := e.Providers.vercelAccount(ctx, p); err != nil {
		return empty, err
	} else if apply && (plan != "hobby" || !p.AllowHobby || !p.AllowPublish) {
		return empty, errors.New("Secret writes require authorized personal noncommercial Hobby conditions")
	}
	envs, err := e.Providers.vercelEnvs(ctx, p)
	if err != nil {
		return empty, err
	}
	remote, err := productionSecret(envs, key)
	if err != nil {
		return empty, err
	}
	if apply {
		for _, version := range versions {
			if version.SyncState == "unknown" || version.SyncState == "applying" {
				return empty, errors.New("a previous Secret write is unresolved; use secret reconcile before another write")
			}
		}
		if last.SyncState == "synced" {
			return empty, errors.New("this local version was already applied; save a new version for an intentional update")
		}
		ops, err := e.Store.operations(p.Name)
		if err != nil {
			return empty, err
		}
		for _, op := range ops {
			if !op.terminal() {
				return empty, errors.New("reconcile the pending deployment before changing its Secrets")
			}
		}
		neon, host, err := e.Providers.inspectNeon(ctx, p)
		if err != nil {
			return empty, err
		}
		if neon.NeonPlan != "free" {
			return empty, errors.New("Secret writes require the validated Neon Free binding")
		}
		value, err := e.Store.secretValue(p.Name, last.Ref)
		if err != nil {
			return empty, err
		}
		defer clear(value)
		if !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 || len(value) == 0 || len(value) > maxSecretSize {
			return empty, errors.New("Vercel Secret must be bounded UTF-8 text without NUL bytes")
		}
		if key == "DATABASE_URL" && !databaseEndpointMatches(string(value), host) {
			return empty, errors.New("DATABASE_URL does not match the registered Neon endpoint")
		}
		last.Marker = "ship-secret:" + last.Ref
		last.SyncState = "applying"
		last.RemoteID = ""
		last.RemoteUpdatedAt = 0
		path, method := "/v10/projects/"+p.VercelProject+"/env", "POST"
		if remote != nil {
			last.RemoteID = remote.ID
			last.RemoteUpdatedAt = remote.UpdatedAt
			path = "/v10/projects/" + p.VercelProject + "/env/" + url.PathEscape(remote.ID)
			method = "PATCH"
		}
		body := map[string]any{"value": string(value), "type": "sensitive", "visibility": "secret", "target": []string{"production"}, "comment": last.Marker}
		if remote == nil {
			body["key"] = key
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return empty, err
		}
		defer clear(payload)
		refs[key] = versions
		if err = atomicJSON(filepath.Join(e.Store.Root, "secrets", p.Name+".json"), refs); err != nil {
			return empty, err
		}
		// An unknown response is resolved by reading the marker and version, never by replaying this mutation.
		writeErr := e.Providers.vercelAPI(ctx, p, path, method, payload, nil)
		var commandErr *providerCommandError
		if errors.As(writeErr, &commandErr) {
			last.WriteErrorCode = commandErr.Code
		}
		last.SyncState = "unknown"
		refs[key] = versions
		if err = atomicJSON(filepath.Join(e.Store.Root, "secrets", p.Name+".json"), refs); err != nil {
			return empty, err
		}
		envs, err = e.Providers.vercelEnvs(ctx, p)
		if err != nil {
			return empty, errors.New("Secret write outcome is unknown; use secret reconcile, not another apply")
		}
		remote, err = productionSecret(envs, key)
		if err != nil {
			return empty, err
		}
	} else {
		// Reconcile the original pending version even if a newer local value was saved meanwhile.
		for i := range versions {
			if versions[i].SyncState == "unknown" || versions[i].SyncState == "applying" {
				last = &versions[i]
				break
			}
		}
	}
	if last.Marker == "" || remote == nil || remote.Type != "sensitive" || remote.Comment != last.Marker || (last.RemoteID != "" && remote.ID != last.RemoteID) {
		return empty, errors.New("cannot confirm the original Secret write; keep it unresolved and review the cloud state")
	}
	if last.SyncState == "synced" && remote.UpdatedAt != last.RemoteUpdatedAt {
		return empty, errors.New("cloud Secret changed after the confirmed write")
	}
	if last.SyncState != "synced" && remote.UpdatedAt <= last.RemoteUpdatedAt {
		return empty, errors.New("Secret version has not advanced; original write remains unconfirmed")
	}
	last.RemoteID = remote.ID
	last.RemoteUpdatedAt = remote.UpdatedAt
	last.SyncState = "synced"
	refs[key] = versions
	if err = atomicJSON(filepath.Join(e.Store.Root, "secrets", p.Name+".json"), refs); err != nil {
		return empty, err
	}
	return secretInfoFor(key, versions), nil
}
