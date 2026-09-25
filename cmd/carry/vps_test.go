package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVPSBindingAndComposeKeepSecretsOutOfProject(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Dockerfile"), []byte("FROM scratch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := Project{Name: "loop", Provider: "vps", Source: source, URL: "https://loop.example.test", VPSHost: "known-host", VPSPort: 8080,
		VPSDataPath: "/data/loop-runs", VPSEnv: map[string]string{"OPENAI_MODEL": "glm-4.7-flash"}, AllowPublish: true}
	if err := validateProject(&p); err != nil {
		t.Fatal(err)
	}
	store, err := newStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.register(p); err != nil {
		t.Fatal(err)
	}
	if _, err = store.saveSecret(p.Name, "OPENAI_API_KEY", []byte("private$test")); err != nil {
		t.Fatal(err)
	}
	v := &Providers{Store: store}
	image := vpsImage(p, "carry:123-safe")
	raw, err := v.vpsCompose(p, image, "carry:123-safe")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Image       string            `json:"image"`
			Environment map[string]string `json:"environment"`
			Command     []string          `json:"command"`
			Ports       []string          `json:"ports"`
			Volumes     []string          `json:"volumes"`
		} `json:"services"`
	}
	if err = json.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	app, proxy := compose.Services["app"], compose.Services["proxy"]
	if app.Image != image || app.Environment["OPENAI_API_KEY"] != "private$$test" || app.Environment["OPENAI_MODEL"] != "glm-4.7-flash" ||
		len(app.Volumes) != 1 || !strings.Contains(app.Volumes[0], ":/data/loop-runs") || proxy.Command[3] != "loop.example.test" || len(proxy.Ports) != 2 {
		t.Fatal("VPS Compose did not isolate the app, proxy, data or escaped secret")
	}
	if docker, err := exec.LookPath("docker"); err == nil {
		file := filepath.Join(root, "compose.json")
		if err = os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(docker, "compose", "-f", file, "config", "--format", "json").Output()
		if err != nil {
			t.Fatal("Docker Compose rejected the generated config", err)
		}
		var resolved struct {
			Services map[string]struct {
				Environment map[string]string `json:"environment"`
			} `json:"services"`
		}
		if json.Unmarshal(output, &resolved) != nil || resolved.Services["app"].Environment["OPENAI_API_KEY"] != "private$$test" {
			t.Fatal("Docker Compose interpolated an escaped secret")
		}
	}
	var unsafe envFlags
	if unsafe.Set("OPENAI_API_KEY=visible") == nil {
		t.Fatal("secret accepted in public project metadata")
	}
	p.VPSHost = "-oProxyCommand=bad"
	if validateReferences(&p) == nil {
		t.Fatal("unsafe SSH host accepted")
	}
}

func TestVPSReadbackMatchesOnlyBoundApplication(t *testing.T) {
	p := Project{Name: "loop", Provider: "vps", VPSHost: "known-host"}
	const id = "aabbccddeeff"
	v := &Providers{Run: func(_ context.Context, tool string, args []string) ([]byte, error) {
		if tool != "docker" || len(args) < 4 || args[0] != "--host" || args[1] != "ssh://known-host" {
			t.Fatal("Docker did not use the bound SSH host")
		}
		switch args[2] {
		case "ps":
			return []byte(id + "\n"), nil
		case "container":
			return []byte(`{"Id":"aabbccddeeff0000","Config":{"Labels":{"carry.project":"loop","carry.marker":"carry:accepted"}},"State":{"Status":"running"}}`), nil
		default:
			t.Fatalf("unexpected Docker command: %s", args[2])
			return nil, nil
		}
	}}
	deployments, err := v.vpsDeployments(context.Background(), p)
	if err != nil || len(deployments) != 1 || deployments[0].Status != "READY" || deployments[0].Meta.Message != "carry:accepted" {
		t.Fatalf("VPS deployment readback failed: %v %#v", err, deployments)
	}
}

