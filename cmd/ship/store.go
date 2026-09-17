package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"
)

var slugPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{7,99}$`)

type Project struct {
	Name           string `json:"name"`
	Provider       string `json:"provider,omitempty"`
	VercelTeam     string `json:"vercel_team,omitempty"`
	VercelProject  string `json:"vercel_project,omitempty"`
	AllowHobby     bool   `json:"allow_personal_noncommercial_hobby,omitempty"`
	Source         string `json:"source"`
	URL            string `json:"url"`
	Workspace      string `json:"railway_workspace"`
	RailwayProject string `json:"railway_project"`
	Service        string `json:"railway_service"`
	Environment    string `json:"railway_environment"`
	NeonOrg        string `json:"neon_org"`
	NeonProject    string `json:"neon_project"`
	NeonEndpoint   string `json:"neon_endpoint"`
	AllowPublish   bool   `json:"allow_publish"`
	AllowTrial     bool   `json:"allow_trial"`
}

type Check struct {
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	HTTP    int    `json:"http_status,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}
type ApplicationChecks struct {
	At      time.Time `json:"at"`
	Results []Check   `json:"results"`
}
type Observation struct {
	At              time.Time `json:"at"`
	Provider        string    `json:"provider,omitempty"`
	ComputePlan     string    `json:"compute_plan,omitempty"`
	DatabaseBinding string    `json:"database_binding,omitempty"`
	RailwayPlan     string    `json:"railway_plan"`
	NeonPlan        string    `json:"neon_plan"`
	ServiceState    string    `json:"service_state"`
	DeploymentID    string    `json:"deployment_id"`
	DatabaseState   string    `json:"database_state"`
	Eligible        bool      `json:"eligible"`
	Reason          string    `json:"reason,omitempty"`
}
type Operation struct {
	ID            string     `json:"id"`
	Project       string     `json:"project"`
	State         string     `json:"state"`
	Created       time.Time  `json:"created_at"`
	Updated       time.Time  `json:"updated_at"`
	Marker        string     `json:"marker"`
	DeploymentID  string     `json:"deployment_id,omitempty"`
	ProviderState string     `json:"provider_state,omitempty"`
	SourceHash    string     `json:"source_sha256,omitempty"`
	SourceFiles   int        `json:"source_files,omitempty"`
	Checks        []Check    `json:"checks,omitempty"`
	ChecksAt      *time.Time `json:"checks_at,omitempty"`
	Message       string     `json:"message"`
}

func (o Operation) terminal() bool {
	return o.State == "deployed" || o.State == "failed" || o.State == "blocked"
}

type Store struct{ Root string }

func newStore(root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"", "projects", "operations", "observations", "checks", "locks", "secrets"} {
		p := filepath.Join(root, sub)
		if err = os.MkdirAll(p, 0700); err != nil {
			return nil, err
		}
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("state directory must be a real private directory")
		}
		if err = os.Chmod(p, 0700); err != nil {
			return nil, err
		}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}
func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func readJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("local record is too large")
	}
	return json.Unmarshal(data, into)
}
func (s *Store) project(name string) (Project, error) {
	var p Project
	if !slugPattern.MatchString(name) {
		return p, errors.New("invalid project name")
	}
	err := readJSON(filepath.Join(s.Root, "projects", name+".json"), &p)
	if err == nil && p.Name != name {
		err = errors.New("project identity mismatch")
	}
	if err == nil {
		err = validateReferences(&p)
	}
	return p, err
}
func (s *Store) projects() ([]Project, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "projects"))
	if err != nil {
		return nil, err
	}
	result := []Project{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-5]
		p, e := s.project(name)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, nil
}
func (s *Store) register(p Project) error {
	unlock, err := s.lock(p.Name)
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(s.Root, "projects", p.Name+".json")
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		return errors.New("project name already exists; registration never overwrites an existing binding")
	}
	return atomicJSON(path, p)
}
func (s *Store) lock(name string) (func(), error) {
	if !slugPattern.MatchString(name) {
		return nil, errors.New("invalid project name")
	}
	f, err := os.OpenFile(filepath.Join(s.Root, "locks", name+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another operation is running for this project")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func (s *Store) busy(name string) bool {
	unlock, err := s.lock(name)
	if err != nil {
		return true
	}
	unlock()
	return false
}
func (s *Store) saveOp(op *Operation) error {
	if !slugPattern.MatchString(op.Project) || !idPattern.MatchString(op.ID) {
		return errors.New("invalid operation identity")
	}
	op.Updated = time.Now().UTC()
	return atomicJSON(filepath.Join(s.Root, "operations", op.Project+"--"+op.ID+".json"), op)
}
func (s *Store) operations(name string) ([]Operation, error) {
	if !slugPattern.MatchString(name) {
		return nil, errors.New("invalid project name")
	}
	files, err := filepath.Glob(filepath.Join(s.Root, "operations", name+"--*.json"))
	if err != nil {
		return nil, err
	}
	// ponytail: scan per-project JSON history for the internal alpha; add an index only when history size warrants it.
	result := []Operation{}
	for _, path := range files {
		var op Operation
		if err = readJSON(path, &op); err != nil {
			return nil, err
		}
		if op.Project != name {
			return nil, errors.New("operation project mismatch")
		}
		result = append(result, op)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}
func (s *Store) observation(name string) *Observation {
	if !slugPattern.MatchString(name) {
		return nil
	}
	var o Observation
	if readJSON(filepath.Join(s.Root, "observations", name+".json"), &o) != nil {
		return nil
	}
	return &o
}
func (s *Store) saveObservation(name string, o Observation) error {
	if !slugPattern.MatchString(name) {
		return errors.New("invalid project name")
	}
	return atomicJSON(filepath.Join(s.Root, "observations", name+".json"), o)
}
func (s *Store) checks(name string) *ApplicationChecks {
	if !slugPattern.MatchString(name) {
		return nil
	}
	var c ApplicationChecks
	if readJSON(filepath.Join(s.Root, "checks", name+".json"), &c) != nil {
		return nil
	}
	return &c
}
func (s *Store) saveChecks(name string, c ApplicationChecks) error {
	if !slugPattern.MatchString(name) {
		return errors.New("invalid project name")
	}
	return atomicJSON(filepath.Join(s.Root, "checks", name+".json"), c)
}
func newOperation(name string) (Operation, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Operation{}, err
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("%d-%s", now.UnixNano(), hex.EncodeToString(random[:]))
	return Operation{ID: id, Project: name, Created: now, State: "checking", Marker: "ship:" + id, Message: "正在预检，尚未提交发布"}, nil
}
