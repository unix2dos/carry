package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type vercelDeployment struct {
	ID            string            `json:"id"`
	UID           string            `json:"uid"`
	State         string            `json:"readyState"`
	Target        string            `json:"target"`
	Alias         []string          `json:"alias"`
	AliasAssigned json.RawMessage   `json:"aliasAssigned"`
	Meta          map[string]string `json:"meta"`
}

type vercelProject struct {
	ID        string                      `json:"id"`
	AccountID string                      `json:"accountId"`
	Targets   map[string]vercelDeployment `json:"targets"`
}

type vercelEnv struct {
	ID                   string   `json:"id"`
	Key                  string   `json:"key"`
	Value                string   `json:"value"`
	Type                 string   `json:"type"`
	Visibility           string   `json:"visibility"`
	Target               []string `json:"target"`
	UpdatedAt            int64    `json:"updatedAt"`
	Comment              string   `json:"comment"`
	GitBranch            string   `json:"gitBranch"`
	ConfigurationID      string   `json:"configurationId"`
	CustomEnvironmentIDs []string `json:"customEnvironmentIds"`
}

func (item vercelEnv) production() bool {
	for _, target := range item.Target {
		if target == "production" {
			return true
		}
	}
	return false
}

func (v *Providers) vercelCall(ctx context.Context, p Project, input []byte, args ...string) ([]byte, error) {
	if v.VercelConfig == "" {
		return nil, errors.New("supply --vercel-config for the official CLI authentication directory")
	}
	args = append(args, "--scope", p.VercelTeam, "--global-config", v.VercelConfig, "--non-interactive")
	return v.callInput(ctx, "vercel", input, args...)
}

func (v *Providers) vercelAPI(ctx context.Context, p Project, path, method string, input []byte, out any) error {
	args := []string{"api", path, "--method", method, "--raw"}
	if input != nil {
		args = append(args, "--input", "-")
	}
	data, err := v.vercelCall(ctx, p, input, args...)
	if err != nil {
		return err
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return errors.New("Vercel returned invalid JSON")
	}
	return nil
}

func (v *Providers) vercelAccount(ctx context.Context, p Project) (vercelProject, string, error) {
	var project vercelProject
	var team struct {
		ID      string `json:"id"`
		Billing struct {
			Plan string `json:"plan"`
		} `json:"billing"`
		Membership struct {
			Role string `json:"role"`
		} `json:"membership"`
	}
	if err := v.vercelAPI(ctx, p, "/v2/teams/"+p.VercelTeam, "GET", nil, &team); err != nil {
		return project, "", err
	}
	if team.ID != p.VercelTeam || team.Membership.Role != "OWNER" {
		return project, "", errors.New("Vercel team ownership could not be verified")
	}
	if err := v.vercelAPI(ctx, p, "/v9/projects/"+p.VercelProject, "GET", nil, &project); err != nil {
		return project, team.Billing.Plan, err
	}
	if project.ID != p.VercelProject || project.AccountID != p.VercelTeam {
		return project, team.Billing.Plan, errors.New("Vercel project ownership mismatch")
	}
	return project, team.Billing.Plan, nil
}

func (v *Providers) vercelEnvs(ctx context.Context, p Project) ([]vercelEnv, error) {
	var response struct {
		Envs []vercelEnv `json:"envs"`
	}
	if err := v.vercelAPI(ctx, p, "/v10/projects/"+p.VercelProject+"/env", "GET", nil, &response); err != nil {
		return nil, err
	}
	if response.Envs == nil {
		return nil, errors.New("Vercel environment metadata is unavailable")
	}
	return response.Envs, nil
}

