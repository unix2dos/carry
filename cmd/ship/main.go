package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

const help = `ship — internal alpha, existing Railway + Neon project bindings

Global flags (before command):
  --state-dir PATH       Private local records (default: config/ship; existing config/upok reused)
  --railway-bin PATH     Official Railway CLI binary; no global installation is performed
  --neon-bin PATH        Official Neon CLI
  --neon-config PATH     Existing private Neon authentication directory

Commands:
  register --name NAME --source DIR --url HTTPS_ORIGIN
    --workspace ID --railway-project ID --service ID --environment ID
    --neon-org ID --neon-project ID --neon-endpoint ID
    [--allow-publish] [--allow-trial]
  list
  status NAME           Live read-only ownership, account plan and resource checks
  check NAME            GET /healthz and /readyz; no writes to business data
  logs NAME             Last 40 deployment log lines; known credentials redacted
  publish NAME [--detach]  Upload a captured source directory to the bound service
  reconcile NAME [--wait]  Find the existing operation by its deployment marker
  history NAME
  secret save NAME KEY --stdin  Store a value in local macOS Keychain; no cloud changes
  secret list NAME       List saved secret metadata, never values
  secret check NAME      Check local Keychain access; does not verify cloud values
  authorize NAME [--allow-publish=true|false] [--allow-trial=true|false]
  serve [--port 0] [--open]  Loopback-only local webpage with session authentication

Official CLI login remains a user-owned prerequisite. This alpha does not create,
delete or adopt whole cloud projects, change billing, or migrate databases.
All non-server command outputs are JSON. State files contain resource references,
not cloud credentials. Use one explicit project authorization for regular updates.
`

func resolveTool(explicit, envName, legacyEnvName, name, relative string) string {
	if explicit != "" {
		return explicit
	}
	for _, key := range []string{envName, legacyEnvName} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	binary, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(binary); err == nil {
		binary = real
	}
	for _, base := range []string{filepath.Dir(binary), filepath.Dir(filepath.Dir(binary))} {
		for _, directory := range []string{"tools", filepath.Join("validation", "cloud-tools")} {
			p := filepath.Join(base, directory, "node_modules", relative)
			if info, e := os.Stat(p); e == nil && !info.IsDir() {
				return p
			}
		}
	}
	if p, e := exec.LookPath(name); e == nil {
		return p
	}
	return ""
}

func defaultStateDir(base string) string {
	for _, name := range []string{"ship", "upok"} {
		path := filepath.Join(base, name)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return path
		}
	}
	return filepath.Join(base, "ship")
}

type Settings struct {
	Railway    string `json:"railway_bin"`
	Neon       string `json:"neon_bin"`
	NeonConfig string `json:"neon_config"`
}

