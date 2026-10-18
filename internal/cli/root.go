package cli

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/routeforge/routeforge/internal/config"
	"github.com/routeforge/routeforge/internal/generator"
	"github.com/routeforge/routeforge/internal/runtime"
	"github.com/routeforge/routeforge/internal/version"
	"github.com/spf13/cobra"
)

var composeFile, configFile string

func Execute() {
	if err := New().Execute(); err != nil {
		os.Exit(1)
	}
}
func New() *cobra.Command {
	root := &cobra.Command{Use: "rforge", Short: "YAML-driven Nginx configuration manager"}
	root.PersistentFlags().StringVarP(&composeFile, "file", "f", "", "Compose file")
	root.PersistentFlags().StringVarP(&configFile, "config", "c", "routeforge.yaml", "RouteForge YAML file")
	root.AddCommand(compose("up", "up"), compose("down", "down"), compose("restart", "restart"), compose("status", "ps"), compose("ps", "ps"), compose("logs", "logs"))
	root.AddCommand(&cobra.Command{Use: "validate", RunE: func(cmd *cobra.Command, args []string) error { _, e := config.Load(configFile); return e }}, &cobra.Command{Use: "generate", RunE: func(cmd *cobra.Command, args []string) error {
		c, e := config.Load(configFile)
		if e != nil {
			return e
		}
		b, e := generator.Render(c)
		if e == nil {
			_, e = cmd.OutOrStdout().Write(b)
		}
		return e
	}}, &cobra.Command{Use: "reload", RunE: func(cmd *cobra.Command, args []string) error {
		return composeExec(cmd, "exec", "-T", "proxy", "rforge", "apply")
	}}, &cobra.Command{Use: "config", RunE: func(cmd *cobra.Command, args []string) error {
		b, e := os.ReadFile(configFile)
		if e == nil {
			_, e = cmd.OutOrStdout().Write(b)
		}
		return e
	}}, &cobra.Command{Use: "version", Run: func(*cobra.Command, []string) { fmt.Fprintln(os.Stdout, version.Value) }})
	root.AddCommand(initCommand())
	root.AddCommand(&cobra.Command{Use: "apply", Hidden: true, RunE: func(*cobra.Command, []string) error {
		path := os.Getenv("ROUTEFORGE_CONFIG")
		if path == "" {
			path = "/config/routeforge.yaml"
		}
		m := &runtime.Manager{ConfigPath: path, Active: "/etc/nginx/nginx.conf", Reload: true}
		return m.Apply()
	}})
	root.AddCommand(&cobra.Command{Use: "runtime", Hidden: true, Run: func(*cobra.Command, []string) { runtime.Run() }})
	return root
}
func compose(name, sub string) *cobra.Command {
	cmd := &cobra.Command{Use: name, DisableFlagParsing: true, RunE: func(cmd *cobra.Command, args []string) error {
		if name == "up" {
			found := false
			for _, a := range args {
				if a == "-d" || a == "--detach" {
					found = true
				}
			}
			if !found {
				args = append(args, "-d")
			}
		}
		return composeExec(cmd, sub, args...)
	}}
	return cmd
}
func composeExec(cmd *cobra.Command, sub string, args ...string) error {
	f := composeFile
	if f == "" {
		for _, n := range []string{"compose.yml", "compose.yaml"} {
			if _, e := os.Stat(n); e == nil {
				f = n
				break
			}
		}
	}
	if f == "" {
		return fmt.Errorf("no compose.yml or compose.yaml in current directory")
	}
	all := []string{"compose", "-f", f, sub}
	all = append(all, args...)
	c := exec.Command("docker", all...)
	c.Stdin = os.Stdin
	c.Stdout = cmd.OutOrStdout()
	c.Stderr = cmd.ErrOrStderr()
	if e := c.Run(); e != nil {
		if x, ok := e.(*exec.ExitError); ok {
			return &exitError{x.ExitCode(), e}
		}
		return fmt.Errorf("cannot run Docker Compose: %w", e)
	}
	return nil
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) ExitCode() int { return e.code }
func initCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "init", RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		files := map[string]string{
			"routeforge.yaml":        sample,
			"compose.yml":            composeSample,
			".env.example":           "HTTP_PORT=80\nHTTPS_PORT=443\n",
			".routeforge/Dockerfile": runtimeDockerfile,
			".routeforge/entrypoint": runtimeEntrypoint,
		}
		for n := range files {
			if _, e := os.Stat(n); e == nil && !force {
				return fmt.Errorf("%s exists (use --force to overwrite)", n)
			}
		}
		for n, v := range files {
			if e := os.MkdirAll(filepath.Dir(n), 0755); e != nil {
				return e
			}
			mode := os.FileMode(0644)
			if n == ".routeforge/entrypoint" {
				mode = 0755
			}
			if e := os.WriteFile(filepath.Clean(n), []byte(v), mode); e != nil {
				return e
			}
		}
		// Include the Go runtime sources so this works from any directory and
		// produces a Linux binary for the Docker target, regardless of the host OS.
		if err := fs.WalkDir(runtimeSource, "bundle", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			data, err := runtimeSource.ReadFile(name)
			if err != nil {
				return err
			}
			rel := strings.TrimPrefix(name, "bundle/")
			if rel == "module.txt" {
				rel = "go.mod"
			} else if rel == "modulesum.txt" {
				rel = "go.sum"
			}
			dest := filepath.Join(".routeforge/src", rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0644)
		}); err != nil {
			return fmt.Errorf("write bundled runtime source: %w", err)
		}
		return nil
	}}
	cmd.Flags().Bool("force", false, "Overwrite existing files")
	return cmd
}

const sample = `version: 1
global:
  worker_connections: 1024
upstreams:
  app:
    address: host.docker.internal:3000
hot_reload:
  enabled: true
  debounce: 2s
  poll_interval: 1s
  rollback_on_failure: true
servers:
  - name: localhost
    listen: 80
    routes:
      - path: /
        upstream: app
        match: prefix
`
const composeSample = `services:
  proxy:
    build:
      context: .
      dockerfile: .routeforge/Dockerfile
    ports:
      - "${HTTP_PORT:-80}:80"
      - "${HTTPS_PORT:-443}:443"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    volumes:
      - ./:/config:ro
    restart: unless-stopped
`

const runtimeDockerfile = `FROM golang:1.26-alpine AS build
WORKDIR /src
COPY .routeforge/src/ .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -o /rforge ./cmd/routeforge-runtime
FROM nginx:1.27-alpine
COPY --from=build /rforge /usr/local/bin/rforge
COPY .routeforge/entrypoint /usr/local/bin/routeforge-entrypoint
RUN chmod 755 /usr/local/bin/routeforge-entrypoint
ENTRYPOINT ["/usr/local/bin/routeforge-entrypoint"]
`

const runtimeEntrypoint = `#!/bin/sh
set -eu
exec /usr/local/bin/rforge
`
