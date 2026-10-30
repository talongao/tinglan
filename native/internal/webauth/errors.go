package webauth

// Only static local failure categories reach the UI, never remote error bodies.
func LoginErrorMessage(err error) string {
	if err != nil {
		switch err.Error() {
		case "webview2 runtime unavailable":
			return "登录需要 Microsoft Edge WebView2 Runtime，请安装后重试。"
		case "webview2 runtime update required":
			return "请更新 Microsoft Edge WebView2 Runtime 后重试登录。"
		case "authorization window initialization failed":
			return "登录窗口初始化失败，请检查 WebView2 Runtime 后重试。"
		case "authorization profile cleanup failed":
			return "临时登录数据清理失败，本次登录未保存，请关闭登录窗口后重试。"
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
