package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Engine, Project) {
	t.Helper()
	store, err := newStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "Dockerfile"), []byte("FROM scratch\n"), 0644)
	p := Project{Name: "demo", Source: source, URL: "https://demo.example.test", Workspace: "workspace-demo", RailwayProject: "project-demo", Service: "service-demo", Environment: "environment-demo", NeonOrg: "org-demo1", NeonProject: "neon-demo", NeonEndpoint: "ep-demo1", AllowPublish: true, AllowTrial: true}
	return &Engine{Store: store, Providers: &Providers{}}, p
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func healthyProvider(tool string, args []string) ([]byte, error) {
	joined := strings.Join(args, " ")
	if tool == "railway" && args[0] == "api" {
		return jsonBytes(map[string]any{"data": map[string]any{"project": map[string]any{"id": "project-demo", "workspaceId": "workspace-demo", "services": map[string]any{"edges": []any{map[string]any{"node": map[string]string{"id": "service-demo"}}}}, "environments": map[string]any{"edges": []any{map[string]any{"node": map[string]string{"id": "environment-demo"}}}}}, "workspace": map[string]any{"plan": "HOBBY", "customer": map[string]any{"isTrialing": true, "isUsageSubscriber": false, "creditBalance": 5, "defaultPaymentMethodId": nil}}}}), nil
	}
	if tool == "railway" && strings.HasPrefix(joined, "service status") {
		return []byte(`{"id":"service-demo","status":"SUCCESS","deploymentId":"deployment-old"}`), nil
	}
	if tool == "railway" && strings.HasPrefix(joined, "variable list") {
		return []byte(`{"PORT":"8080","DATABASE_URL":"postgresql://user:secret-password@ep-demo1.region.neon.tech/db","VALIDATION_TOKEN":"a-secret-validation-token"}`), nil
	}
	if tool == "neon" && strings.HasPrefix(joined, "orgs list") {
		return []byte(`[{"id":"org-demo1","plan":"free"}]`), nil
	}
	if tool == "neon" && strings.Contains(joined, "/endpoints/") {
		return []byte(`{"endpoint":{"id":"ep-demo1","host":"ep-demo1.region.neon.tech","current_state":"idle"}}`), nil
	}
	if tool == "neon" && strings.Contains(joined, "/projects/") {
		return []byte(`{"project":{"id":"neon-demo","org_id":"org-demo1"}}`), nil
	}
	return nil, errors.New("unexpected provider invocation")
}

