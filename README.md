# genUpdate_client

`genUpdate_client` 是 [genUpdate_server](https://github.com/jwwsjlm/genUpdate_server) 的通用更新客户端。它可以获取服务端更新清单，比较本地文件，下载缺失或变更的文件，并在校验通过后替换到本地目录。

[![GitHub Release](https://img.shields.io/github/v/release/jwwsjlm/genUpdate_client)](https://github.com/jwwsjlm/genUpdate_client/releases)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.26.3-blue)](https://golang.org)

## 功能

- 获取服务端更新清单：`/updateList/{appName}`
- 支持 token 鉴权：`Authorization: Bearer <token>`
- 本地文件先比大小，再比 SHA256，未变化则跳过
- 下载到 `.tmp` 临时文件，校验大小和 SHA256 后再替换目标文件
- 支持断点续传，服务端支持 `Range` 时会从 `.tmp` 已下载位置继续
- 支持按文件并发下载：`-concurrency`
- 并发下载时显示整体字节进度，避免大文件下载期间看起来像卡住
- 支持等待目标软件进程退出，避免 Windows 文件占用导致替换失败
- 运行时会输出目标软件当前版本和服务端最新版本，便于确认是否需要更新
- 支持动态进度条；也可用 `-no-progress` 关闭，便于第三方程序调用或日志重定向
- 同时支持独立 CLI 和 Go 库调用
- tag 推送后自动通过 GitHub Actions + GoReleaser 构建 Release

## 快速开始

先部署服务端项目：

```text
https://github.com/jwwsjlm/genUpdate_server
```

服务端中的应用名称必须和客户端 `-name` 一致。

从 Releases 下载客户端：

```text
https://github.com/jwwsjlm/genUpdate_client/releases
```

也可以源码编译：

```bash
git clone https://github.com/jwwsjlm/genUpdate_client.git
cd genUpdate_client
go build -trimpath -ldflags="-s -w" -o genUpdate_client .
```

Windows：

```bash
go build -trimpath -ldflags="-s -w" -o genUpdate_client.exe .
```

## CLI 用法

交互模式：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月
```

自动模式：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -y -no-wait
```

带 token：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -token your-token -y -no-wait
```

等待目标软件退出：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -process yourapp.exe -wait-timeout 2m -y -no-wait
```

并发下载：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -concurrency 3 -y -no-wait
```

被第三方程序调用或输出到日志文件时，建议关闭动态进度条：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -y -no-wait -no-progress
```

## 参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `-url` | 是 | 更新服务端地址，例如 `http://localhost:8090` |
| `-name` | 是 | 应用名称，必须和服务端应用名一致 |
| `-token` | 否 | 服务端开启 token 后传入访问 token |
| `-config` | 否 | 配置文件路径；默认尝试读取程序同目录 `genUpdate_client.json` |
| `-process` | 否 | 更新前等待退出的目标进程名，例如 `yourapp.exe` |
| `-wait-timeout` | 否 | 等待目标进程退出的最长时间，例如 `2m`；`0` 表示一直等待 |
| `-concurrency` | 否 | 并发下载数量，默认 `1` 表示顺序下载 |
| `-y` | 否 | 自动确认更新，不等待用户输入 Y/N |
| `-no-wait` | 否 | 程序结束后立即退出 |
| `-no-progress` | 否 | 关闭动态进度条，适合第三方程序调用、日志重定向或不支持动态刷新的终端 |

## 配置文件

在更新器同目录创建 `genUpdate_client.json`：

```json
{
  "url": "http://localhost:8090",
  "name": "星月",
  "token": "your-token",
  "process": "yourapp.exe",
  "concurrency": 3,
  "waitProcessTimeoutSeconds": 120,
  "autoYes": true,
  "noWait": true,
  "noProgress": false
}
```

Release 压缩包内会附带 `genUpdate_client.example.json`。可以把它复制为 `genUpdate_client.json` 后按实际环境修改。

然后直接运行：

```bash
./genUpdate_client
```

也可以指定配置文件：

```bash
./genUpdate_client -config ./config/update.json
```

命令行参数优先级更高，会覆盖配置文件中的同名设置。

## 作为 Go 库使用

安装：

```bash
go get github.com/jwwsjlm/genUpdate_client
```

直接更新：

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jwwsjlm/genUpdate_client/updater"
)

func main() {
	client, err := updater.New(updater.Options{
		BaseURL:            "http://localhost:8090",
		AppName:            "星月",
		Token:              "your-token",
		ProcessName:        "yourapp.exe",
		WaitProcessTimeout: 2 * time.Minute,
		Concurrency:        3,
		Writer:             os.Stdout,
		Progress:           true,
	})
	if err != nil {
		panic(err)
	}

	result, err := client.Run(context.Background())
	if err != nil {
		fmt.Println("更新失败:", err)
	}

	fmt.Printf("总计 %d, 下载 %d, 跳过 %d, 失败 %d\n",
		result.Total,
		result.Downloaded,
		result.Skipped,
		len(result.Failed),
	)
}
```

先展示公告，再执行更新：

```go
manifest, err := client.FetchManifest(context.Background())
if err != nil {
	panic(err)
}

fmt.Println(manifest.AppList.ReleaseNote.Version)
fmt.Println(manifest.AppList.ReleaseNote.Description)

result, err := client.Update(context.Background(), manifest)
```

## 更新流程

1. 请求 `{url}/updateList/{name}` 获取清单
2. 校验服务端返回状态
3. 等待目标进程退出，如果设置了 `process`
4. 逐个或并发处理文件
5. 本地文件大小一致时再计算 SHA256
6. 需要更新时下载到 `.tmp`
7. 下载后校验大小和 SHA256
8. 校验通过后替换目标文件

## 服务端响应示例

```json
{
  "appList": {
    "fileName": "星月",
    "ReleaseNote": {
      "appName": "星月",
      "description": "更新说明",
      "version": "1.0.0"
    },
    "fileList": [
      {
        "path": "星月/data/ip.dat",
        "name": "ip.dat",
        "size": 123456,
        "sha256": "文件 SHA256",
        "downloadURL": "/download/星月/data/ip.dat"
      }
    ]
  },
  "ret": "ok"
}
```

客户端会把 `path` 中应用目录之后的部分作为本地相对路径：

```text
星月/qqwry.dat -> qqwry.dat
星月/data/ip.dat -> data/ip.dat
```

为避免 token 泄漏，`downloadURL` 只允许相对路径，或与 `-url` 同源的绝对 URL。

## 安全性

- 下载完成后必须通过 SHA256 校验，否则不会替换正式文件
- 下载文件会先落到 `.tmp`，避免失败时破坏已有文件
- 会拒绝路径穿越和绝对路径
- 会拒绝跨域下载 URL，避免把 token 发送给第三方域名
- token 通过 `Authorization: Bearer <token>` 发送

建议生产环境使用 HTTPS，并妥善保护配置文件中的 token。

## 性能

- 大文件下载使用流式复制，不会把文件整体读入内存
- SHA256 使用流式计算
- 本地文件先比较大小，大小不一致时不再计算 SHA256
- `-concurrency` 可以提升多文件下载速度
- 默认顺序下载，输出更清晰，也减少对服务端的瞬时压力

## 发布新版本

项目已经配置 GitHub Actions 和 GoReleaser。推送 tag 后会自动构建 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

自动构建平台：

- Windows amd64
- Windows arm64
- Linux amd64
- Linux arm64
- macOS amd64
- macOS arm64

## 常见问题

### 目标软件正在运行怎么办

使用 `-process yourapp.exe`。更新器会等待进程退出后再替换文件。

### 软件当前版本从哪里读取

客户端会优先使用 `-process` 指向的 exe 读取 Windows 文件版本信息。如果只传了 `yourapp.exe`，会先在当前目录找，再到更新器所在目录找；找不到或 exe 没有版本信息时显示 `未知`。

### 为什么本地文件存在还会重新下载

服务端清单中的 `size` 或 `sha256` 与本地不一致时，会重新下载。

### 断点续传什么时候生效

下载中断后保留了 `.tmp` 文件，并且服务端支持 `Range` 时会生效。

### 并发越高越好吗

不一定。并发会增加服务端压力，也会让日志更密集。一般建议从 `2` 或 `3` 开始。并发模式下进度条显示的是所有文件的整体字节进度；如果某个大文件还在下载，进度会继续刷新，不会只停在“已完成文件数”上。

### 进度条停在 50% 是卡死了吗

`v0.4.0` 的并发模式只按文件数量显示进度：两个文件时，小文件完成后会显示 50%，大文件下载期间没有字节进度，所以容易看起来像卡住。新版本并发模式改为整体字节进度，可以看到大文件下载过程。

### 进度条出现 `\x1b[36m` 这样的字符怎么办

这是终端没有正确解释颜色控制码或动态刷新字符。当前版本默认使用纯文本进度条，不再输出颜色控制码；如果仍然由第三方程序捕获输出，建议加 `-no-progress`。

### 为什么双击或从目标软件里启动会弹出新窗口

Windows 控制台程序如果不是从已有 CMD/PowerShell 里启动，系统通常会给它新建一个控制台窗口。当前 Release 构建是控制台程序，没有使用 `-H windowsgui`；Windows amd64 构建也嵌入了 `asInvoker` manifest，避免因为文件名包含 `Update` 被系统误判为安装器并触发提权。若希望复用原来的 CMD 窗口，需要从那个 CMD/PowerShell 里直接运行，或让目标软件启动更新器时继承标准输入输出。若只想后台更新，可以由目标软件以库方式调用，或启动 CLI 时使用 `-y -no-wait -no-progress`。

## License

MIT License