func output(v any) { enc := json.NewEncoder(os.Stdout); enc.SetIndent("", "  "); enc.Encode(v) }
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("ship", flag.ContinueOnError)
	stateDir := flags.String("state-dir", defaultStateDir(base), "private local state")
	rwy := flags.String("railway-bin", "", "official Railway CLI")
	neon := flags.String("neon-bin", "", "official Neon CLI")
	neonConfig := flags.String("neon-config", "", "Neon authentication directory")
	flags.Usage = func() { fmt.Print(help) }
	if err = flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = flags.Args()
	if len(args) == 0 || args[0] == "help" {
		fmt.Print(help)
		return nil
	}
	store, err := newStore(*stateDir)
	if err != nil {
		return err
	}
	var settings Settings
	if err = readJSON(filepath.Join(store.Root, "settings.json"), &settings); err != nil && !os.IsNotExist(err) {
		return errors.New("local CLI settings could not be read")
	}
	if *rwy != "" {
		settings.Railway = *rwy
	}
	if *neon != "" {
		settings.Neon = *neon
	}
	if *neonConfig != "" {
		settings.NeonConfig = *neonConfig
	}
	if settings.NeonConfig == "" {
		settings.NeonConfig = filepath.Join(home, ".config", "neon")
	}
	providers := &Providers{Railway: resolveTool(settings.Railway, "SHIP_RAILWAY_BIN", "UPOK_RAILWAY_BIN", "railway", "@railway/cli/bin/railway"), Neon: resolveTool(settings.Neon, "SHIP_NEON_BIN", "UPOK_NEON_BIN", "neon", ".bin/neon"), NeonConfig: settings.NeonConfig}
	engine := &Engine{Store: store, Providers: providers}
	switch args[0] {
	case "secret":
		return secretCommand(store, args[1:], os.Stdin)
	case "register":
		f := flag.NewFlagSet("register", flag.ContinueOnError)
		var p Project
		f.StringVar(&p.Name, "name", "", "local project name")
		f.StringVar(&p.Source, "source", "", "source directory")
		f.StringVar(&p.URL, "url", "", "application HTTPS origin")
		f.StringVar(&p.Workspace, "workspace", "", "Railway workspace")
		f.StringVar(&p.RailwayProject, "railway-project", "", "Railway project")
		f.StringVar(&p.Service, "service", "", "Railway service")
		f.StringVar(&p.Environment, "environment", "", "Railway environment")
		f.StringVar(&p.NeonOrg, "neon-org", "", "Neon organization")
		f.StringVar(&p.NeonProject, "neon-project", "", "Neon project")
		f.StringVar(&p.NeonEndpoint, "neon-endpoint", "", "Neon endpoint")
		f.BoolVar(&p.AllowPublish, "allow-publish", false, "authorize regular source updates to this exact service")
		f.BoolVar(&p.AllowTrial, "allow-trial", false, "explicitly accept the current Trial account for internal testing")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected registration arguments")
		}
		if p.Source == "" {
			return errors.New("source is required")
		}
		if err = validateProject(&p); err != nil {
			return err
		}
		if _, err = providers.inspect(ctx, p); err != nil {
			return err
		}
		if err = store.register(p); err != nil {
			return err
		}
		if err = atomicJSON(filepath.Join(store.Root, "settings.json"), Settings{providers.Railway, providers.Neon, providers.NeonConfig}); err != nil {
			return err
		}
		output(p)
		return nil
	case "list":
		projects, err := store.projects()
		if err != nil {
			return err
		}
		output(projects)
		return nil
	case "serve":
		f := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := f.Int("port", 0, "loopback port")
		open := f.Bool("open", false, "open local browser")
		if err = f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected server arguments")
		}
		return serve(ctx, engine, *port, *open)
	}
	if len(args) < 2 {
		return errors.New("project name is required")
	}
	p, err := store.project(args[1])
	if err != nil {
		return errors.New("registered project not found")
	}
	switch args[0] {
	case "authorize":
		f := flag.NewFlagSet("authorize", flag.ContinueOnError)
		f.BoolVar(&p.AllowPublish, "allow-publish", p.AllowPublish, "authorization for future source updates to this exact service")
		f.BoolVar(&p.AllowTrial, "allow-trial", p.AllowTrial, "accept Trial conditions for this binding")
		if err = f.Parse(args[2:]); err != nil {
			return err
		}
		if f.NArg() != 0 || f.NFlag() == 0 {
			return errors.New("supply an explicit authorization flag")
		}
		unlock, err := store.lock(p.Name)
		if err != nil {
			return err
		}
		defer unlock()
		if err = atomicJSON(filepath.Join(store.Root, "projects", p.Name+".json"), p); err != nil {
			return err
		}
		output(p)
	case "status":
		if len(args) != 2 {
			return errors.New("unexpected status arguments")
		}
		o, err := engine.refresh(ctx, p)
		if err != nil {
			return err
		}
		output(o)
	case "check":
		if len(args) != 2 {
			return errors.New("unexpected check arguments")
		}
		c, err := engine.check(ctx, p)
		if err != nil {
			return err
		}
		output(c)
		for _, result := range c.Results {
			if !result.OK {
				return errors.New("application access remains unverified; see check results")
			}
		}
	case "logs":
		if len(args) != 2 {
			return errors.New("unexpected logs arguments")
		}
		logs, err := engine.logs(ctx, p)
		if err != nil {
			return err
		}
		output(map[string]any{"lines": logs, "redaction": "known credentials and common secret patterns; review before sharing"})
	case "history":
		ops, err := store.operations(p.Name)
		if err != nil {
			return err
		}
		output(ops)
	case "publish":
		wait := true
		if len(args) == 3 && args[2] == "--detach" {
			wait = false
		} else if len(args) != 2 {
			return errors.New("use publish NAME [--detach]")
		}
		op, unlock, err := engine.begin(p)
		if err != nil {
			return err
		}
		defer unlock()
		err = engine.executePublish(ctx, p, op, wait)
		output(op)
		if err == nil {
			err = operationError(op)
		}
		return err
	case "reconcile":
		wait := false
		if len(args) == 3 && args[2] == "--wait" {
			wait = true
		} else if len(args) != 2 {
			return errors.New("use reconcile NAME [--wait]")
		}
		op, err := engine.reconcile(ctx, p, wait)
		if op != nil {
			output(op)
			if err == nil {
				err = operationError(op)
			}
		}
		return err
	default:
		return errors.New("unknown command; run ship help")
	}
	return nil
}
