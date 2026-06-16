# v2.10.3 Tester2 §3/§4 证据索引

候选 `origin/v2.10.3-int @80794195` · 真 install test-instance `v2103acc` · 真浏览器 · run-real 26/26 PASS。
可一键复现：`AC_BASE=… node tests/e2e/v2/capture-v2103.mjs`（或 `AC_SPAWN=1 BIN=… node …` 自起真实例）。

## T167 全会话附件（真浏览器，人端）
| 文件 | 内容 |
|---|---|
| `t167_channel_1_open.png` | Channel 入口（真实 nav 到达）|
| `t167_channel_2_image_sent.png` | Channel 图片预览卡已发送 |
| `t167_channel_3_file_sent.png` | Channel 图片卡 + 文件卡 |
| `t167_channel_4_both_attachments_light.png` | Channel 附件 light |
| `t167_channel_4_both_attachments_dark.png` | Channel 附件 dark（右栏 Participants: Owner+Sandbox Agent 共享）|
| `t167_dm_{1_open,2_image_sent,3_file_sent,4_both_attachments_light}.png` | DM 私信（与真 agent）图片+文件 |
| `t167_plan_{1_open,2_image_sent,3_file_sent,4_both_attachments_light}.png` | **Plan chat**（T167 本体修复点）图片预览卡+文件卡 |
| `t167_task_{1_open,2_image_sent,3_file_sent,4_both_attachments_light}.png` | Task 讨论会话 图片+文件 |
| `backend-t167-agent-files.txt` | agent 文件工具 4/4 PASS（参与 plan 上传+下载 / 非参与·跨 org 403 / post_message 带附件）|

## T170 issue 生命周期（真浏览器 + 后端）
| 文件 | 内容 |
|---|---|
| `t170_1_issue_open.png` | issue 创建/open 态 |
| `t170_3_issue_closed.png` | Edit 弹窗改 → 渲染 CLOSED |
| `t170_4_issue_reopened.png` | Edit 弹窗改 → 渲染 REOPENED |
| `t170_2_issue_discussion.png` | issue 讨论空态（预期，关联会话由 agent comment_issue 创建）|
| `backend-t170-agent-issues.txt` | agent issue 7 工具 19/19 PASS（成功+权限+校验）|

## 汇总
| 文件 | 内容 |
|---|---|
| `results.json` | capture 脚本逐项 PASS/FAIL 机读结果（26/26 PASS）|
| `git_ls_tree_proof.txt` | §4 verify-in-tree 实证（证据在被 tag commit 的 tree 里）|
