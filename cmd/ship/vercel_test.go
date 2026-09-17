package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderErrorDoesNotEchoResponseDetails(t *testing.T) {
	err := safeProviderError("vercel", []byte(`{"error":{"code":"bad_request","message":"rejected plaintext-private-value"}}`))
	if !strings.Contains(err.Error(), "bad_request") || strings.Contains(err.Error(), "plaintext-private-value") {
		t.Fatal("provider response details escaped redaction boundary")
	}
	err = safeProviderError("vercel", []byte(`{"error":{"code":"unsafe value with spaces","message":"plaintext-private-value"}}`))
	if !strings.Contains(err.Error(), "cli_error_or_timeout") || strings.Contains(err.Error(), "unsafe value") {
		t.Fatal("unstructured provider detail was exposed")
	}
}

type vercelFixture struct {
	e               *Engine
	p               Project
	env             vercelEnv
	hidden          *vercelEnv
	plan, owner     string
	writes, uploads int
	loseReply       bool
	deployments     []vercelDeployment
}

func newVercelFixture(t *testing.T) *vercelFixture {
	t.Helper()
	e, p := fixture(t)
	p.Provider = "vercel"
	p.VercelTeam = "team_demo123"
	p.VercelProject = "prj_demo123"
	p.AllowHobby = true
	f := &vercelFixture{e: e, p: p, plan: "hobby", owner: p.VercelTeam, env: vercelEnv{ID: "env_demo123", Key: "DATABASE_URL", Type: "sensitive", Visibility: "secret", Target: []string{"production"}, UpdatedAt: 100}}
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(p.Source, "Dockerfile"), filepath.Join(p.Source, "Dockerfile.vercel")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Source, "vercel.json"), []byte(`{"framework":"container"}`), 0600); err != nil {
		t.Fatal(err)
	}
	items := map[string][]byte{}
	e.Store.KeychainPut = func(ref string, value []byte) error { items[ref] = bytes.Clone(value); return nil }
	e.Store.KeychainGet = func(ref string) ([]byte, error) { return bytes.Clone(items[ref]), nil }
	if _, err := e.Store.saveSecret(p.Name, "DATABASE_URL", []byte("postgresql://user:cached-password@ep-demo1.region.neon.tech/db")); err != nil {
		t.Fatal(err)
	}
	e.Providers.Store = e.Store
	e.Providers.VercelConfig = "/private/vercel-auth"
	e.Providers.RunInput = func(_ context.Context, tool string, args []string, input []byte) ([]byte, error) {
		if tool == "neon" {
			return healthyProvider(tool, args)
		}
		if tool != "vercel" {
			t.Fatalf("unexpected provider %s", tool)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--scope "+p.VercelTeam) || !strings.Contains(joined, "--global-config /private/vercel-auth") {
			t.Fatal("Vercel call lost its explicit account scope")
		}
		if strings.Contains(joined, "cached-password") {
			t.Fatal("secret entered command arguments")
		}
		switch args[0] {
		case "deploy":
			f.uploads++
			if !strings.Contains(joined, "--prod") || !strings.Contains(joined, "--project "+p.VercelProject) {
				t.Fatal("deployment target was implicit")
			}
			for i, a := range args {
				if a == "--meta" {
					f.deployments = []vercelDeployment{{UID: "dpl_original", State: "BUILDING", Target: "production", Meta: map[string]string{"ship_operation": strings.TrimPrefix(args[i+1], "ship_operation=")}}}
				}
			}
			return nil, errors.New("lost deployment response")
		case "logs":
			return []byte("cached-password"), nil
		case "api":
			path := args[1]
			switch {
			case strings.HasPrefix(path, "/v2/teams/"):
				return jsonBytes(map[string]any{"id": f.owner, "billing": map[string]string{"plan": f.plan}, "membership": map[string]string{"role": "OWNER"}}), nil
			case path == "/v9/projects/"+p.VercelProject:
				return jsonBytes(vercelProject{ID: p.VercelProject, AccountID: p.VercelTeam, Targets: map[string]vercelDeployment{"production": {ID: "dpl_old", State: "READY", Alias: []string{"demo.example.test"}}}}), nil
			case strings.HasPrefix(path, "/v7/deployments?"):
				return jsonBytes(map[string]any{"deployments": f.deployments}), nil
			case strings.Contains(path, "/env"):
				if input == nil {
					return jsonBytes(map[string]any{"envs": []vercelEnv{f.env}}), nil
				}
				f.writes++
				if path != "/v10/projects/"+p.VercelProject+"/env/"+f.env.ID {
					t.Fatal("Secret update diverged from the pinned official CLI endpoint")
				}
				var body map[string]any
				if json.Unmarshal(input, &body) != nil || body["type"] != "sensitive" || body["visibility"] != "secret" {
					t.Fatal("Secret protection was downgraded")
				}
				if _, exists := body["key"]; exists {
					t.Fatal("Secret update attempted to rename an existing key")
				}
				next := f.env
				next.UpdatedAt++
				next.Comment = body["comment"].(string)
				if f.loseReply {
					f.hidden = &next
					return nil, errors.New("lost Secret response")
				}
				f.env = next
				return []byte(`{}`), nil
			}
		}
		return nil, errors.New("unexpected Vercel invocation")
	}
	return f
}