func TestUnknownSubmissionIsReconciledWithoutAnotherUpload(t *testing.T) {
	e, p := fixture(t)
	upCalls := 0
	marker := ""
	visible := false
	e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
		if tool == "railway" && args[0] == "up" {
			upCalls++
			marker = args[len(args)-1]
			return nil, errors.New("lost response after server accepted upload")
		}
		if tool == "railway" && args[0] == "deployment" {
			if !visible {
				return []byte(`[]`), nil
			}
			return jsonBytes([]any{map[string]any{"id": "deployment-new", "status": "SLEEPING", "meta": map[string]string{"cliMessage": marker}}}), nil
		}
		return healthyProvider(tool, args)
	}
	op, unlock, err := e.begin(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.executePublish(context.Background(), p, op, false); err == nil {
		t.Fatal("expected uncertain outcome")
	}
	unlock()
	if op.State != "unknown" || upCalls != 1 {
		t.Fatalf("unexpected state %+v / calls %d", op, upCalls)
	}
	if _, release, err := e.begin(p); err == nil {
		release()
		t.Fatal("duplicate publish was permitted")
	}
	// A fresh Engine simulates reopening the tool; only the durable operation marker is reused.
	visible = true
	reopened := &Engine{Store: e.Store, Providers: e.Providers}
	recovered, err := reopened.reconcile(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "deployed" || recovered.DeploymentID != "deployment-new" || upCalls != 1 {
		t.Fatal("reconciliation did not preserve the original submission")
	}
	data, _ := os.ReadFile(filepath.Join(e.Store.Root, "operations", p.Name+"--"+op.ID+".json"))
	if strings.Contains(string(data), "secret-password") || strings.Contains(string(data), "a-secret-validation-token") {
		t.Fatal("credentials entered operation state")
	}
}
func TestPreflightDenialNeverUploads(t *testing.T) {
	e, p := fixture(t)
	p.AllowTrial = false
	upCalls := 0
	e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
		if args[0] == "up" {
			upCalls++
		}
		return healthyProvider(tool, args)
	}
	op, unlock, err := e.begin(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if e.executePublish(context.Background(), p, op, false) == nil {
		t.Fatal("Trial without explicit acceptance was permitted")
	}
	if upCalls != 0 || op.State != "blocked" {
		t.Fatal("denied preflight produced a submission")
	}
	if err = e.reconcileOnce(context.Background(), p, op); err != nil || op.State != "blocked" {
		t.Fatal("pre-submit failure was turned into unknown")
	}
}

func TestBillingChangesBlockPublish(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"paid subscription", func(c map[string]any) { c["isUsageSubscriber"] = true }},
		{"payment method", func(c map[string]any) { c["defaultPaymentMethodId"] = "payment-test" }},
		{"not Trial", func(c map[string]any) { c["isTrialing"] = false }},
		{"no credits", func(c map[string]any) { c["creditBalance"] = float64(0) }},
		{"missing subscription state", func(c map[string]any) { delete(c, "isUsageSubscriber") }},
		{"unknown trial state", func(c map[string]any) { c["isTrialing"] = nil }},
		{"missing payment state", func(c map[string]any) { delete(c, "defaultPaymentMethodId") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, p := fixture(t)
			uploads := 0
			e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
				if args[0] == "up" {
					uploads++
				}
				b, err := healthyProvider(tool, args)
				if err != nil {
					return b, err
				}
				if tool == "railway" && args[0] == "api" {
					var d map[string]any
					json.Unmarshal(b, &d)
					c := d["data"].(map[string]any)["workspace"].(map[string]any)["customer"].(map[string]any)
					tc.change(c)
					b = jsonBytes(d)
				}
				return b, nil
			}
			op, unlock, err := e.begin(p)
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			if e.executePublish(context.Background(), p, op, false) == nil || uploads != 0 || op.State != "blocked" {
				t.Fatal("billing boundary did not prevent upload")
			}
		})
	}
}
func TestOwnershipMismatchAndSecretSafeSource(t *testing.T) {
	e, p := fixture(t)
	p.Workspace = "wrong-workspace"
	e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
		return healthyProvider(tool, args)
	}
	if _, err := e.Providers.inspect(context.Background(), p); err == nil {
		t.Fatal("workspace mismatch accepted")
	}
	os.WriteFile(filepath.Join(p.Source, ".env"), []byte("a-secret-validation-token"), 0600)
	stage, _, count, err := bundleSource(p.Source, e.Store.Root, []string{"a-secret-validation-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if count != 1 {
		t.Fatal("secret env file was staged")
	}
	if _, err = os.Stat(filepath.Join(stage, ".env")); !os.IsNotExist(err) {
		t.Fatal("env file included")
	}
	os.WriteFile(filepath.Join(p.Source, "main.go"), []byte("a-secret-validation-token"), 0600)
	if _, _, _, err = bundleSource(p.Source, e.Store.Root, []string{"a-secret-validation-token"}); err == nil {
		t.Fatal("embedded credential uploaded")
	}
	os.Remove(filepath.Join(p.Source, "main.go"))
	os.Symlink("/etc/passwd", filepath.Join(p.Source, "link"))
	if _, _, _, err = bundleSource(p.Source, e.Store.Root, nil); err == nil {
		t.Fatal("source symlink followed")
	}
}
func TestPrivateStateAndProjectLock(t *testing.T) {
	e, p := fixture(t)
	if err := validateProject(&p); err != nil {
		t.Fatal(err)
	}
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(e.Store.Root, "projects", p.Name+".json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("project record is not private")
	}
	unlock, err := e.Store.lock(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := e.Store.lock(p.Name); err == nil {
		release()
		t.Fatal("concurrent writer accepted")
	}
	unlock()
	if _, err = e.Store.project("../../outside"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err = e.Store.register(p); err == nil {
		t.Fatal("existing binding overwritten")
	}
}
func TestLocalAPIBoundary(t *testing.T) {
	e, p := fixture(t)
	if err := e.Store.register(p); err != nil {
		t.Fatal(err)
	}
	e.Providers.Run = func(context.Context, string, []string) ([]byte, error) {
		t.Fatal("local polling invoked a cloud provider")
		return nil, nil
	}
	a := &localAPI{engine: e, host: "127.0.0.1:1234", token: "session-key", ctx: context.Background()}
	cases := []struct {
		host, origin, auth, method, body string
		code                             int
	}{
		{"attacker.test:1234", "", "Bearer session-key", "GET", "", 403},
		{a.host, "https://attacker.test", "Bearer session-key", "GET", "", 403},
		{a.host, "", "", "GET", "", 401},
		{a.host, "", "Bearer session-key", "GET", "", 200},
		{a.host, "", "Bearer session-key", "POST", "{}", 403},
		{a.host, "http://" + a.host, "Bearer session-key", "POST", `{"command":"delete"}`, 400},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://"+a.host+"/api/projects", strings.NewReader(c.body))
		r.Host = c.host
		r.Header.Set("Origin", c.origin)
		r.Header.Set("Authorization", c.auth)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != c.code {
			t.Fatalf("got %d want %d", w.Code, c.code)
		}
	}
}
func TestCredentialRedactionAndNoRedirect(t *testing.T) {
	vars := map[string]string{"DATABASE_URL": "postgresql://user:secret-password@db.example/db", "VALIDATION_TOKEN": "a-secret-validation-token"}
	text := redact("postgresql://user:secret-password@db.example/db secret-password a-secret-validation-token Authorization: Bearer random-secret", secretValues(vars))
	for _, v := range []string{"secret-password", "a-secret-validation-token", "random-secret"} {
		if strings.Contains(text, v) {
			t.Fatal("credential survived redaction")
		}
	}
	encoded, _ := json.Marshal(map[string]string{"value": "secret\nwith\"quotes"})
	if strings.Contains(redact(string(encoded), []string{"secret\nwith\"quotes"}), "quotes") {
		t.Fatal("JSON-escaped credential survived redaction")
	}
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer origin.Close()
	checks := checkApplication(context.Background(), origin.URL)
	if targetCalls != 0 || checks[0].OK || checks[0].HTTP != 302 {
		t.Fatal("application check followed a redirect")
	}
}

func TestCompletedHistoryIsStable(t *testing.T) {
	e, p := fixture(t)
	op, err := newOperation(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	op.State = "deployed"
	op.DeploymentID = "deployment-finished"
	op.ProviderState = "SUCCESS"
	op.Message = "verified at completion"
	if err = e.Store.saveOp(&op); err != nil {
		t.Fatal(err)
	}
	stamp := op.Updated
	calls := 0
	e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
		calls++
		return jsonBytes([]any{map[string]any{"id": op.DeploymentID, "status": "REMOVED", "meta": map[string]string{"cliMessage": op.Marker}}}), nil
	}
	result, err := e.reconcile(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "deployed" || result.ProviderState != "SUCCESS" || calls != 0 || !result.Updated.Equal(stamp) {
		t.Fatal("a historical completed operation was rewritten from later resource state")
	}
}

func TestLegacyMarkerSurvivesRename(t *testing.T) {
	e, p := fixture(t)
	op, err := newOperation(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(op.Marker, "upok:") {
		t.Fatal("new operation did not use the UpOK marker")
	}
	op.Marker = "pdeploy:" + op.ID
	op.State = "unknown"
	if err = e.Store.saveOp(&op); err != nil {
		t.Fatal(err)
	}
	e.Providers.Run = func(_ context.Context, tool string, args []string) ([]byte, error) {
		if tool != "railway" || args[0] != "deployment" {
			t.Fatalf("unexpected mutation or query during reconciliation: %s %v", tool, args)
		}
		return jsonBytes([]any{map[string]any{"id": "legacy-deployment", "status": "SLEEPING", "meta": map[string]string{"cliMessage": op.Marker}}}), nil
	}
	result, err := e.reconcile(context.Background(), p, false)
	if err != nil || result.State != "deployed" || result.Marker != op.Marker || result.DeploymentID != "legacy-deployment" {
		t.Fatalf("legacy operation was not reconciled intact: %v", err)
	}
	for _, name := range []string{".pdeploy", ".upok"} {
		if !excluded(name) {
			t.Fatalf("private state directory can enter an upload: %s", name)
		}
	}
}
