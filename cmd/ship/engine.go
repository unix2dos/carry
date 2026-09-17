package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Engine struct {
	Store        *Store
	Providers    *Providers
	PollInterval time.Duration
}

func validateReferences(p *Project) error {
	if !slugPattern.MatchString(p.Name) {
		return errors.New("name must use lowercase letters, digits and hyphens")
	}
	ids := []string{p.NeonOrg, p.NeonProject, p.NeonEndpoint}
	switch p.Provider {
	case "", "railway":
		ids = append(ids, p.Workspace, p.RailwayProject, p.Service, p.Environment)
	case "vercel":
		if !strings.HasPrefix(p.VercelTeam, "team_") || !strings.HasPrefix(p.VercelProject, "prj_") {
			return errors.New("Vercel requires explicit team and project IDs")
		}
		ids = append(ids, p.VercelTeam, p.VercelProject)
	default:
		return errors.New("unsupported compute provider")
	}
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			return errors.New("all provider resource IDs must be explicit and valid")
		}
	}
	u, err := url.Parse(p.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("application URL must be an HTTPS origin without credentials")
	}
	p.URL = strings.TrimSuffix(p.URL, "/")
	return nil
}
func validateProject(p *Project) error {
	if err := validateReferences(p); err != nil {
		return err
	}
	var err error
	p.Source, err = filepath.Abs(p.Source)
	if err != nil {
		return err
	}
	p.Source, err = filepath.EvalSymlinks(p.Source)
	if err != nil {
		return errors.New("source directory does not exist")
	}
	dockerfile := "Dockerfile"
	if p.Provider == "vercel" {
		dockerfile = "Dockerfile.vercel"
	}
	info, err := os.Stat(filepath.Join(p.Source, dockerfile))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("internal alpha requires a source directory with %s", dockerfile)
	}
	return nil
}

func operationError(op *Operation) error {
	if op.State == "failed" || op.State == "blocked" || op.State == "unknown" {
		return errors.New(op.Message)
	}
	return nil
}
func (e *Engine) refresh(ctx context.Context, p Project) (Observation, error) {
	f, err := e.Providers.inspect(ctx, p)
	if err != nil {
		return f.Observation, err
	}
	err = e.Store.saveObservation(p.Name, f.Observation)
	return f.Observation, err
}
func (e *Engine) check(ctx context.Context, p Project) (ApplicationChecks, error) {
	c := ApplicationChecks{At: time.Now().UTC(), Results: checkApplication(ctx, p.URL)}
	return c, e.Store.saveChecks(p.Name, c)
}

