# Line One WebRTC SIP Phone

本项目是一个本地运行的 WebRTC 到 SIP/UDP 网关，用于在浏览器网页话机与 SIP 服务器（例如 VOS3000）之间建立外呼音频通话。

```text
Browser WebRTC <-> Go/Pion gateway <-> SIP/UDP + RTP <-> SIP server
```

> 使用本项目即表示你同意自行承担合法合规使用责任。请先阅读 [免责声明](免责声明.md) 和 [安全说明](SECURITY.md)。

## 功能

- SIP UDP 注册、保活、外呼、挂断和远端挂断。
- PCMU/G.711U 和 PCMA/G.711A 音频。
- 耳麦实时通话或音频文件播报。
- SIP 信令查看、导出和最近通话记录。
- SIP 密码变更后的保活检测与重新登录提示。
- Windows 无黑窗启动并自动打开默认浏览器。
- 页面内退出程序，释放 HTTP、SIP 和 RTP 端口。

## Windows 使用

1. 从 GitHub Release 下载 Windows zip。
2. 解压后双击 `LineOnePhone.exe`。
3. 程序会在后台启动并自动打开默认浏览器。
4. 在网页填写 SIP 注册地址、账号和密码。
5. 退出时点击页面右上角“退出程序”。关闭浏览器标签页不会退出后台程序。

## 默认端口

| 用途 | 默认端口 |
| --- | --- |
| 网页服务 | `127.0.0.1:8080/TCP` |
| 本地 SIP | `5066/UDP` |
| 本地 RTP | `40000/UDP` |

SIP 服务端口以网页填写的地址为准，不固定为 `5060`。例如服务器为 `119.188.95.103:7080`，可用 Wireshark 过滤器：

```text
udp.port == 5066 || udp.port == 7080
```

RTP 可使用：

```text
udp.port == 40000
```

## 从源码运行

要求：Go 1.22 或更新版本。

```powershell
go test ./...
go run ./cmd/gateway
```

默认页面地址：<http://127.0.0.1:8080>。

Windows 打包：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\package_v1_windows.ps1
```

## 已知限制

- 单账号、单路通话。
- 仅支持 SIP UDP；暂不支持 SIP TCP/TLS。
- 仅支持主动外呼，暂不支持来电接听。
- G.729 当前未启用。
- 信令记录仅存在内存中，程序重启后会清空。
- NAT、SIP 代理和 Record-Route 场景需按实际 SIP 报文验证。

## 安全与隐私

- 不要提交 `.env`、日志、抓包或导出的信令文件。
- 页面会将保存的 SIP 配置和密码存到当前浏览器 `localStorage`，不要在共享或不可信电脑上保存配置。
- 信令和日志可能包含号码、IP、Call-ID、SDP 和服务器信息，公开前必须脱敏。
- 详见 [SECURITY.md](SECURITY.md)、[免责声明.md](免责声明.md) 和 [docs/GITHUB_RELEASE.md](docs/GITHUB_RELEASE.md)。

## 发布前

请先根据 [OPEN_SOURCE_CHECKLIST.md](OPEN_SOURCE_CHECKLIST.md) 完成检查，并在根目录添加明确的 `LICENSE` 文件。Windows zip 建议上传到 GitHub Release，而不是提交到仓库。