func (v *Providers) vercelVariables(ctx context.Context, p Project) (map[string]string, error) {
	if v.Store == nil {
		return nil, errors.New("local secret store is unavailable")
	}
	envs, err := v.vercelEnvs(ctx, p)
	if err != nil {
		return nil, err
	}
	refs, err := v.Store.secretRecords(p.Name)
	if err != nil {
		return nil, err
	}
	for _, versions := range refs {
		for _, version := range versions {
			if version.SyncState == "unknown" || version.SyncState == "applying" {
				return nil, errors.New("a Secret write is unresolved; reconcile it before publishing or reading logs")
			}
		}
	}
	values := map[string]string{}
	for _, item := range envs {
		if !item.production() {
			continue
		}
		if _, exists := values[item.Key]; exists {
			return nil, errors.New("ambiguous production environment variables")
		}
		if item.Type == "sensitive" || item.Visibility == "secret" {
			// Presence is observable even when the provider keeps the value write-only.
			// Source deployment retains that configuration; a local cache is optional.
			values[item.Key] = ""
			versions := refs[item.Key]
			if len(versions) == 0 {
				continue
			}
			last := versions[len(versions)-1]
			if last.SyncState != "synced" || last.RemoteID != item.ID || last.RemoteUpdatedAt != item.UpdatedAt || last.Marker != item.Comment || item.UpdatedAt <= 0 {
				continue
			}
			value, err := v.Store.secretValue(p.Name, last.Ref)
			if err != nil {
				return nil, err
			}
			values[item.Key] = string(value)
			clear(value)
		} else {
			if item.Key == "DATABASE_URL" {
				return nil, errors.New("Vercel DATABASE_URL must remain a Secret")
			}
			values[item.Key] = item.Value
		}
	}
	return values, nil
}

func databaseEndpointMatches(raw, host string) bool {
	dsn, err := url.Parse(raw)
	if err != nil || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") || host == "" {
		return false
	}
	parts := strings.SplitN(host, ".", 2)
	pooler := ""
	if len(parts) == 2 {
		pooler = parts[0] + "-pooler." + parts[1]
	}
	return dsn.Hostname() == host || dsn.Hostname() == pooler
}

func (v *Providers) inspectVercel(ctx context.Context, p Project) (preflight, error) {
	result := preflight{Observation: Observation{At: time.Now().UTC(), Provider: "vercel", ComputePlan: "Unknown", NeonPlan: "Unknown", DatabaseBinding: "unverified"}}
	project, plan, err := v.vercelAccount(ctx, p)
	if err != nil {
		return result, err
	}
	result.Observation.ComputePlan = plan
	production := project.Targets["production"]
	result.Observation.ServiceState = production.State
	result.Observation.DeploymentID = production.ID
	address, _ := url.Parse(p.URL)
	belongs := false
	for _, alias := range production.Alias {
		belongs = belongs || alias == address.Host
	}
	if !belongs {
		return result, errors.New("application URL is not an alias of the bound Vercel production project")
	}
	neon, host, err := v.inspectNeon(ctx, p)
	if err != nil {
		return result, err
	}
	result.Observation.NeonPlan = neon.NeonPlan
	result.Observation.DatabaseState = neon.DatabaseState
	result.Observation.DatabaseBinding = neon.DatabaseBinding
	if plan != "hobby" {
		result.Observation.Reason = "此 Vercel 路径只支持已核对的 Hobby，停止付费或未知计划的发布"
		return result, nil
	}
	if !p.AllowHobby {
		result.Observation.Reason = "需要明确接受 Hobby 个人非商业用途条件"
		return result, nil
	}
	if p.hasNeon() && neon.NeonPlan != "free" {
		result.Observation.Reason = "数据库组织不是已验证的 Free 计划"
		return result, nil
	}
	vars, err := v.vercelVariables(ctx, p)
	if err != nil {
		result.Observation.Reason = err.Error()
		return result, nil
	}
	if p.hasNeon() {
		databaseURL, configured := vars["DATABASE_URL"]
		if !configured {
			result.Observation.Reason = "Vercel production 未配置 DATABASE_URL"
			return result, nil
		}
		if databaseURL != "" && !databaseEndpointMatches(databaseURL, host) {
			result.Observation.Reason = "本地保存的 DATABASE_URL 与登记的 Neon 连接端点不匹配"
			return result, nil
		}
		result.Observation.DatabaseBinding = "provider_secret_retained_not_readable"
		if databaseURL != "" {
			result.Observation.DatabaseBinding = "last_write_and_metadata_match_not_runtime_identity"
		}
	}
	result.Secrets = secretValues(vars)
	result.Observation.Eligible = true
	return result, nil
}

