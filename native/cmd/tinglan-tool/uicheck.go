package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// uiCheckCommand ports native/scripts/check-ui.mjs: build the vivahead binary,
// feed the same synthetic command scripts to two demo profiles, fail on error
// lines or layout problems, and require the 965-track queue to be saved.
func uiCheckCommand(stdout, _ io.Writer, _ []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	native := filepath.Join(root, "native")
	goBin := goBinary()
	qa := filepath.Join(root, "out", "native", "qa")
	if err := os.MkdirAll(qa, 0o755); err != nil {
		return err
	}
	binary := filepath.Join(qa, "music-headless")
	if runtime.GOOS == "windows" {
		// go build -o appends .exe on Windows; exec needs the real file name.
		binary += ".exe"
	}
	build := exec.Command(goBin, "build", "-tags=vivahead", "-o", binary, "./cmd/music")
	build.Dir = native
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build headless binary: %w", err)
	}
	profile, err := os.MkdirTemp("", "hwmusic-viva-ui-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)

	themes := []string{"晴海", "鼠尾草", "暖砂", "蔷薇", "冰川", "午夜蓝", "深林", "石墨", "夜玫瑰", "咖啡", "曜石", "晨光"}
	var long []string
	long = append(long, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "library-current.png")))
	long = append(long, `click "播放顺序"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "mode-menu.png")), `click "随机播放"`, `click "标准"`,
		"layout", fmt.Sprintf("shot %s", filepath.Join(qa, "quality-menu.png")), `click "无损"`, `click "调节音量"`, "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "volume-menu.png")), `click "调节音量"`, `click "设置"`, "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "settings-playback-current.png")), `click "外观设置"`, "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "settings-appearance-current.png")))
	for _, theme := range themes {
		long = append(long, fmt.Sprintf("click %q", theme), "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "theme-"+theme+".png")))
		if theme == "午夜蓝" {
			long = append(long, `click "调节音量"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "volume-menu-dark.png")), `click "调节音量"`)
		}
	}
	long = append(long, `click "选择系统字体"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "font-picker-current.png")), `click "关闭"`)
	for _, width := range []int{1100, 1020, 960} {
		long = append(long, fmt.Sprintf("resize %d 640", width), "layout", fmt.Sprintf("shot %s", filepath.Join(qa, fmt.Sprintf("settings-appearance-%d.png", width))))
	}
	long = append(long, `click "播放设置"`, "scroll 800 420 0 550", "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "settings-playback-scrolled-960.png")),
		"resize 1180 760", `click "我的收藏"`, `click "专辑 Fixture Album"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "album-current.png")),
		`click "返回"`, `click "艺人 Local fixture"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "artist-current.png")), `click "返回"`,
		`click "我的歌单"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "collections-current.png")), `click "离线测试歌单"`, "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "playlist-current.png")),
		`click "播放全部"`, `click "打开沉浸歌词"`, "settle 400", "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "stage-current.png")),
		"resize 960 640", `click "调节音量"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "volume-menu-960.png")), `click "调节音量"`, "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "stage-960.png")), `click "歌词选项"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "lyrics-menu-960.png")),
		`click "歌词提前半秒"`, `click "歌词选项"`, `click "退出沉浸歌词"`, "settle 400", `click "播放队列"`, "layout",
		fmt.Sprintf("shot %s", filepath.Join(qa, "drawer-current.png")), "quit")
	short := []string{`click "播放全部"`, `waitfor "共 965 首收藏" 5000`, `click "播放队列"`, "layout", fmt.Sprintf("shot %s", filepath.Join(qa, "queue-965.png")), "quit"}

	type layout struct {
		Tracks int `json:"tracks"`
		Checks int `json:"checks"`
	}
	report := struct {
		Platform       string   `json:"platform"`
		Arch           string   `json:"arch"`
		Synthetic      bool     `json:"synthetic"`
		PrivateAccount bool     `json:"privateAccount"`
		Layouts        []layout `json:"layouts"`
	}{Platform: "darwin", Arch: "arm64", Synthetic: true, PrivateAccount: false}

	errorLine := regexp.MustCompile(`(?m)^error\b`)
	problems := regexp.MustCompile(`ok [1-9]\d* problems`)
	passed := regexp.MustCompile(`ok 0 problems`)
	for _, plan := range []struct {
		count    int
		commands []string
	}{{2, long}, {965, short}} {
		script := strings.Join(plan.commands, "\n") + "\n"
		cmd := exec.Command(binary, "--demo", fmt.Sprint(plan.count), "--profile", profile)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "VIVA_HEADLESS=1")
		cmd.Stdin = strings.NewReader(script)
		output, err := cmd.CombinedOutput()
		_ = os.WriteFile(filepath.Join(qa, fmt.Sprintf("ui-%d.txt", plan.count)), output, 0o644)
		text := string(output)
		if err != nil || errorLine.MatchString(text) || problems.MatchString(text) {
			detail := strings.TrimSpace(text)
			if len(detail) > 400 {
				detail = detail[:400]
			}
			return fmt.Errorf("native UI check failed for %d (err=%v, output=%q); see out/native/qa/ui-%d.txt", plan.count, err, detail, plan.count)
		}
		report.Layouts = append(report.Layouts, layout{Tracks: plan.count, Checks: len(passed.FindAllString(text, -1))})
	}
	saved, err := os.ReadFile(filepath.Join(profile, "queue.local.json"))
	if err != nil {
		return fmt.Errorf("queue was not saved: %w", err)
	}
	var queue struct {
		SchemaVersion int               `json:"schemaVersion"`
		Items         []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(saved, &queue); err != nil {
		return fmt.Errorf("parse saved queue: %w", err)
	}
	if queue.SchemaVersion != 2 {
		return fmt.Errorf("queue schemaVersion = %d, want 2", queue.SchemaVersion)
	}
	if len(queue.Items) != 965 {
		return fmt.Errorf("native UI saved an incomplete queue: %d", len(queue.Items))
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(qa, "report.json"), append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}
