# GitHub 发布指南

## 项目说明

Line One WebRTC SIP Phone 是一个本地运行的 WebRTC 到 SIP/UDP 网关：

```text
Browser WebRTC <-> Go/Pion gateway <-> SIP/UDP + RTP <-> SIP server
```

当前支持 SIP UDP 注册、外呼、挂断、PCMU/PCMA、耳麦通话、音频文件播报、信令查看和导出。

## Windows 使用

1. 从 GitHub Release 下载 Windows zip。
2. 解压后双击 `LineOnePhone.exe`。
3. 程序无黑窗运行并自动打开默认浏览器。
4. 在网页填写 SIP 注册地址、账号和密码。
5. 退出时点击页面右上角“退出程序”，以释放端口。

关闭浏览器标签页不会退出后台程序。重复双击 `LineOnePhone.exe` 时，程序会尝试打开已有网页。

## 默认端口

| 用途 | 默认端口 |
| --- | --- |
| 网页服务 | `127.0.0.1:8080/TCP` |
| 本地 SIP | `5066/UDP` |
| 本地 RTP | `40000/UDP` |

SIP 服务端口以网页填写的地址为准，不固定为 `5060`。例如服务器地址为 `119.188.95.103:7080`，可使用 Wireshark 过滤器：

```text
udp.port == 5066 || udp.port == 7080
```

RTP 可使用：

```text
udp.port == 40000
```

## 已知限制

- 单账号、单路通话。
- 仅支持 SIP UDP，不支持 SIP TCP/TLS。
- 仅支持主动外呼，不支持来电接听。
- G.729 当前未启用。
- 信令记录仅存在内存中，程序重启后会清空。
- NAT、代理和 Record-Route 场景需按实际 SIP 报文验证。

## 安全与隐私

- 不要提交 `.env`、日志、抓包或导出的信令文件。
- 页面会将保存的 SIP 配置和密码存到当前浏览器 `localStorage`。请不要在共享或不可信电脑上保存配置。
- 信令和日志可能包含号码、IP、Call-ID、SDP 和服务器信息，公开前必须脱敏。
- Windows zip 应上传到 GitHub Release，不应提交到 Git 仓库。

## 发布前必做

1. 添加明确的 `LICENSE` 文件。
2. 执行 `go test ./...` 和 `node --check web/app.js`。
3. 确认 `.env.example` 不含真实账号、密码或服务器地址。
4. 检查仓库和 Git 历史不含真实账号、密码、Token 或私钥。
5. 在 Release Notes 说明已知限制、端口和免责声明。
