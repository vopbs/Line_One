# Windows 打包与交付说明

## 目标

生成可分发的 Windows 压缩包。使用者解压后无需安装 Go，双击 LineOnePhone.exe 即可启动网页话机。

压缩包默认只使用 127.0.0.1，不包含真实 SIP 服务器、账号或密码。

## 打包环境

- Go 1.22 或更新版本
- PowerShell 5 或更新版本

## 打包命令

在项目根目录执行：

`powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\package_v1_windows.ps1
`

生成文件：

`	ext
dist\v1-windows-amd64.zip
`

## 压缩包内容

`	ext
v1\
  LineOnePhone.exe
  phone-cli.exe
  .env.example
  web\
  docs\
`

## 用户运行方式

1. 解压 zip。
2. 双击 LineOnePhone.exe。
3. 程序自动打开默认浏览器到 http://127.0.0.1:8080。
4. 在网页中填写自己的 SIP 注册地址、账号和密码。
5. 退出时点击页面右上角 退出程序，以释放端口。

## 网络配置

本机测试默认即可：

`	ext
HTTP_ADDR=127.0.0.1:8080
ADVERTISE_IP=127.0.0.1
SIP_LOCAL_PORT=5066
RTP_PORT=40000
`

如果需要对接真实 SIP 服务器，可复制 .env.example 为 .env，并把 ADVERTISE_IP 改成 SIP 服务器可访问到的本机 IP。

如果部署在 NAT 后面，需要映射：

- SIP_LOCAL_PORT/UDP
- RTP_PORT/UDP

非本机访问网页时，需要 HTTPS，否则浏览器可能拒绝麦克风权限。
