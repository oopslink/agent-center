import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'
import { fileURLToPath, URL } from 'node:url'

export default withMermaid(defineConfig({
  vite: {
    resolve: {
      alias: [
        // markdown 源在 ../docs/，跟 sites/node_modules 跨目录；Node v25 + vite rollup 对
        // 'vue' / 'vue/server-renderer' 等裸 specifier 从 docs/ 解析失败，显式 alias 到本仓 node_modules
        {
          find: /^vue\/server-renderer$/,
          replacement: fileURLToPath(new URL('./node_modules/vue/server-renderer/index.mjs', new URL('../', import.meta.url))),
        },
        {
          find: /^vue$/,
          replacement: fileURLToPath(new URL('./node_modules/vue/dist/vue.runtime.esm-bundler.js', new URL('../', import.meta.url))),
        },
        // dayjs 是 CJS 包（main: dayjs.min.js），dev 模式下 ESM module loader 没拿到 default export；
        // 直接 alias 到 ESM 入口 dayjs/esm/index.js
        {
          find: /^dayjs$/,
          replacement: fileURLToPath(new URL('./node_modules/dayjs/esm/index.js', new URL('../', import.meta.url))),
        },
      ],
    },
  },
  title: 'agent-center · DDD 设计',
  description: 'agent-center DDD 设计文档可视化站点（战略 / 战术 / ADR / 实现）',
  lang: 'zh-CN',
  base: '/',

  // markdown 源在 ../docs（项目 root 下的 docs/）；sites/ 仅承载脚手架
  srcDir: '../docs',

  // 清晰 URL（去 .html 后缀）
  cleanUrls: true,
  lastUpdated: true,

  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      {
        text: '战略层',
        items: [
          { text: '战略层 README', link: '/design/architecture/strategic/00-domain-vision' },
          { text: '领域愿景 Domain Vision', link: '/design/architecture/strategic/00-domain-vision' },
          { text: '子域分类 Subdomain', link: '/design/architecture/strategic/01-subdomain-classification' },
          { text: '系统总览 System Overview', link: '/design/architecture/strategic/02-system-overview' },
          { text: '限界上下文 + UL', link: '/design/architecture/strategic/03-bounded-contexts' },
        ],
      },
      {
        text: '战术层 / 各 BC',
        items: [
          { text: 'BC1 TaskRuntime', link: '/design/architecture/tactical/task-runtime/00-overview' },
          { text: 'BC2 Discussion', link: '/design/architecture/tactical/discussion/00-overview' },
          { text: 'BC3 Workforce', link: '/design/architecture/tactical/workforce/00-overview' },
          { text: 'BC4 Cognition', link: '/design/architecture/tactical/cognition/00-overview' },
          { text: 'BC5 Observability', link: '/design/architecture/tactical/observability/00-overview' },
          { text: 'BC6 Conversation', link: '/design/architecture/tactical/conversation/00-overview' },
          { text: 'BC7 Bridge', link: '/design/architecture/tactical/bridge/00-overview' },
        ],
      },
      { text: 'ADR', link: '/design/decisions/' },
      { text: '蓝图', link: '/design/ddd-blueprint' },
      { text: '规约', link: '/rules/conventions' },
    ],

    sidebar: {
      '/design/architecture/strategic/': [
        {
          text: '战略层',
          items: [
            { text: '0. 领域愿景', link: '/design/architecture/strategic/00-domain-vision' },
            { text: '1. 子域分类', link: '/design/architecture/strategic/01-subdomain-classification' },
            { text: '2. 系统总览', link: '/design/architecture/strategic/02-system-overview' },
            { text: '3. 限界上下文 + UL', link: '/design/architecture/strategic/03-bounded-contexts' },
          ],
        },
      ],
      '/design/architecture/tactical/': [
        {
          text: 'BC1 TaskRuntime',
          collapsed: false,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/task-runtime/00-overview' },
            { text: 'Task 聚合', link: '/design/architecture/tactical/task-runtime/01-task' },
            { text: 'TaskExecution 聚合', link: '/design/architecture/tactical/task-runtime/02-task-execution' },
            { text: 'InputRequest 聚合', link: '/design/architecture/tactical/task-runtime/03-input-request' },
          ],
        },
        {
          text: 'BC2 Discussion',
          collapsed: true,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/discussion/00-overview' },
          ],
        },
        {
          text: 'BC3 Workforce',
          collapsed: true,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/workforce/00-overview' },
            { text: 'Worker 聚合 + Mapping', link: '/design/architecture/tactical/workforce/01-worker' },
            { text: 'Project 聚合', link: '/design/architecture/tactical/workforce/02-project' },
            { text: 'WorkerProjectProposal 聚合', link: '/design/architecture/tactical/workforce/03-worker-project-proposal' },
          ],
        },
        {
          text: 'BC4 Cognition',
          collapsed: true,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/cognition/00-overview' },
            { text: 'SupervisorInvocation 聚合 + DecisionRecord', link: '/design/architecture/tactical/cognition/01-supervisor-invocation' },
            { text: 'Memory 聚合', link: '/design/architecture/tactical/cognition/02-memory' },
          ],
        },
        {
          text: 'BC5 Observability',
          collapsed: true,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/observability/00-overview' },
          ],
        },
        {
          text: 'BC6 Conversation',
          collapsed: true,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/conversation/00-overview' },
            { text: 'Conversation 聚合 + Message', link: '/design/architecture/tactical/conversation/01-conversation' },
            { text: 'Identity 聚合 + ChannelBinding', link: '/design/architecture/tactical/conversation/02-identity' },
          ],
        },
        {
          text: 'BC7 Bridge',
          collapsed: true,
          items: [
            { text: 'Overview', link: '/design/architecture/tactical/bridge/00-overview' },
            { text: 'FeishuBridge 实现', link: '/design/architecture/tactical/bridge/01-feishu-integration' },
          ],
        },
        {
          text: 'agent-harness（跨 BC 协作）',
          collapsed: true,
          items: [
            { text: 'Prompt 组装', link: '/design/architecture/tactical/agent-harness/01-prompt-assembly' },
            { text: 'Skill CLI tooling', link: '/design/architecture/tactical/agent-harness/02-skill-cli-tooling' },
          ],
        },
        {
          text: 'presentation（Web Console）',
          collapsed: true,
          items: [
            { text: 'Web Console', link: '/design/architecture/tactical/presentation/01-web-console' },
          ],
        },
      ],
      '/design/decisions/': [
        {
          text: 'ADR 索引',
          items: [
            { text: 'ADR 索引 README', link: '/design/decisions/' },
          ],
        },
        {
          text: 'ADR 列表（按时间序）',
          collapsed: false,
          items: [
            { text: '0001 不引入 MCP', link: '/design/decisions/0001-no-mcp' },
            { text: '0002 不用 LLM SDK', link: '/design/decisions/0002-no-llm-sdk-use-cli-agents' },
            { text: '0003 Supervisor 命名', link: '/design/decisions/0003-supervisor-not-brain' },
            { text: '0004 Issue 取代 Suggestion', link: '/design/decisions/0004-issue-not-suggestion' },
            { text: '0005 项目宪章留项目仓', link: '/design/decisions/0005-project-charter-stays-in-project-repo' },
            { text: '0006 BlobStore', link: '/design/decisions/0006-blob-store-for-large-content' },
            { text: '0007 Conversation 层', link: '/design/decisions/0007-conversation-as-unified-session' },
            { text: '0008 WorkerProjectMapping', link: '/design/decisions/0008-worker-project-mapping-via-discovery-proposal' },
            { text: '0009 Issue 解耦', link: '/design/decisions/0009-issue-conversation-decoupled-via-bridge' },
            { text: '0010 Task/TaskExecution 两层', link: '/design/decisions/0010-task-execution-two-layer-model' },
            { text: '0011 Dispatch 可靠性', link: '/design/decisions/0011-dispatch-reliability-protocol' },
            { text: '0012 Memory file-based', link: '/design/decisions/0012-memory-file-based' },
            { text: '0013 Invocation 并发', link: '/design/decisions/0013-supervisor-invocation-concurrency' },
            { text: '0014 事件溯源 L1', link: '/design/decisions/0014-event-sourcing-level' },
            { text: '0015 agent_trace 不进 events', link: '/design/decisions/0015-agent-trace-not-in-events-table' },
            { text: '0016 (Superseded) bound thread', link: '/design/decisions/0016-task-progress-via-bound-thread' },
            { text: '0017 Task ↔ Conversation 1:1', link: '/design/decisions/0017-task-as-conversation' },
            { text: '0018 Detached agent + shim', link: '/design/decisions/0018-detached-agent-via-per-execution-shim' },
            { text: '0019 BC1+BC4 合并', link: '/design/decisions/0019-bc-scheduling-execution-merged-to-task-runtime' },
            { text: '0020 (Superseded) Card → Bridge', link: '/design/decisions/0020-card-confined-to-bridge-bc' },
            { text: '0021 Issue ↔ Conversation 1:1', link: '/design/decisions/0021-issue-as-conversation' },
          ],
        },
      ],
      '/design/requirements/': [
        {
          text: '需求层',
          items: [
            { text: 'Overview', link: '/design/requirements/00-overview' },
            { text: '功能需求', link: '/design/requirements/01-functional' },
            { text: '非功能需求', link: '/design/requirements/02-non-functional' },
            { text: '出范围', link: '/design/requirements/03-out-of-scope' },
          ],
        },
      ],
      '/design/implementation/': [
        {
          text: '实现层',
          items: [
            { text: 'README', link: '/design/implementation/' },
            { text: 'BlobStore', link: '/design/implementation/01-blob-store' },
          ],
        },
      ],
      '/rules/': [
        {
          text: '项目规约',
          items: [
            { text: 'Conventions（核心）', link: '/rules/conventions' },
            { text: '文档管理', link: '/rules/documentation' },
            { text: '测试规约', link: '/rules/testing' },
          ],
        },
      ],
    },

    outline: {
      level: [2, 4],
      label: '本页目录',
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/oopslink/agent-center' },
    ],

    docFooter: {
      prev: '上一页',
      next: '下一页',
    },

    lastUpdatedText: '最后更新',

    search: {
      provider: 'local',
      options: {
        locales: {
          'zh-CN': {
            translations: {
              button: { buttonText: '搜索', buttonAriaLabel: '搜索文档' },
              modal: {
                noResultsText: '无匹配结果',
                resetButtonTitle: '清空查询',
                footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' },
              },
            },
          },
        },
      },
    },
  },

  // 多数 dead link 来自：
  // (1) ADR 历史引用旧路径（conventions § 5 不动旧 ADR）
  // (2) drafts/ 内部 checkpoint 文档引用
  // (3) implementation 层 TBD 文件（02-persistence-schema / 03-cli-subcommands / 04-configuration / 等）
  // 全忽略；待 implementation 层落地后再分级处理
  ignoreDeadLinks: true,

  mermaid: {
    theme: 'default',
  },
}))
