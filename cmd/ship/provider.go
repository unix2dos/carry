package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Providers struct {
	Railway      string
	Neon         string
	NeonConfig   string
	Vercel       string
	VercelConfig string
	Store        *Store
	// The command seam lets tests verify cloud arguments and unknown outcomes without real mutations.
	Run      func(context.Context, string, []string) ([]byte, error)
	RunInput func(context.Context, string, []string, []byte) ([]byte, error)
}
type limitedBuffer struct {
	bytes.Buffer
	exceeded bool
}

type providerCommandError struct {
	Tool, Code string
	Rejected   bool
}

func (e *providerCommandError) Error() string {
	if e.Rejected {
		return fmt.Sprintf("%s request was rejected (%s); no automatic retry", e.Tool, e.Code)
	}
	return fmt.Sprintf("%s request failed (%s); cloud outcome is not confirmed, no automatic retry", e.Tool, e.Code)
}

func safeProviderError(tool string, data, stderr []byte) error {
	if tool == "vercel" && strings.Contains(string(stderr), "Error: You cannot change the key of a Sensitive Environment Variable. (400)") {
		return &providerCommandError{Tool: tool, Code: "sensitive_key_immutable", Rejected: true}
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	code := "cli_error_or_timeout"
	if json.Unmarshal(data, &response) == nil && regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(response.Error.Code) {
		code = response.Error.Code
	}
	return &providerCommandError{Tool: tool, Code: code}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := (2 << 20) - b.Len()
	if n > room {
		b.exceeded = true
		p = p[:max(0, room)]
	}
	b.Buffer.Write(p)
	return n, nil
}
func cleanEnv() []string {
	result := []string{}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		switch k {
		case "RAILWAY_TOKEN", "RAILWAY_API_TOKEN", "NEON_API_KEY", "NEON_PROFILE", "VERCEL_TOKEN", "VERCEL_ORG_ID", "VERCEL_PROJECT_ID", "VERCEL_TELEMETRY_DISABLED", "NO_UPDATE_NOTIFIER":
			continue
		}
		result = append(result, v)
	}
	return append(result, "VERCEL_TELEMETRY_DISABLED=1", "NO_UPDATE_NOTIFIER=1")
}
func (v *Providers) call(ctx context.Context, tool string, args ...string) ([]byte, error) {
	return v.callInput(ctx, tool, nil, args...)
}
func (v *Providers) callInput(ctx context.Context, tool string, input []byte, args ...string) ([]byte, error) {
	if tool == "neon" {
		args = append(args, "--config-dir", v.NeonConfig, "--no-analytics", "--output", "json")
	}
	if v.RunInput != nil {
		return v.RunInput(ctx, tool, args, input)
	}
	if v.Run != nil && input == nil {
		return v.Run(ctx, tool, args)
	}
	bin := v.Railway
	if tool == "neon" {
		bin = v.Neon
	}
	if tool == "vercel" {
		bin = v.Vercel
	}
	if bin == "" {
		return nil, fmt.Errorf("%s CLI is not configured", tool)
	}
	timeout := 40 * time.Second
	if len(args) > 0 && ((tool == "railway" && args[0] == "up") || (tool == "vercel" && args[0] == "deploy")) {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = cleanEnv()
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, safeProviderError(tool, stdout.Bytes(), stderr.Bytes())
	}
	if stdout.exceeded {
		return nil, errors.New("provider output exceeded the size limit")
	}
	return stdout.Bytes(), nil
}
func (v *Providers) decode(ctx context.Context, tool string, out any, args ...string) error {
	b, err := v.call(ctx, tool, args...)
	if err != nil {
		return err
	}
	if json.Unmarshal(b, out) != nil {
		return fmt.Errorf("%s returned invalid JSON", tool)
	}
	return nil
}
func selectors(p Project) []string {
	return []string{"--project", p.RailwayProject, "--service", p.Service, "--environment", p.Environment}
}
func (v *Providers) graph(ctx context.Context, q string, vars any, out any) error {
	b, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err = v.decode(ctx, "railway", &envelope, "api", q, "--variables", string(b), "--compact"); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 && string(envelope.Errors) != "null" && string(envelope.Errors) != "[]" {
		return errors.New("Railway API rejected the query")
	}
	if json.Unmarshal(envelope.Data, out) != nil {
		return errors.New("Railway API returned incomplete data")
	}
	return nil
}

type Deployment struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	AliasAssigned bool   `json:"alias_assigned,omitempty"`
	Meta          struct {
		Message string `json:"cliMessage"`
	} `json:"meta"`
}

