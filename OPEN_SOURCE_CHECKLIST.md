# GitHub 发布检查清单

- [ ] 添加并确认 `LICENSE`（MIT、Apache-2.0 或 GPL-3.0 等）。
- [ ] 确认 `.env` 不被提交，且 `.env.example` 不含真实服务器、账号或密码。
- [ ] 确认日志、pcap/pcapng 抓包、导出的信令和 `dist/` 未被提交。
- [ ] 执行 `go test ./...`。
- [ ] 执行 `node --check web/app.js`。
- [ ] 检查 README 的端口、启动、退出和抓包说明是否与当前版本一致。
- [ ] 将 Windows zip 上传到 GitHub Release，而不是提交到仓库。
- [ ] 创建版本标签，例如 `v1.0.0`。
- [ ] 在 Release Notes 中写明已知限制和免责声明。