func aliasAssigned(value json.RawMessage) bool {
	if bytes.Equal(value, []byte("true")) {
		return true
	}
	var timestamp int64
	return json.Unmarshal(value, &timestamp) == nil && timestamp > 0
}

func (v *Providers) vercelDeployments(ctx context.Context, p Project) ([]Deployment, error) {
	var response struct {
		Deployments []vercelDeployment `json:"deployments"`
	}
	if err := v.vercelAPI(ctx, p, "/v7/deployments?projectId="+url.QueryEscape(p.VercelProject)+"&limit=100&target=production", "GET", nil, &response); err != nil {
		return nil, err
	}
	result := []Deployment{}
	for _, item := range response.Deployments {
		if item.Target != "production" {
			continue
		}
		d := Deployment{ID: item.UID, Status: item.State, AliasAssigned: aliasAssigned(item.AliasAssigned)}
		d.Meta.Message = item.Meta["ship_operation"]
		result = append(result, d)
	}
	return result, nil
}

func (v *Providers) submit(ctx context.Context, p Project, stage, marker string) error {
	if p.Provider == "vercel" {
		_, err := v.vercelCall(ctx, p, nil, "deploy", stage, "--project", p.VercelProject, "--prod", "--yes", "--json", "--no-wait", "--meta", "ship_operation="+marker)
		return err
	}
	args := append([]string{"up", stage, "--path-as-root"}, selectors(p)...)
	args = append(args, "--detach", "--json", "--message", marker)
	_, err := v.call(ctx, "railway", args...)
	return err
}

func prepareVercelSource(stage string, p Project) error {
	var config map[string]json.RawMessage
	if err := readJSON(filepath.Join(stage, "vercel.json"), &config); err != nil {
		return errors.New("Vercel container source requires vercel.json with framework set to container")
	}
	var framework string
	if json.Unmarshal(config["framework"], &framework) != nil || framework != "container" {
		return errors.New("Vercel framework must be container")
	}
	// Keep the alpha's deployment behavior reviewable; advanced project configuration needs separate validation.
	if len(config) != 1 {
		return errors.New("this alpha accepts only the container framework field in vercel.json")
	}
	if _, err := os.Stat(filepath.Join(stage, "Dockerfile.vercel")); err != nil {
		return errors.New("Dockerfile.vercel is missing")
	}
	if err := os.Mkdir(filepath.Join(stage, ".vercel"), 0700); err != nil {
		return err
	}
	// Provider link metadata is generated locally and excluded by Vercel from the upload.
	return atomicJSON(filepath.Join(stage, ".vercel", "project.json"), map[string]string{"projectId": p.VercelProject, "orgId": p.VercelTeam})
}

func (v *Providers) vercelLogs(ctx context.Context, p Project) ([]string, error) {
	if _, _, err := v.vercelAccount(ctx, p); err != nil {
		return nil, err
	}
	vars, err := v.vercelVariables(ctx, p)
	if err != nil {
		return nil, errors.New("Vercel Secret metadata could not be checked or a write is unresolved; logs withheld")
	}
	data, err := v.vercelCall(ctx, p, nil, "logs", "--project", p.VercelProject, "--environment", "production", "--limit", "40", "--json", "--no-follow")
	if err != nil {
		return nil, err
	}
	lines := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, redact(line, secretValues(vars)))
		}
	}
	return lines, nil
}
