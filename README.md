# cc-connect（个人改版）

基于上游 [chenhg5/cc-connect](https://github.com/chenhg5/cc-connect) 的 `main` 分支，只列出与上游的区别。用法和文档见上游仓库。

## 与上游的区别

**Agent / 显示**
- Claude Code `/usage` 探测：支持新版 Claude Code 的 CSI G 光标定位；信任目录提示光标默认在 “No” 时先下移再确认。
- `[display] hide_tool_results = true`：隐藏工具结果消息，保留工具调用消息。
- 独立的工具调用消息也按 `tool_max_len` 截断（上游只截断卡片里的）。
- 后台任务在本轮结束后才产出的消息（如 ACP 后台任务）也遵守 `NO_REPLY`。
- ACP：配置里的 `reasoning_effort` 正确显示在页脚（上游会显示成第一档 low）。

**Relay**
- `relay send` 默认在目标 bot 上新开会话，加 `--resume` 才续上次的 relay 对话。
- `relay send --async`：立即返回；对方完成后，回复以「[relay 回信]」开头作为新消息注入发起方会话（会话忙时每 5 秒重试，最多 1 小时）。

**Telegram**
- 发送和编辑消息都不带链接预览。
- 相册（`media_group_id`）多图合并成一条消息。
- 回复消息时只带选中的引用；回复 bot 自己的消息只带前 200 字。
- 贴纸转成图片（动态 / 视频贴纸用缩略图）并附 emoji；贴纸等 2 秒与下一条消息合并，收到的贴纸记录到 `~/.cc-connect/stickers.jsonl`。
- 每条消息等 0.8 秒，期间到达的转发消息并入同一轮。

## 协议

MIT，见 [LICENSE](LICENSE)。上游版权归 chenhg5 及 cc-connect 贡献者。
