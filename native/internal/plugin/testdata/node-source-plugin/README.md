# Node 示例音源插件

演示 `source.json` 的 MCP 式入口：`{"command":"node","args":["main.mjs"]}`。
插件本身用 Content-Length 帧的 JSON-RPC 2.0 与宿主通信，不依赖 Go SDK。

试用方式：

1. 确认 `node` 在 PATH 里（`node --version`）。
2. 打开 听澜 → 设置 → 音源 → 导入插件，选择本目录。
3. 启用后在搜索页切到「Node 示例源」，搜「脚本」应看到 `脚本写的歌`。

这是示例，不是给终端用户的正式音源。
