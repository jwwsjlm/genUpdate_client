# 通用更新客户端

📦 自动更新客户端，与服务端配合实现软件自动更新。

[![GitHub Release](https://img.shields.io/github/v/release/jwwsjlm/genUpdate_client)](https://github.com/jwwsjlm/genUpdate_client/releases)
[![License](https://img.shields.io/github/license/jwwsjlm/genUpdate_client)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.21-blue)](https://golang.org)

---

## ✨ 功能特性

- 🔄 **自动更新** - 自动检测并更新到最新版本
- 🔐 **SHA256 校验** - 下载前比对本地文件，下载后再次校验完整性
- 🧱 **原子替换** - 先下载到临时文件，再覆盖正式文件
- 🛠️ **易于集成** - 简单参数即可接入
- 🤖 **支持自动模式** - 支持无交互运行

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

### 命令行参数

```bash
./genUpdate_client -url http://your-server.com:8090 -name 你的软件名
```

### 常用参数

- `-url`：服务端地址
- `-name`：应用名称
- `-y`：自动确认更新，无需交互
- `-no-wait`：程序结束后立即退出

### 自动模式示例

```bash
./genUpdate_client -url http://localhost:8090 -name 星月 -y -no-wait
```

---

## 🔗 配套服务端

本项目需要配合 [genUpdate_server](https://github.com/jwwsjlm/genUpdate_server) 使用。

**服务端功能：**
- 版本管理
- 文件存储
- SHA256 校验
- 稳定下载链接

---

## 💡 更新流程

1. 获取版本清单：`/updateList/{appName}`
2. 对比本地文件 SHA256
3. 下载缺失或变更文件到临时文件
4. 校验下载文件 SHA256
5. 原子替换正式文件

---

## 📸 响应示例

访问：`http://localhost:8090/updateList/星月`

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
        "sha256": "...",
        "downloadURL": "/download/星月/qqwry.dat"
      }
    ]
  },
  "ret": "ok"
}
```

---

## 📄 许可证

MIT License

---

**如果有帮助，欢迎 Star ⭐️！**
