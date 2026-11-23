# cc-connect（个人改版）

基于上游 [chenhg5/cc-connect](https://github.com/chenhg5/cc-connect) 的 `main` 分支，只列出与上游的区别。用法和文档见上游仓库。

## 与上游的区别

**Agent / 显示**
- Claude Code `/usage` 探测：支持新版 Claude Code 的 CSI G 光标定位；信任目录提示光标默认在 “No” 时先下移再确认。
- `[display] hide_tool_results = true`：隐藏工具结果消息，保留工具调用消息。
- 独立的工具调用消息也按 `tool_max_len` 截断（上游只截断卡片里的）。
- 后台任务在本轮结束后才产出的消息（如 ACP 后台任务）也遵守 `NO_REPLY`。
- ACP：本轮结束后才到的回复（如 agy 后台任务完成后继续说话）等 1.5 秒没有新内容再作为一条消息发出（上游逐块各发一条）。
- ACP：配置里的 `reasoning_effort` 正确显示在页脚（上游会显示成第一档 low）。

**Relay**
- `relay send` 默认在目标 bot 上新开会话，加 `--resume` 才续上次的 relay 对话。
- `relay send --async`：立即返回；对方完成后，回复以「[relay 回信]」开头作为新消息注入发起方会话（会话忙时每 5 秒重试，最多 1 小时）。

**Telegram**
- 平台选项 `rich_messages = false`：不用上游的 Bot API 10.x 富文本消息（`sendRichMessage`），改用普通消息——富文本在客户端里不能部分引用回复、不能部分复制。默认仍开启。
- 富文本发送（上游 `sendRichMessage`，本身不出链接预览）失败、回退到普通 `sendMessage` / `editMessageText` 时，不带链接预览。
- 相册（`media_group_id`）的多张图等 0.6 秒合并成一条消息。
- 回复消息：用户选中了部分文字时只带选中的引用；回复 bot 自己的消息时只带原文前 200 字（上游带全文）。
- 贴纸转成图片发给 agent（动态 / 视频贴纸用缩略图），内容为 `[贴纸 emoji]`；贴纸等 2 秒与下一条消息合并成一轮。
- 收到的贴纸（贴纸包名、emoji、file_id）追加记录到 `~/.cc-connect/stickers.jsonl`，只做记录；cc-connect 本身不提供发贴纸功能，由 [`tools/tg-sticker`](tools/tg-sticker) 负责。
- 每条消息等 0.8 秒，期间到达的转发消息并入同一轮；群里跟在 @bot 消息后面的转发即使没 @ 也会并入。

## 附带工具

- [`tools/tg-sticker`](tools/tg-sticker)：让 cc-connect 里的 agent 发 Telegram 贴纸（Python 3.11+，无第三方依赖）。在 agent 的 shell 里运行，依赖 cc-connect 注入的 `CC_PROJECT` / `CC_SESSION_KEY`，bot token 运行时从 `~/.cc-connect/config.toml` 读取。
  - `list [关键字]` 列出贴纸（id、emoji、贴纸包、备注、静态预览图路径），`send <id|emoji|备注关键字>` 发到当前会话，`note <id> <备注>` 写备注。
  - 贴纸包手动管理：`add <包名|t.me/addstickers 链接>`、`remove <包名>`、`packs`；`seen` 列出用户最近发过但未入库的包。
  - 数据在 `~/.cc-connect/stickers/`：`packs.json`（入库的包，所有 bot 共用）、`notes.json`（备注，共用）、`library-<项目>.json`（file_id 按 bot 区分）和预览图。
  - 安装：`ln -s "$PWD/tools/tg-sticker" ~/.local/bin/tg-sticker`

## 协议

MIT，见 [LICENSE](LICENSE)。上游版权归 chenhg5 及 cc-connect 贡献者。