func (v *Providers) deployments(ctx context.Context, p Project) ([]Deployment, error) {
	if p.Provider == "vercel" {
		return v.vercelDeployments(ctx, p)
	}
	var d []Deployment
	args := append([]string{"deployment", "list"}, selectors(p)...)
	args = append(args, "--limit", "100", "--json")
	err := v.decode(ctx, "railway", &d, args...)
	return d, err
}
func (v *Providers) variables(ctx context.Context, p Project) (map[string]string, error) {
	if p.Provider == "vercel" {
		return v.vercelVariables(ctx, p)
	}
	var raw map[string]*string
	args := append([]string{"variable", "list"}, selectors(p)...)
	args = append(args, "--json")
	if err := v.decode(ctx, "railway", &raw, args...); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for k, val := range raw {
		if val != nil {
			result[k] = *val
		}
	}
	return result, nil
}

type preflight struct {
	Observation Observation
	Secrets     []string
}

func (v *Providers) inspect(ctx context.Context, p Project) (preflight, error) {
	if p.Provider == "vercel" {
		return v.inspectVercel(ctx, p)
	}
	result := preflight{Observation: Observation{At: time.Now().UTC(), Provider: "railway", RailwayPlan: "Unknown", NeonPlan: "Unknown"}}
	var account struct {
		Project struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspaceId"`
			Services    struct {
				Edges []struct {
					Node struct {
						ID string `json:"id"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"services"`
			Environments struct {
				Edges []struct {
					Node struct {
						ID string `json:"id"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"environments"`
		} `json:"project"`
		Workspace struct {
			Plan     string `json:"plan"`
			Customer *struct {
				Trial      *bool           `json:"isTrialing"`
				Subscribed *bool           `json:"isUsageSubscriber"`
				Credit     *float64        `json:"creditBalance"`
				Payment    json.RawMessage `json:"defaultPaymentMethodId"`
			} `json:"customer"`
		} `json:"workspace"`
	}
	q := `query($project:String!,$workspace:String!){project(id:$project){id workspaceId services{edges{node{id}}} environments{edges{node{id}}}} workspace(workspaceId:$workspace){plan customer{isTrialing isUsageSubscriber creditBalance defaultPaymentMethodId}}}`
	if err := v.graph(ctx, q, map[string]string{"project": p.RailwayProject, "workspace": p.Workspace}, &account); err != nil {
		return result, err
	}
	serviceOK, envOK := false, false
	for _, e := range account.Project.Services.Edges {
		serviceOK = serviceOK || e.Node.ID == p.Service
	}
	for _, e := range account.Project.Environments.Edges {
		envOK = envOK || e.Node.ID == p.Environment
	}
	if account.Project.ID != p.RailwayProject || account.Project.WorkspaceID != p.Workspace || !serviceOK || !envOK {
		return result, errors.New("Railway resource ownership does not match the registered project")
	}
	c := account.Workspace.Customer
	if c == nil || c.Trial == nil || c.Subscribed == nil || c.Credit == nil || len(c.Payment) == 0 {
		return result, errors.New("Railway billing state is unavailable")
	}
	if *c.Trial {
		result.Observation.RailwayPlan = "Trial"
	} else if account.Workspace.Plan == "FREE" {
		result.Observation.RailwayPlan = "Free"
	} else {
		result.Observation.RailwayPlan = "Paid or unverified"
	}
	result.Observation.ComputePlan = result.Observation.RailwayPlan
	var live struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		DeploymentID string `json:"deploymentId"`
	}
	args := append([]string{"service", "status"}, selectors(p)...)
	args = append(args, "--json")
	if err := v.decode(ctx, "railway", &live, args...); err != nil {
		return result, err
	}
	if live.ID != p.Service {
		return result, errors.New("service identity mismatch")
	}
	result.Observation.ServiceState = live.Status
	result.Observation.DeploymentID = live.DeploymentID
	neon, host, err := v.inspectNeon(ctx, p)
	result.Observation.NeonPlan = neon.NeonPlan
	result.Observation.DatabaseState = neon.DatabaseState
	if err != nil {
		return result, err
	}
	vars, err := v.variables(ctx, p)
	if err != nil {
		return result, err
	}
	dsn, err := url.Parse(vars["DATABASE_URL"])
	if err != nil || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") {
		return result, errors.New("DATABASE_URL is missing or invalid; alpha supports the validated PostgreSQL contract")
	}
	parts := strings.SplitN(host, ".", 2)
	pooler := ""
	if len(parts) == 2 {
		pooler = parts[0] + "-pooler." + parts[1]
	}
	if dsn.Hostname() != host && dsn.Hostname() != pooler {
		return result, errors.New("application DATABASE_URL does not match the registered Neon endpoint")
	}
	result.Observation.DatabaseBinding = "provider_value_matches_endpoint"
	result.Secrets = secretValues(vars)
	switch {
	case *c.Subscribed || string(c.Payment) != "null":
		result.Observation.Reason = "当前计费状态不符合内部版的不自动收费路径"
	case !*c.Trial:
		result.Observation.Reason = "此内部版尚未验收正式 Free 或付费账号的发布；保留只读能力"
	case !p.AllowTrial:
		result.Observation.Reason = "该项目尚未明确接受 Trial 试用条件"
	case *c.Credit <= 0:
		result.Observation.Reason = "Trial 额度不足，停止发布"
	case result.Observation.NeonPlan != "free":
		result.Observation.Reason = "数据库组织不是已验证的 Free 计划"
	default:
		result.Observation.Eligible = true
	}
	return result, nil
}

var secretName = regexp.MustCompile(`(?i)(password|token|secret|api.?key|database_url|dsn|private.?key)`)
var credentialURL = regexp.MustCompile(`(?i)(?:postgres(?:ql)?|mysql|redis|https?)://[^\s"<>]*@[^\s"<>]*`)
var inlineSecret = regexp.MustCompile(`(?i)(?:bearer\s+[a-z0-9._~+/=-]+|(?:password|token|secret|api_key)\s*[:=]\s*["']?[^\s,"']+)`)

func secretValues(vars map[string]string) []string {
	result := []string{}
	for k, v := range vars {
		if secretName.MatchString(k) && len(v) >= 4 {
			result = append(result, v)
			if u, e := url.Parse(v); e == nil && u.User != nil {
				if pass, ok := u.User.Password(); ok && len(pass) >= 4 {
					result = append(result, pass)
				}
			}
		}
	}
	return result
}
func redact(s string, secrets []string) string {
	for _, v := range secrets {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
			encoded, _ := json.Marshal(v)
			s = strings.ReplaceAll(s, string(encoded[1:len(encoded)-1]), "[REDACTED]")
			s = strings.ReplaceAll(s, url.QueryEscape(v), "[REDACTED]")
		}
	}
	s = credentialURL.ReplaceAllString(s, "[REDACTED_URL]")
	return inlineSecret.ReplaceAllString(s, "[REDACTED]")
}
func (v *Providers) logs(ctx context.Context, p Project) ([]string, error) {
	if p.Provider == "vercel" {
		return v.vercelLogs(ctx, p)
	}
	vars, err := v.variables(ctx, p)
	if err != nil {
		return nil, errors.New("cannot obtain redaction context; logs withheld")
	}
	args := append([]string{"logs"}, selectors(p)...)
	args = append(args, "--deployment", "--lines", "40", "--json")
	b, err := v.call(ctx, "railway", args...)
	if err != nil {
		return nil, err
	}
	result := []string{}
	secrets := secretValues(vars)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, redact(line, secrets))
		}
	}
	return result, nil
}
func checkApplication(ctx context.Context, base string) []Check {
	client := http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	results := []Check{}
	for _, path := range []string{"/healthz", "/readyz"} {
		c := Check{Path: path}
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			c.Error = "invalid application URL"
			results = append(results, c)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			c.Error = "访问未完成，当前可用性待确认"
			results = append(results, c)
			continue
		}
		c.HTTP = resp.StatusCode
		c.OK = resp.StatusCode == 200
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		var detail struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(body, &detail) == nil && len(detail.Version) < 100 {
			c.Version = detail.Version
		}
		if !c.OK {
			c.Error = "应用检查未通过；发布结果与访问结果分别记录"
		}
		results = append(results, c)
	}
	return results
}