func (e *Engine) logs(ctx context.Context, p Project) ([]string, error) {
	known, err := e.Store.knownSecrets(p.Name)
	if err != nil {
		return nil, errors.New("local secret-file redaction context is unavailable; logs withheld")
	}
	lines, err := e.Providers.logs(ctx, p)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		lines[i] = redact(lines[i], known)
	}
	return lines, nil
}
func excluded(name string) bool {
	for _, v := range []string{".git", ".vercel", "node_modules", ".venv", "__pycache__", "target", ".ship", ".upok", ".pdeploy", ".ssh", ".aws", ".netrc", ".npmrc"} {
		if name == v {
			return true
		}
	}
	lower := strings.ToLower(name)
	return lower == ".env" || (strings.HasPrefix(lower, ".env.") && lower != ".env.example") || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".db")
}
func bundleSource(source, root string, secrets []string) (dir, digest string, count int, err error) {
	if rel, e := filepath.Rel(source, root); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", 0, errors.New("the source directory must not contain local tool state")
	}
	dir, err = os.MkdirTemp("", "ship-source-*")
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	hash := sha256.New()
	var total int64
	// ponytail: copy at most 20 MiB of regular source files for the internal alpha; larger repositories need a reviewed archive strategy.
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot read source tree")
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		if excluded(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source contains a symlink: %s; alpha does not follow symlinks", rel)
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0700)
		}
		info, e := d.Info()
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("source contains an unsupported file")
		}
		total += info.Size()
		if total > 20<<20 || count >= 2000 {
			return errors.New("source exceeds the internal alpha limit (20 MiB / 2000 files)")
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return errors.New("source file could not be read")
		}
		after, e := os.Stat(path)
		if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return errors.New("source changed during capture; review it before publishing")
		}
		for _, secret := range secrets {
			if len(secret) >= 8 && strings.Contains(string(data), secret) {
				return errors.New("source contains a known cloud credential; upload blocked")
			}
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		hash.Write(data)
		if e = os.WriteFile(filepath.Join(dir, rel), data, info.Mode().Perm()&0755); e != nil {
			return e
		}
		count++
		return nil
	})
	if err == nil {
		digest = hex.EncodeToString(hash.Sum(nil))
	}
	return
}
func (e *Engine) begin(p Project) (*Operation, func(), error) {
	if !p.AllowPublish {
		return nil, nil, errors.New("publishing is not authorized for this project binding")
	}
	unlock, err := e.Store.lock(p.Name)
	if err != nil {
		return nil, nil, err
	}
	operations, err := e.Store.operations(p.Name)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	for _, op := range operations {
		if !op.terminal() {
			unlock()
			return nil, nil, fmt.Errorf("operation %s needs reconciliation before another publish", op.ID)
		}
	}
	op, err := newOperation(p.Name)
	if err == nil {
		err = e.Store.saveOp(&op)
	}
	if err != nil {
		unlock()
		return nil, nil, err
	}
	return &op, unlock, nil
}
func (e *Engine) record(op *Operation, state, message string) error {
	op.State = state
	op.Message = message
	return e.Store.saveOp(op)
}
func (e *Engine) executePublish(ctx context.Context, p Project, op *Operation, wait bool) error {
	pre, err := e.Providers.inspect(ctx, p)
	if err != nil {
		e.record(op, "blocked", err.Error())
		return err
	}
	if err = e.Store.saveObservation(p.Name, pre.Observation); err != nil {
		return err
	}
	if !pre.Observation.Eligible {
		err = errors.New(pre.Observation.Reason)
		e.record(op, "blocked", err.Error())
		return err
	}
	known, err := e.Store.knownSecrets(p.Name)
	if err != nil {
		e.record(op, "blocked", "本地密钥文件不可用，无法完成源码密钥检查")
		return err
	}
	pre.Secrets = append(pre.Secrets, known...)
	stage, digest, count, err := bundleSource(p.Source, e.Store.Root, pre.Secrets)
	if err != nil {
		e.record(op, "blocked", err.Error())
		return err
	}
	defer os.RemoveAll(stage)
	if err = ctx.Err(); err != nil {
		e.record(op, "blocked", "操作在提交前停止")
		return err
	}
	op.SourceHash = digest
	op.SourceFiles = count
	if p.Provider == "vercel" {
		if err = prepareVercelSource(stage, p); err != nil {
			e.record(op, "blocked", err.Error())
			return err
		}
	}
	// Durable intent precedes the only cloud mutation. Unknown outcomes are reconciled by this unique marker.
	if err = e.record(op, "submitting", "正在提交源码；结果未确认前不会重复发布"); err != nil {
		return err
	}
	submitErr := e.Providers.submit(ctx, p, stage, op.Marker)
	if err = e.record(op, "unknown", "提交结果待核对"); err != nil {
		return err
	}
	if err = e.reconcileOnce(ctx, p, op); err != nil {
		return err
	}
	if submitErr != nil && op.DeploymentID == "" {
		return errors.New("submission outcome is unknown; use reconcile, not another publish")
	}
	if wait && !op.terminal() {
		return e.monitor(ctx, p, op)
	}
	return nil
}
func (e *Engine) reconcileOnce(ctx context.Context, p Project, op *Operation) error {
	if op.State == "blocked" {
		return nil
	}
	if op.State == "checking" {
		return e.record(op, "blocked", "操作在提交前中断，未记录任何发布请求")
	}
	deployments, err := e.Providers.deployments(ctx, p)
	if err != nil {
		e.record(op, "unknown", "平台查询失败，保留已有记录；稍后重新核对")
		return err
	}
	var matches []Deployment
	for _, d := range deployments {
		if d.Meta.Message == op.Marker && (op.DeploymentID == "" || d.ID == op.DeploymentID) {
			matches = append(matches, d)
		}
	}
	if len(matches) != 1 {
		return e.record(op, "unknown", "未能唯一匹配原部署。保留待核实状态，不重复提交（查询最近 100 次部署）")
	}
	d := matches[0]
	op.DeploymentID = d.ID
	op.ProviderState = d.Status
	switch d.Status {
	case "SUCCESS", "READY":
		if p.Provider == "vercel" && !d.AliasAssigned {
			return e.record(op, "deploying", "构建已完成，等待生产域名关联；不会重复提交")
		}
		c, err := e.check(ctx, p)
		if err != nil {
			return err
		}
		op.Checks = c.Results
		op.ChecksAt = &c.At
		return e.record(op, "deployed", "平台已完成发布；应用访问检查单独显示")
	case "SLEEPING":
		return e.record(op, "deployed", "部署已完成；上次核对时服务处于休眠")
	case "FAILED", "ERROR", "CRASHED", "REMOVED", "CANCELED", "CANCELLED", "SKIPPED":
		return e.record(op, "failed", "平台报告本次部署未运行；请查看日志后决定下一步")
	default:
		return e.record(op, "deploying", "已找到原部署，等待平台完成；不会创建替代资源")
	}
}
func (e *Engine) monitor(ctx context.Context, p Project, op *Operation) error {
	interval := e.PollInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	for !op.terminal() {
		select {
		case <-ctx.Done():
			e.record(op, "unknown", "本地进程停止，云端结果需在重开后核对")
			return ctx.Err()
		case <-deadline.C:
			e.record(op, "unknown", "本次观察窗口结束，云端任务未取消；可继续核对")
			return nil
		case <-time.After(interval):
			if err := e.reconcileOnce(ctx, p, op); err != nil {
				return err
			}
		}
	}
	return nil
}
func (e *Engine) reconcile(ctx context.Context, p Project, wait bool) (*Operation, error) {
	unlock, err := e.Store.lock(p.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	ops, err := e.Store.operations(p.Name)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 {
		return nil, errors.New("no recorded operation to reconcile")
	}
	op := &ops[0]
	for i := range ops {
		if !ops[i].terminal() {
			op = &ops[i]
			break
		}
	}
	// A completed operation is historical evidence; current resource state belongs to status/check.
	if op.terminal() {
		return op, nil
	}
	if err = e.reconcileOnce(ctx, p, op); err == nil && wait && !op.terminal() {
		err = e.monitor(ctx, p, op)
	}
	return op, err
}
