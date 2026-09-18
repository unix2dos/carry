package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderErrorDoesNotEchoResponseDetails(t *testing.T) {
	err := safeProviderError("vercel", []byte(`{"error":{"code":"bad_request","message":"rejected plaintext-private-value"}}`), nil)
	if !strings.Contains(err.Error(), "bad_request") || strings.Contains(err.Error(), "plaintext-private-value") {
		t.Fatal("provider response details escaped redaction boundary")
	}
	err = safeProviderError("vercel", []byte(`{"error":{"code":"unsafe value with spaces","message":"plaintext-private-value"}}`), nil)
	if !strings.Contains(err.Error(), "cli_error_or_timeout") || strings.Contains(err.Error(), "unsafe value") {
		t.Fatal("unstructured provider detail was exposed")
	}
}

func TestVercelKnownValidationRejectionIsNotUnknown(t *testing.T) {
	// Exact response observed from the pinned CLI in the isolated cloud probe.
	err := safeProviderError("vercel", nil, []byte("Error: You cannot change the key of a Sensitive Environment Variable. (400)\n"))
	var classified *providerCommandError
	if !errors.As(err, &classified) || !classified.Rejected || classified.Code != "sensitive_key_immutable" {
		t.Fatal("known provider validation rejection was discarded")
	}
	f := newVercelFixture(t)
	run := f.e.Providers.RunInput
	f.e.Providers.RunInput = func(ctx context.Context, tool string, args []string, input []byte) ([]byte, error) {
		if input != nil {
			f.writes++
			return nil, err
		}
		return run(ctx, tool, args, input)
	}
	if _, err = f.e.syncVercelSecret(context.Background(), f.p, "DATABASE_URL", true); err == nil {
		t.Fatal("rejected write reported success")
	}
	records, readErr := f.e.Store.secretRecords(f.p.Name)
	if readErr != nil || records["DATABASE_URL"][0].SyncState != "rejected" {
		t.Fatal("definite rejection was not preserved")
	}
	if _, err = f.e.syncVercelSecret(context.Background(), f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("rejected attempt was automatically reused")
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
	if err != nil || !pre.Observation.Eligible || pre.Observation.DatabaseBinding != "provider_secret_retained_not_readable" {
		t.Fatal("local cache was treated as cloud truth")
	}
	f.loseReply = true
	if _, err = f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("uncertain Secret write was not retained")
	}
	if _, err = f.e.syncVercelSecret(ctx, f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("uncertain Secret write was replayed")
	}
	pre, err = f.e.Providers.inspect(ctx, f.p)
	if err != nil || pre.Observation.Eligible {
		t.Fatal("unresolved Secret write did not block deployment")
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
	if err != nil || !pre.Observation.Eligible || pre.Observation.DatabaseBinding != "provider_secret_retained_not_readable" {
		t.Fatal("externally changed Secret was trusted")
	}
	lines, err = f.e.logs(ctx, f.p)
	if err != nil || len(lines) != 1 || lines[0] != "[REDACTED]" {
		t.Fatal("known historical secrets were not redacted")
	}
}

func TestVercelDeployRetainsOpaqueProviderSecrets(t *testing.T) {
	f := newVercelFixture(t)
	if err := os.Remove(filepath.Join(f.e.Store.Root, "secrets", f.p.Name+".json")); err != nil {
		t.Fatal(err)
	}
	op, unlock, err := f.e.begin(f.p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = f.e.executePublish(context.Background(), f.p, op, false); err != nil {
		t.Fatalf("existing configuration could not be retained: %v", err)
	}
	if f.uploads != 1 || f.writes != 0 || op.State != "deploying" {
		t.Fatal("source deployment depended on replacing provider Secrets")
	}
	observation := f.e.Store.observation(f.p.Name)
	if observation == nil || observation.DatabaseBinding != "provider_secret_retained_not_readable" {
		t.Fatal("opaque database identity was reported as verified")
	}
}

func TestDiagnosedSecretHistoryDoesNotReopen(t *testing.T) {
	f := newVercelFixture(t)
	records, err := f.e.Store.secretRecords(f.p.Name)
	if err != nil {
		t.Fatal(err)
	}
	records["DATABASE_URL"][0].SyncState = "diagnosed_not_applied"
	records["DATABASE_URL"][0].Resolution = "user-approved diagnosis"
	path := filepath.Join(f.e.Store.Root, "secrets", f.p.Name+".json")
	if err = atomicJSON(path, records); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	f.e.Providers.RunInput = func(context.Context, string, []string, []byte) ([]byte, error) {
		t.Fatal("archived diagnosis contacted the provider")
		return nil, nil
	}
	info, err := f.e.syncVercelSecret(context.Background(), f.p, "DATABASE_URL", false)
	if err != nil || info.Status != "write_diagnosed_not_applied" {
		t.Fatal("archived disposition was reopened")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("historical disposition changed")
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

func TestVercelWithoutNeon(t *testing.T) {
	f := newVercelFixture(t)
	f.p.NeonOrg, f.p.NeonProject, f.p.NeonEndpoint = "", "", ""
	if err := atomicJSON(filepath.Join(f.e.Store.Root, "projects", f.p.Name+".json"), f.p); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.e.Store.Root, "secrets", f.p.Name+".json")); err != nil {
		t.Fatal(err)
	}
	original := f.e.Providers.RunInput
	f.e.Providers.RunInput = func(ctx context.Context, tool string, args []string, input []byte) ([]byte, error) {
		if tool == "neon" {
			t.Fatal("application-only project contacted Neon")
		}
		if args[0] == "api" && strings.Contains(args[1], "/env") && input == nil {
			return []byte(`{"envs":[]}`), nil
		}
		return original(ctx, tool, args, input)
	}
	ctx := context.Background()
	f.plan = "pro"
	pre, err := f.e.Providers.inspect(ctx, f.p)
	if err != nil || pre.Observation.Eligible {
		t.Fatal("omitting Neon bypassed Hobby restriction")
	}
	f.plan = "hobby"
	op, unlock, err := f.e.begin(f.p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = f.e.executePublish(ctx, f.p, op, false); err != nil || f.uploads != 1 || f.writes != 0 || op.State != "deploying" {
		t.Fatalf("application without DATABASE_URL could not deploy: %v", err)
	}
	observation := f.e.Store.observation(f.p.Name)
	if observation == nil || !observation.Eligible || observation.DatabaseBinding != "not_managed" || observation.NeonPlan != "" {
		t.Fatal("application-only state claimed database ownership")
	}
	if _, err = f.e.logs(ctx, f.p); err != nil {
		t.Fatalf("application-only logs require Neon: %v", err)
	}
}

func TestVercelOrdinarySecretWithoutNeon(t *testing.T) {
	f := newVercelFixture(t)
	f.p.NeonOrg, f.p.NeonProject, f.p.NeonEndpoint = "", "", ""
	f.env.Key = "API_TOKEN"
	original := f.e.Providers.RunInput
	f.e.Providers.RunInput = func(ctx context.Context, tool string, args []string, input []byte) ([]byte, error) {
		if tool == "neon" {
			t.Fatal("ordinary Secret on application-only project contacted Neon")
		}
		return original(ctx, tool, args, input)
	}
	if _, err := f.e.Store.saveSecret(f.p.Name, "API_TOKEN", []byte("standalone-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.syncVercelSecret(context.Background(), f.p, "API_TOKEN", true); err != nil || f.writes != 1 {
		t.Fatalf("ordinary Secret required a database: %v", err)
	}
	if _, err := f.e.syncVercelSecret(context.Background(), f.p, "DATABASE_URL", true); err == nil || f.writes != 1 {
		t.Fatal("database configuration was rewritten without a verifiable binding")
	}
}
