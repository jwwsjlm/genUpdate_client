# 通用更新客户端

📦 自动更新客户端，与服务端配合实现软件自动更新。

[![GitHub Release](https://img.shields.io/github/v/release/jwwsjlm/genUpdate_client)](https://github.com/jwwsjlm/genUpdate_client/releases)
[![License](https://img.shields.io/github/license/jwwsjlm/genUpdate_client)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)

---

## ✨ 功能特性

- 🔄 **自动更新** - 自动检测并更新到最新版本
- 🔐 **SHA256 校验** - 确保下载文件完整性
- 📊 **版本同步** - 与服务端版本保持一致
- 🛠️ **易于集成** - 简单配置即可使用

---

## 🚀 快速开始

### 方式一：下载编译好的程序

从 [Releases](https://github.com/jwwsjlm/genUpdate_client/releases) 下载最新版本

### 方式二：源码编译

```bash
git clone https://github.com/jwwsjlm/genUpdate_client.git
cd genUpdate_client
go build -o genUpdate_client .
```

---

## 📖 使用说明

### 配置

修改源码中的配置：

```go
// 服务端地址
baseURL := "http://your-server.com"

// 应用名称
appName := "你的软件名"
```

### 运行

```bash
./genUpdate_client
```

---

## 🔗 配套服务端

本项目需要配合 [genUpdate_server](https://github.com/jwwsjlm/genUpdate_server) 使用。

**服务端功能：**
- 版本管理
- 文件存储
- SHA256 校验
- 临时下载链接

---

## 💡 自定义下载器

如果你想自己实现下载器，核心逻辑如下：

```go
// 1. 获取版本信息
resp := http.Get(baseURL + "/updateList/" + appName)
data := parseJSON(resp)

// 2. 对比本地文件 SHA256
localSHA256 := calculateSHA256(localFile)
if localSHA256 != data.fileList[i].sha256 {
    // 3. 下载更新
    downloadURL := baseURL + data.fileList[i].downloadURL
    downloadFile(downloadURL)
}
```

---

## 📸 示例

### 服务端示例

访问：http://up.975135.xyz/updateList/星月

**响应示例：**
```json
{
  "appList": {
    "fileName": "星月",
    "ReleaseNote": {
      "appName": "星月",
      "version": "1.0.0"
    },
    "fileList": [...]
  },
  "ret": "ok"
}
```

---

## 🛠️ 技术栈

- **语言:** Go
- **校验:** SHA256
- **协议:** HTTP/HTTPS

---

## 📄 许可证

MIT License

---

## 📬 联系方式

- GitHub: [@jwwsjlm](https://github.com/jwwsjlm)
- 博客：https://blog.xsojson.com

---

**如果有帮助，欢迎 Star ⭐️！**
