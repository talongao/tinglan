package webauth

// Only static local failure categories reach the UI, never remote error bodies.
func LoginErrorMessage(err error) string {
	if err != nil {
		switch err.Error() {
		case "compatible authorization browser not installed":
			return "登录需要已安装的 Edge 或 Chrome，请安装后重试。"
		case "authorization page failed to load":
			return "华为登录页加载失败，请检查网络后重试。"
		case "authorization browser startup timed out", "browser target unavailable", "browser setup failed at Page.enable", "browser setup failed at Fetch.enable":
			return "登录窗口启动超时，请关闭后重试。"
		}
	}
	return "登录未完成，请重试；原会话保持不变"
}
