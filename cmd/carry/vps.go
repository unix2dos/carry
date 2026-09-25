package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var vpsHostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

type envFlags map[string]string

func (e *envFlags) String() string { return "" }
func (e *envFlags) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok || !validVPSEnv(map[string]string{key: val}) {
		return errors.New("use a non-secret uppercase KEY=VALUE; save credentials with 'secret save'")
	}
	if *e == nil {
		*e = map[string]string{}
	}
	(*e)[key] = val
	return nil
}

func validVPSEnv(values map[string]string) bool {
	for key, value := range values {
		if !secretKeyPattern.MatchString(key) || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") ||
			strings.HasSuffix(key, "_KEY") || strings.HasSuffix(key, "_TOKEN") || strings.HasSuffix(key, "_PASSWORD") || strings.HasSuffix(key, "_SECRET") || key == "DATABASE_URL" {
			return false
		}
	}
	return true
}

func validVPSDataPath(value string) bool {
	return path.IsAbs(value) && value != "/" && path.Clean(value) == value && !strings.ContainsAny(value, ":\r\n\x00")
}

func (v *Providers) vpsCall(ctx context.Context, p Project, args ...string) ([]byte, error) {
	return v.call(ctx, "docker", append([]string{"--host", "ssh://" + p.VPSHost}, args...)...)
}

func (v *Providers) vpsHostResources(ctx context.Context, p Project) (diskMiB, availableMiB int, webPortsBusy bool, err error) {
	b, err := v.call(ctx, "ssh", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oConnectTimeout=5", p.VPSHost,
		"df -Pm /var/lib/docker && free -m && ss -H -ltn")
	if err != nil {
		return 0, 0, false, err
	}
	for index, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if index == 1 && len(fields) >= 4 {
			diskMiB, _ = strconv.Atoi(fields[3])
		}
		if len(fields) >= 7 && fields[0] == "Mem:" {
			availableMiB, _ = strconv.Atoi(fields[len(fields)-1])
		}
		if len(fields) >= 4 && fields[0] == "LISTEN" && (strings.HasSuffix(fields[3], ":80") || strings.HasSuffix(fields[3], ":443")) {
			webPortsBusy = true
		}
	}
	if diskMiB < 1 || availableMiB < 1 {
		return 0, 0, false, errors.New("VPS disk or memory readback is incomplete")
	}
	return diskMiB, availableMiB, webPortsBusy, nil
}

func (v *Providers) inspectVPS(ctx context.Context, p Project) (preflight, error) {
	result := preflight{Observation: Observation{At: time.Now().UTC(), Provider: "vps", ComputePlan: "user_owned_vps", DatabaseBinding: "not_managed", ServiceState: "not_deployed"}}
	b, err := v.vpsCall(ctx, p, "info", "--format", "{{json .}}")
	if err != nil {
		result.Observation.Reason = "Docker on the bound SSH host is unavailable"
		return result, err
	}
	var info struct {
		Architecture string `json:"Architecture"`
		NCPU         int    `json:"NCPU"`
		MemTotal     int64  `json:"MemTotal"`
	}
	if json.Unmarshal(b, &info) != nil || info.Architecture == "" || info.NCPU < 1 || info.MemTotal < 1 {
		return result, errors.New("VPS Docker information is incomplete")
	}
	result.Observation.ComputePlan = fmt.Sprintf("%s %d CPU %d MiB", info.Architecture, info.NCPU, info.MemTotal>>20)
	deployments, err := v.vpsDeployments(ctx, p)
	if err != nil {
		return result, err
	}
	if len(deployments) > 1 {
		return result, errors.New("more than one Carry application container matches this VPS binding")
	}
	diskMiB, availableMiB, webPortsBusy, err := v.vpsHostResources(ctx, p)
	if err != nil {
		return result, err
	}
	if diskMiB < 2048 || availableMiB < 256 {
		result.Observation.Reason = "VPS needs at least 2 GiB free disk and 256 MiB available memory before publishing"
		return result, nil
	}
	if len(deployments) == 0 && webPortsBusy {
		result.Observation.Reason = "ports 80 or 443 are already in use by an existing service"
		return result, nil
	}
	if len(deployments) == 1 {
		result.Observation.ServiceState = strings.ToLower(deployments[0].Status)
		result.Observation.DeploymentID = deployments[0].ID
	}
	result.Observation.Eligible = true
	return result, nil
}