func TestVercelSecretUnknownWriteAndDrift(t *testing.T) {
	f := newVercelFixture(t)
	ctx := context.Background()
	pre, err := f.e.Providers.inspect(ctx, f.p)
	if err != nil || pre.Observation.Eligible {
		t.Fatal("local cache was treated as cloud truth")
	}
	f.loseReply = true
	if _, err = f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("uncertain Secret write was not retained")
	}
	if _, err = f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("uncertain Secret write was replayed")
	}
	f.env = *f.hidden
	if _, err = f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", false); err != nil || f.writes != 1 {
		t.Fatalf("original Secret was not reconciled: %v", err)
	}
	pre, err = f.e.Providers.inspect(ctx, f.p)
	if err != nil || !pre.Observation.Eligible || pre.Observation.DatabaseBinding != "last_write_and_metadata_match_not_runtime_identity" {
		t.Fatalf("confirmed binding was not eligible: %v", err)
	}
	lines, err := f.e.logs(ctx, f.p)
	if err != nil || len(lines) != 1 || lines[0] != "[REDACTED]" {
		t.Fatal("cached database password leaked through logs")
	}
	f.env.UpdatedAt++
	pre, err = f.e.Providers.inspect(ctx, f.p)
	if err != nil || pre.Observation.Eligible {
		t.Fatal("externally changed Secret was trusted")
	}
	if _, err = f.e.logs(ctx, f.p); err == nil {
		t.Fatal("logs were returned with stale redaction context")
	}
}

func TestVercelBoundariesAndDeploymentReconciliation(t *testing.T) {
	f := newVercelFixture(t)
	ctx := context.Background()
	f.plan = "pro"
	if _, err := f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 0 {
		t.Fatal("paid plan permitted a mutation")
	}
	f.plan = "hobby"
	f.owner = "team_other123"
	if _, err := f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 0 {
		t.Fatal("wrong owner permitted a mutation")
	}
	f.owner = f.p.VercelTeam
	if _, err := f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err != nil {
		t.Fatal(err)
	}
	op, unlock, err := f.e.begin(f.p)
	if err != nil {
		t.Fatal(err)
	}
	err = f.e.executePublish(ctx, f.p, op, false)
	unlock()
	if err != nil || f.uploads != 1 || op.State != "deploying" || op.DeploymentID != "dpl_original" {
		t.Fatalf("lost response was not reconciled: %v", err)
	}
	if _, release, err := f.e.begin(f.p); err == nil {
		release()
		t.Fatal("duplicate deployment permitted")
	}
	f.deployments[0].State = "READY"
	recovered, err := f.e.reconcile(ctx, f.p, false)
	if err != nil || recovered.State != "deploying" {
		t.Fatal("READY without production alias was marked deployed")
	}
	f.deployments[0].State = "ERROR"
	recovered, err = f.e.reconcile(ctx, f.p, false)
	if err != nil || recovered.State != "failed" || recovered.DeploymentID != "dpl_original" || f.uploads != 1 {
		t.Fatal("reconciliation changed the original deployment")
	}
	if _, err = f.e.Store.saveSecret(f.p.Name, "DATABASE_URL", []byte("postgresql://user:other-password@other-db.example/db")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("wrong database endpoint was applied")
	}
}

func TestVercelSourceRejectsImplicitConfiguration(t *testing.T) {
	f := newVercelFixture(t)
	stage, _, _, err := bundleSource(f.p.Source, f.e.Store.Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if err = prepareVercelSource(stage, f.p); err != nil {
		t.Fatal(err)
	}
	var linked map[string]string
	if readJSON(filepath.Join(stage, ".vercel/project.json"), &linked) != nil || linked["orgId"] != f.p.VercelTeam || linked["projectId"] != f.p.VercelProject {
		t.Fatal("staged source lost its explicit resource binding")
	}
	if err = os.WriteFile(filepath.Join(stage, "vercel.json"), []byte(`{"framework":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	if prepareVercelSource(stage, f.p) == nil {
		t.Fatal("Other framework was accepted for a container deployment")
	}
}
