# 示例音源插件（仅用于本地验证）

一个最小的外置音源实现，用来验证宿主侧发现、授权、启动、搜索、分页导入与停用流程。
它不连接任何真实平台，返回固定数据。

## 安装（macOS arm64）

```sh
go build -o /tmp/sample/bin/sample ./internal/plugin/testdata/sample-plugin/main.go
mkdir -p "/tmp/sample"
cp native/internal/plugin/testdata/sample-plugin/source.json /tmp/sample/
mkdir -p "$HOME/Library/Application Support/tinglan/native-v1/sources/sample/bin"
cp /tmp/sample/bin/sample "$HOME/Library/Application Support/tinglan/native-v1/sources/sample/bin/"
cp /tmp/sample/source.json "$HOME/Library/Application Support/tinglan/native-v1/sources/sample/"
```

重启听澜，在 设置 → 音源 → 外置音源 里点「示例音源」并确认启用；搜索页会出现「示例音源」页签。
停用后插件进程退出，本机曲库中的既有绑定不会被删除。

Windows 构建后把 `entry.windows-x64` 指向 `bin/sample.exe`。