func TestVPSPreflightBlocksAnExistingWebListener(t *testing.T) {
	p := Project{Name: "loop", Provider: "vps", VPSHost: "known-host"}
	busy := true
	v := &Providers{Run: func(_ context.Context, tool string, args []string) ([]byte, error) {
		if tool == "ssh" {
			out := "Filesystem 1M-blocks Used Available Use% Mounted on\n/dev/root 40000 5000 35000 13% /\n              total        used        free      shared  buff/cache   available\nMem:          1973         400        1000           0         573        1500\n"
			if busy {
				out += "LISTEN 0 4096 0.0.0.0:80 0.0.0.0:*\n"
			}
			return []byte(out), nil
		}
		if tool != "docker" {
			t.Fatalf("unexpected tool %s", tool)
		}
		if args[2] == "info" {
			return []byte(`{"Architecture":"x86_64","NCPU":2,"MemTotal":2069409792}`), nil
		}
		if args[2] == "ps" {
			return []byte(""), nil
		}
		t.Fatalf("unexpected Docker command %s", args[2])
		return nil, nil
	}}
	blocked, err := v.inspectVPS(context.Background(), p)
	if err != nil || blocked.Observation.Eligible || !strings.Contains(blocked.Observation.Reason, "80") {
		t.Fatalf("occupied web port was not blocked: %v %#v", err, blocked)
	}
	busy = false
	ready, err := v.inspectVPS(context.Background(), p)
	if err != nil || !ready.Observation.Eligible {
		t.Fatalf("free host was not eligible: %v %#v", err, ready)
	}
}

func TestVPSRollbackRestoresEarlierImageWithoutRepeatingBuild(t *testing.T) {
	store, err := newStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := Project{Name: "loop", Provider: "vps", VPSHost: "known-host", VPSPort: 8080, URL: "https://127.0.0.1:1", AllowPublish: true}
	old, err := newOperation(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	old.State, old.SourceHash = "deployed", "old-source"
	if err = store.saveOp(&old); err != nil {
		t.Fatal(err)
	}
	current, err := newOperation(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	current.State, current.SourceHash = "deployed", "current-source"
	if err = store.saveOp(&current); err != nil {
		t.Fatal(err)
	}
	marker := current.Marker
	v := &Providers{Store: store, Run: func(_ context.Context, tool string, args []string) ([]byte, error) {
		if tool != "docker" || len(args) < 3 {
			t.Fatal("unexpected rollback tool call")
		}
		switch args[2] {
		case "ps":
			return []byte("aabbccddeeff\n"), nil
		case "container":
			return []byte(`{"Id":"aabbccddeeff0000","Config":{"Labels":{"carry.project":"loop","carry.marker":"` + marker + `"}},"State":{"Status":"running"}}`), nil
		case "image":
			if len(args) < 5 || args[4] != vpsImage(p, old.Marker) {
				t.Fatal("rollback did not inspect the earlier image")
			}
			return []byte("image-id"), nil
		case "compose":
			if len(args) < 5 {
				t.Fatal("rollback Compose file was missing")
			}
			var config struct {
				Services map[string]struct {
					Image  string            `json:"image"`
					Labels map[string]string `json:"labels"`
				} `json:"services"`
			}
			if json.Unmarshal(mustRead(t, args[4]), &config) != nil || config.Services["app"].Image != vpsImage(p, old.Marker) {
				t.Fatal("rollback did not select the earlier application image")
			}
			marker = config.Services["app"].Labels["carry.marker"]
			return []byte(""), nil
		}
		t.Fatalf("unexpected rollback Docker command: %s", args[2])
		return nil, nil
	}}
	e := &Engine{Store: store, Providers: v}
	op, unlock, err := e.begin(p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = e.rollbackVPS(context.Background(), p, op); err != nil || op.State != "deployed" || op.SourceHash != old.SourceHash || marker != op.Marker {
		t.Fatalf("rollback did not reconcile the restored image: %v %#v", err, op)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
