package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// pluginTarget is one release platform for plugins. CI builds each on its own
// runner so the official plugin links against that platform's WebView stack and
// needs no cross-compiled cgo.
type pluginTarget struct {
	Platform string
	OS       string
	Arch     string
}

// hostPluginTarget returns the target matching the machine running the tool.
func hostPluginTarget() pluginTarget {
	return pluginTarget{
		Platform: runtime.GOOS + "-" + archKey(runtime.GOARCH),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}
}

// pluginsManifest is the subset of source.json the packager needs.
type pluginsManifest struct {
	ID      string                     `json:"id"`
	Version string                     `json:"version"`
	Builtin bool                       `json:"builtin"`
	Entry   map[string]json.RawMessage `json:"entry"`
}

// pluginsCommand packages every plugin under <repo>/plugins for release.
//
// A plugin whose manifest says builtin is an official Go source: its binary is
// cross-compiled from native/cmd/<id>-source and bundled with the manifests.
// Every other plugin is published as its source tree, so its author supplies
// the interpreter (node, python, …) at install time.
func pluginsCommand(stdout, stderr io.Writer, args []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	targets := []pluginTarget{hostPluginTarget()}
	if len(args) > 0 {
		want := strings.TrimSpace(args[0])
		matched := false
		for _, target := range knownPluginTargets() {
			if target.Platform == want {
				targets = []pluginTarget{target}
				matched = true
				break
			}
		}
		if !matched {
			return usageError("plugins", "unknown platform "+want)
		}
	}
	filter, err := pluginFilter()
	if err != nil {
		return err
	}
	source := filepath.Join(root, "plugins")
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("plugins: %w", err)
	}
	outDir := filepath.Join(root, "release", "plugins")
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	var sums []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if filter != nil && !filter[entry.Name()] {
			continue
		}
		dir := filepath.Join(source, entry.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "source.json"))
		if err != nil {
			return fmt.Errorf("plugins: %s: %w", entry.Name(), err)
		}
		var manifest pluginsManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("plugins: %s/source.json: %w", entry.Name(), err)
		}
		if manifest.ID == "" {
			return fmt.Errorf("plugins: %s/source.json 缺少 id", entry.Name())
		}
		version := strings.TrimSpace(manifest.Version)
		if version == "" {
			version = "0.0.0"
		}
		for _, target := range targets {
			stage, err := os.MkdirTemp("", "tinglan-plugin-")
			if err != nil {
				return err
			}
			err = stagePlugin(root, dir, manifest, target.OS, target.Arch, stage, stdout, stderr)
			if err == nil {
				name := fmt.Sprintf("%s-%s-%s.zip", manifest.ID, version, target.Platform)
				err = zipDir(stage, filepath.Join(outDir, name))
				if err == nil {
					sum, sumErr := fileSHA256(filepath.Join(outDir, name))
					if sumErr != nil {
						err = sumErr
					} else {
						sums = append(sums, sum+"  "+name)
					}
					fmt.Fprintf(stdout, "plugins: %s\n", name)
				}
			}
			_ = os.RemoveAll(stage)
			if err != nil {
				return fmt.Errorf("plugins: %s: %w", manifest.ID, err)
			}
		}
	}
	sort.Strings(sums)
	if err := os.WriteFile(filepath.Join(outDir, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return nil
}

// pluginFilter reads TINGLAN_PLUGINS. Unset or "all" packages every plugin;
// an empty value is a caller error; otherwise it is a comma-separated id list.
func pluginFilter() (map[string]bool, error) {
	raw, ok := os.LookupEnv("TINGLAN_PLUGINS")
	if !ok || strings.TrimSpace(raw) == "all" {
		return nil, nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil, usageError("plugins", "TINGLAN_PLUGINS 为空：没有选择要打包的插件")
	}
	filter := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		if id := strings.TrimSpace(part); id != "" {
			filter[id] = true
		}
	}
	if len(filter) == 0 {
		return nil, usageError("plugins", "TINGLAN_PLUGINS 没有有效插件 id")
	}
	return filter, nil
}

// stagePlugin fills stage with the files that make up one plugin package.
func stagePlugin(root, dir string, manifest pluginsManifest, goos, goarch, stage string, stdout, stderr io.Writer) error {
	if manifest.Builtin {
		binary, err := pluginEntryPath(manifest.Entry, goos, goarch)
		if err != nil {
			return err
		}
		target := filepath.Join(stage, filepath.FromSlash(binary))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		command := filepath.Join(root, "native", "cmd", manifest.ID+"-source")
		if _, err := os.Stat(command); err != nil {
			return fmt.Errorf("缺少官方插件入口 %s: %w", command, err)
		}
		args := []string{"build", "-trimpath", "-ldflags=-s -w", "-o", target, "./cmd/" + manifest.ID + "-source"}
		if err := runCommand(commandSpec{
			Dir:  filepath.Join(root, "native"),
			Name: goBinary(),
			Args: args,
		}, stdout, stderr); err != nil {
			return err
		}
	}
	for _, name := range []string{"source.json", "descriptor.json", "README.md"} {
		source := filepath.Join(dir, name)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		if err := copyFile(source, filepath.Join(stage, name)); err != nil {
			return err
		}
	}
	if !manifest.Builtin {
		return copyTree(dir, stage)
	}
	return nil
}

// pluginEntryPath returns the binary path declared for one platform.
func pluginEntryPath(entry map[string]json.RawMessage, goos, goarch string) (string, error) {
	key := goos + "-" + archKey(goarch)
	raw, ok := entry[key]
	if !ok {
		raw, ok = entry["any"]
	}
	if !ok {
		return "", fmt.Errorf("清单没有 %s 入口", key)
	}
	var command string
	if err := json.Unmarshal(raw, &command); err == nil {
		if command == "" {
			return "", errors.New("入口为空")
		}
		return command, nil
	}
	var spec struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil || spec.Command == "" || len(spec.Args) > 0 {
		return "", errors.New("官方插件入口必须是目录内的可执行文件")
	}
	return spec.Command, nil
}

// archKey maps GOARCH to the manifest's platform suffix.
func archKey(goarch string) string {
	if goarch == "amd64" {
		return "x64"
	}
	return goarch
}

// zipDir writes every regular file under dir into dst, keeping relative paths.
func zipDir(dir, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(infoOf(path))
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		w, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		_, err = io.Copy(w, source)
		return err
	})
	if walkErr != nil {
		writer.Close()
		return walkErr
	}
	return writer.Close()
}

func infoOf(path string) os.FileInfo {
	info, _ := os.Stat(path)
	return info
}

// copyTree copies regular files below src into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

// fileSHA256 returns the "<hex>  <name>" line GitHub Releases expects.
func fileSHA256(path string) (string, error) {
	sum, err := sha256Hex(path)
	if err != nil {
		return "", err
	}
	return sum, nil
}

// knownPluginTargets lists every platform a release publishes plugin packages
// for. CI calls the tool once per runner with that runner's platform.
func knownPluginTargets() []pluginTarget {
	return []pluginTarget{
		{"darwin-arm64", "darwin", "arm64"},
		{"darwin-x64", "darwin", "amd64"},
		{"windows-x64", "windows", "amd64"},
	}
}
