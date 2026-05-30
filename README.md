# genUpdate_client

通用更新客户端，配合 [genUpdate_server](https://github.com/jwwsjlm/genUpdate_server) 使用，用来从服务端获取更新清单、下载变更文件，并在校验通过后替换到本地目录。

[![GitHub Release](https://img.shields.io/github/v/release/jwwsjlm/genUpdate_client)](https://github.com/jwwsjlm/genUpdate_client/releases)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.24-blue)](https://golang.org)

## 适用场景

- 你的软件需要一个独立的更新程序。
- 更新文件由 `genUpdate_server` 管理和提供下载。
- 客户端只负责拉取清单、对比本地文件、下载缺失或变更文件。

## 功能

- 获取服务端更新清单：`/updateList/{appName}`
- 本地文件大小和 SHA256 对比，未变化则跳过下载
- 下载后再次校验文件大小和 SHA256
- 使用临时文件下载，成功后再替换目标文件
- 支持自动确认模式，适合被主程序或脚本调用
- 支持 GitHub tag 自动构建 Release 产物

## 快速使用

### 1. 准备服务端

先部署并配置服务端项目：

```text
https://github.com/jwwsjlm/genUpdate_server
```

服务端中配置的应用名称需要和客户端参数 `-name` 一致。例如服务端应用名是 `星月`，客户端也必须传入 `-name 星月`。

### 2. 下载客户端

可以从 Releases 下载已经编译好的程序：

```text
https://github.com/jwwsjlm/genUpdate_client/releases
```

也可以自己编译：

```bash
git clone https://github.com/jwwsjlm/genUpdate_client.git
cd genUpdate_client
go build -trimpath -ldflags="-s -w" -o genUpdate_client .
```

Windows 下可以编译为：

```bash
go build -trimpath -ldflags="-s -w" -o genUpdate_client.exe .
```

### 3. 运行更新

交互模式：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月
```

自动模式：

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -y -no-wait
```

Windows 示例：

```powershell
.\genUpdate_client.exe -url http://localhost:8090 -name 星月 -y -no-wait
```

## 命令参数

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `-url` | 是 | 更新服务端地址，例如 `http://localhost:8090` |
| `-name` | 是 | 应用名称，必须和服务端中的应用名一致 |
| `-token` | 否 | 服务端开启 token 后传入访问 token |
| `-y` | 否 | 自动确认更新，不再等待用户输入 Y/N |
| `-no-wait` | 否 | 程序结束后立即退出，不等待回车 |
| `-config` | 否 | 指定配置文件路径；不传时会尝试读取程序同目录的 `genUpdate_client.json` |
| `-process` | 否 | 更新前等待退出的目标进程名，例如 `yourapp.exe` |
| `-wait-timeout` | 否 | 等待目标进程退出的最长时间，例如 `2m`；不传或为 `0` 表示一直等待 |

## 配置文件

如果不想每次都写命令行参数，可以在更新器同目录创建 `genUpdate_client.json`：

```json
{
  "url": "http://localhost:8090",
  "name": "星月",
  "token": "your-token",
  "process": "yourapp.exe",
  "waitProcessTimeoutSeconds": 120,
  "autoYes": true,
  "noWait": true
}
```

然后直接运行：

```bash
./genUpdate_client
```

Windows 下可以直接双击 `genUpdate_client.exe`，或者在快捷方式中启动它。

也可以指定其他配置文件：

```bash
./genUpdate_client -config ./config/update.json
```

命令行参数优先级更高。如果配置文件里写了 `name`，但启动时又传了 `-name 其他应用`，最终会使用命令行里的值。

## Token 鉴权

如果服务端配置了应用 token，客户端需要传入相同 token：

```bash
genUpdate_client.exe -url http://localhost:8090 -name 星月 -token your-token -y -no-wait
```

客户端会在请求清单和下载文件时发送：

```text
Authorization: Bearer your-token
```

这个格式和服务端的 `GENUPDATE_APP_TOKENS` 配置兼容。

## 目标软件正在运行怎么办

如果目标软件正在运行，Windows 下相关文件可能会被占用，导致更新器无法替换文件。

推荐做法是在启动更新时指定目标进程名：

```bash
genUpdate_client.exe -url http://localhost:8090 -name 星月 -process yourapp.exe -y -no-wait
```

更新器会先检测 `yourapp.exe` 是否仍在运行。如果还在运行，会提示用户关闭，并显示已等待时间；进程退出后自动继续更新。

也可以设置等待超时时间：

```bash
genUpdate_client.exe -url http://localhost:8090 -name 星月 -process yourapp.exe -wait-timeout 2m -y -no-wait
```

如果超过 `2m` 目标进程仍未退出，更新会停止，避免一直卡住。

## 集成方式

这个项目当前是一个独立的命令行更新器。你不一定要通过“第三方 CLI 软件”调用它，只要能启动外部进程就可以集成。

常见方式：

- 用户手动双击或在终端运行
- 主程序在退出前或启动前调用更新器
- 使用 `.bat`、PowerShell、shell 脚本调用
- 使用 Windows 计划任务或系统服务定时调用
- 由安装器、启动器、托盘程序调用

主程序调用更新器时，推荐使用自动模式：

```bash
genUpdate_client.exe -url http://localhost:8090 -name 星月 -y -no-wait
```

Go 程序中调用示例：

```go
cmd := exec.Command(
	"genUpdate_client.exe",
	"-url", "http://localhost:8090",
	"-name", "星月",
	"-y",
	"-no-wait",
)
err := cmd.Run()
```

C# 程序中调用示例：

```csharp
var process = new Process();
process.StartInfo.FileName = "genUpdate_client.exe";
process.StartInfo.Arguments = "-url http://localhost:8090 -name 星月 -y -no-wait";
process.StartInfo.UseShellExecute = false;
process.Start();
process.WaitForExit();
```

## 作为 Go 库使用

如果你的项目也是 Go，可以不启动外部 exe，直接引入更新库：

```bash
go get github.com/jwwsjlm/genUpdate_client
```

示例：

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

如果你想先展示版本公告，再由用户确认后更新，可以拆成两步：

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

1. 请求服务端接口：`{url}/updateList/{name}`
2. 读取服务端返回的文件列表
3. 对本地文件先比较大小，大小一致时再比较 SHA256
4. 本地文件缺失或校验不一致时，下载到 `.tmp` 临时文件
5. 校验下载文件的大小和 SHA256
6. 校验通过后替换目标文件

## 服务端响应示例

访问：

```text
http://localhost:8090/updateList/星月
```

示例响应：

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
        "path": "星月/qqwry.dat",
        "name": "qqwry.dat",
        "size": 123456,
        "sha256": "文件 SHA256",
        "downloadURL": "/download/星月/qqwry.dat"
      }
    ]
  },
  "ret": "ok"
}
```

客户端会把 `path` 中应用目录之后的部分作为本地相对路径。例如：

```text
星月/qqwry.dat -> qqwry.dat
星月/data/ip.dat -> data/ip.dat
```

## 发布新版本

项目已经配置 GitHub Actions 和 GoReleaser。推送 tag 后会自动编译并发布 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

自动构建的平台：

- Windows amd64
- Windows arm64
- Linux amd64
- Linux arm64
- macOS amd64
- macOS arm64

Release 中会包含压缩包和 `checksums.txt`。

## 常见问题

### 更新失败或文件无法替换

请先关闭正在运行的目标软件。Windows 下如果目标程序仍在运行，相关文件可能会被占用，导致替换失败。

### 本地文件存在，为什么还会重新下载

客户端会比较服务端清单里的 `size` 和 `sha256`。只要大小或 SHA256 不一致，就会重新下载。

### `-name` 应该填什么

填写服务端中配置的应用名称。它会用于请求：

```text
/updateList/{name}
```

也会用于解析服务端返回的文件路径。

## 许可证

MIT License