func (v *Providers) inspectNeon(ctx context.Context, p Project) (Observation, string, error) {
	observation := Observation{NeonPlan: "Unknown"}
	var project struct {
		Project struct {
			ID    string `json:"id"`
			OrgID string `json:"org_id"`
		} `json:"project"`
	}
	if err := v.decode(ctx, "neon", &project, "api", "/projects/"+p.NeonProject, "--method", "GET"); err != nil {
		return observation, "", err
	}
	if project.Project.ID != p.NeonProject || project.Project.OrgID != p.NeonOrg {
		return observation, "", errors.New("Neon project ownership mismatch")
	}
	var orgs []struct {
		ID   string `json:"id"`
		Plan string `json:"plan"`
	}
	if err := v.decode(ctx, "neon", &orgs, "orgs", "list"); err != nil {
		return observation, "", err
	}
	for _, o := range orgs {
		if o.ID == p.NeonOrg {
			observation.NeonPlan = o.Plan
		}
	}
	var endpoint struct {
		Endpoint struct {
			ID    string `json:"id"`
			Host  string `json:"host"`
			State string `json:"current_state"`
		} `json:"endpoint"`
	}
	if err := v.decode(ctx, "neon", &endpoint, "api", "/projects/"+p.NeonProject+"/endpoints/"+p.NeonEndpoint, "--method", "GET"); err != nil {
		return observation, "", err
	}
	if endpoint.Endpoint.ID != p.NeonEndpoint || endpoint.Endpoint.Host == "" {
		return observation, "", errors.New("Neon endpoint identity mismatch")
	}
	observation.DatabaseState = endpoint.Endpoint.State
	return observation, endpoint.Endpoint.Host, nil
}