func (v *Providers) vpsDeployments(ctx context.Context, p Project) ([]Deployment, error) {
	b, err := v.vpsCall(ctx, p, "ps", "-a", "--filter", "label=com.docker.compose.project=carry-"+p.Name,
		"--filter", "label=com.docker.compose.service=app", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	result := []Deployment{}
	for _, id := range strings.Fields(string(b)) {
		if !regexp.MustCompile(`^[a-f0-9]{12,64}$`).MatchString(id) || len(result) >= 2 {
			return nil, errors.New("VPS returned unexpected container identities")
		}
		body, err := v.vpsCall(ctx, p, "container", "inspect", id, "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		var detail struct {
			ID     string `json:"Id"`
			Config struct {
				Labels map[string]string `json:"Labels"`
			} `json:"Config"`
			State struct {
				Status string `json:"Status"`
			} `json:"State"`
		}
		if json.Unmarshal(body, &detail) != nil || !strings.HasPrefix(detail.ID, id) || detail.Config.Labels["carry.project"] != p.Name {
			return nil, errors.New("VPS application container metadata is invalid")
		}
		d := Deployment{ID: detail.ID, Status: "FAILED"}
		if detail.State.Status == "running" {
			d.Status = "READY"
		}
		d.Meta.Message = detail.Config.Labels["carry.marker"]
		result = append(result, d)
	}
	return result, nil
}

func (v *Providers) vpsLogs(ctx context.Context, p Project) ([]string, error) {
	deployments, err := v.vpsDeployments(ctx, p)
	if err != nil || len(deployments) != 1 {
		return nil, errors.New("one known VPS application container is required for logs")
	}
	if v.Docker == "" {
		return nil, errors.New("Docker CLI is not configured")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, v.Docker, "--host", "ssh://"+p.VPSHost, "logs", "--tail", "40", deployments[0].ID)
	cmd.Env = cleanEnv()
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if cmd.Run() != nil || stdout.exceeded || stderr.exceeded {
		return nil, errors.New("VPS logs could not be read")
	}
	return strings.Split(strings.TrimSpace(stdout.String()+stderr.String()), "\n"), nil
}

func vpsImage(p Project, marker string) string {
	return "carry-" + p.Name + ":" + strings.ReplaceAll(marker, ":", "-")
}

func (v *Providers) vpsCompose(p Project, image, marker string) ([]byte, error) {
	environment := map[string]string{}
	for key, value := range p.VPSEnv {
		environment[key] = strings.ReplaceAll(value, "$", "$$")
	}
	records, err := v.Store.secretRecords(p.Name)
	if err != nil {
		return nil, err
	}
	for key, versions := range records {
		value := versions[len(versions)-1].Value
		if value == "" {
			return nil, errors.New("VPS secret is empty")
		}
		environment[key] = strings.ReplaceAll(value, "$", "$$")
	}
	name := "carry-" + p.Name
	app := map[string]any{
		"image": image, "restart": "unless-stopped", "container_name": name + "-app",
		"labels": map[string]string{"carry.project": p.Name, "carry.marker": marker}, "environment": environment,
	}
	volumes := map[string]any{name + "-caddy": map[string]any{}}
	if p.VPSDataPath != "" {
		app["volumes"] = []string{name + "-data:" + p.VPSDataPath}
		volumes[name+"-data"] = map[string]any{}
	}
	proxy := map[string]any{
		"image": "caddy:2.10.2-alpine@sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d", "restart": "unless-stopped", "container_name": name + "-proxy",
		"command": []string{"caddy", "reverse-proxy", "--from", strings.TrimPrefix(p.URL, "https://"), "--to", fmt.Sprintf("app:%d", p.VPSPort)},
		"ports":   []string{"80:80", "443:443"}, "volumes": []string{name + "-caddy:/data"},
	}
	return json.Marshal(map[string]any{"services": map[string]any{"app": app, "proxy": proxy}, "volumes": volumes})
}

func (v *Providers) transferVPSImage(ctx context.Context, p Project, image string) error {
	if v.Docker == "" {
		return errors.New("Docker CLI is not configured")
	}
	transferCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	save := exec.CommandContext(transferCtx, v.Docker, "image", "save", image)
	load := exec.CommandContext(transferCtx, v.Docker, "--host", "ssh://"+p.VPSHost, "image", "load")
	save.Env, load.Env = cleanEnv(), cleanEnv()
	save.Stdout, load.Stdin = writer, reader
	var saveErr, loadErr, loadOut limitedBuffer
	save.Stderr, load.Stderr, load.Stdout = &saveErr, &loadErr, &loadOut
	if err = load.Start(); err != nil {
		return errors.New("VPS image load could not start")
	}
	if err = save.Start(); err != nil {
		writer.Close()
		load.Wait()
		return errors.New("local image transfer could not start")
	}
	reader.Close()
	writer.Close()
	saveResult := save.Wait()
	loadResult := load.Wait()
	if saveResult != nil || loadResult != nil || saveErr.exceeded || loadErr.exceeded || loadOut.exceeded {
		return errors.New("VPS image transfer result is unknown; inspect the remote image before retrying")
	}
	return nil
}

func (v *Providers) submitVPS(ctx context.Context, p Project, stage, marker string) error {
	b, err := v.vpsCall(ctx, p, "info", "--format", "{{.Architecture}}")
	if err != nil {
		return err
	}
	architecture := strings.TrimSpace(string(b))
	platform := ""
	switch architecture {
	case "x86_64", "amd64":
		platform = "linux/amd64"
	case "aarch64", "arm64":
		platform = "linux/arm64"
	default:
		return errors.New("unsupported VPS CPU architecture")
	}
	image := vpsImage(p, marker)
	if _, err = v.call(ctx, "docker", "buildx", "build", "--platform", platform, "--load", "--tag", image, stage); err != nil {
		return err
	}
	if err = v.transferVPSImage(ctx, p, image); err != nil {
		return err
	}
	return v.applyVPSCompose(ctx, p, image, marker)
}

func (v *Providers) applyVPSCompose(ctx context.Context, p Project, image, marker string) error {
	compose, err := v.vpsCompose(p, image, marker)
	if err != nil {
		return err
	}
	_, err = v.callInput(ctx, "docker", compose, "--host", "ssh://"+p.VPSHost, "compose", "-f", "-", "-p", "carry-"+p.Name, "up", "-d", "--no-build", "--pull", "missing")
	return err
}
