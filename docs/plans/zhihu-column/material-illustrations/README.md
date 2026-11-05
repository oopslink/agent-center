# 七篇稿件的完整材质插画

已完成 26 张配图并嵌入七篇稿件；加上 2 张实际页面与 3 张论文原图，正文共 31 张。正式入口是[完整图文稿件](../publish-bundle/index.html)和[配图审阅](../figure-gallery.html)。

## 视觉约定

参考 [guizang-material-illustration](https://github.com/op7418/guizang-material-illustration) 的材质插画方法。该来源是技能说明，不表示当前环境安装了同名插件。用户明确指定的 Q 版人形机器人及丰富色彩，覆盖原技能的单强调色等默认建议。

- 角色：大头、小身体、深色面屏与双眼，完整双臂、双腿和手脚；不同角色保持同一造型家族。
- 材质：哑光陶瓷与纸张，暖白场景，柔和投影；模型、任务与证据用具体物体解释。
- 色彩：钴蓝 `#3158D8`、珊瑚橙 `#F18C73`、薄荷绿 `#76BFA8`、柔黄 `#E8C76C`、淡紫 `#A89ACB`。颜色辅助辨认，含义同时用文字标明。
- 文字：图中用短标签，条件、来源与局限放在紧邻图的正文和图注。
- 边界：插画是机制概括、说明性场景或数据展示，不冒充产品页面、真实聊天或原始运行轨迹。

## 文件与复现

`assets/` 保存 26 张正式 PNG，均为 1672 × 941。`02-workitem-origin` 沿用批准的试稿，提示词见[试稿记录](../guizang-trial/PROMPTS.md)。`03-execution-lanes` 已按用户指出的身份边界问题重绘：只用一个机器人代表整体 Agent，Supervisor 和 Executor 放在同一逻辑边界内，以内部组件表达；新版提示词见 [supervisor-boundary-generation.json](supervisor-boundary-generation.json)。原三机器人试稿仅作历史记录，不再用于正式正文。原先其余 23 张的初始提示词位于 [manifest.json](manifest.json)；最终提示词、生成文件来源及淘汰记录位于 [outputs.json](outputs.json)。费用增幅与读取分布经过重生成，最终以数字表呈现。

图片由图像生成工具生成，按生成结果原样复制，不进行程序化像素修改。原图保留在生成目录。`build_gallery.py` 仅处理 Markdown/HTML、图片复制、引用检查及 ZIP 打包，不重新生成插画。

```sh
python3 docs/plans/zhihu-column/material-illustrations/build_gallery.py
```

脚本需要 Python 的 `markdown` 与 `Pillow`，生成本地阅读页、七篇可编辑源稿、全部图片及 `zhihu-column-illustrated.zip`。补充 Mermaid 保留源码，阅读页中可展开查看；其主要关系有单独 PNG 配图。

## 数字与检查

见 [QA.md](QA.md) 和 [validation.json](validation.json)。图表数据仍以正文引用的论文、代码和假设为准；图片风格更新不构成新实验，也没有重新证明原论文结论。本文记录的是本轮图文制作检查，不是第三方审稿或独立统计复核。

新增的开篇情境图 `02-task-journey.png` 放在现第 4 篇“需求动机”中，先解释工作流转，再引出模型职责。完整生成与箭头修订提示词见 [task-journey-generation.json](task-journey-generation.json)。

阅读顺序已调整为身份与上下文 → Supervisor–Executor → Task → 工作流 → Plan/DAG → 记忆 → 开发实践。素材文件名保留原编号作为稳定 ID，正文及审阅页按新篇次组织。
