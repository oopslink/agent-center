# DDD 设计图绘制技能手册

> 可复用于任意领域的 DDD 可视化设计规范。基于**纯 HTML + 内联 CSS** 实现，**无外部依赖**，可直接作为静态页面部署到 GitHub Pages。
>
> 本规则约束 `sites/` 下所有版本设计展示页（`sites/designs/<version>/`）的绘制方式，确保各版本设计图风格统一、可对照演进。

---

## 一、整体结构

用三个标签页（Tab）覆盖 DDD 的三个层次：

| Tab | 内容 | 对应 DDD 层次 |
|-----|------|-------------|
| ① 限界上下文地图 | 上下文划分、上下游关系 | 战略设计 |
| ② 战术设计 | 聚合/实体/值对象/领域服务/Repository | 战术设计 |
| ③ Event Storming | Actor→Command→Aggregate→Event→Policy 流程 | 流程建模 |

---

## 二、战略设计 — 限界上下文地图

### 上下文分类与配色

```
核心域  border: #e03131  background: #fff5f5  标题色: #c92a2a
支撑域  border: #1971c2  background: #e7f5ff  标题色: #1864ab
通用域  border: #868e96  background: #f8f9fa  标题色: #495057
```

### 集成关系标注

| 标签 | 含义 |
|------|------|
| `OHS` | 开放主机服务，提供标准 API 供他人调用 |
| `ACL` | 防腐层，隔离外部模型防止污染本地领域 |
| `U/D` | 上游 / 下游，标注信息流方向 |
| `CF`  | 遵从者，完全跟随上游模型 |

### 布局技巧

- 核心域放中间，支撑域围绕，通用域（外部服务）放边缘
- 用 `grid3`（三列）排列上下文卡片
- 上下文之间用带标签的箭头行（`arrows-row`）表达集成关系
- 核心域卡片用 `grid-column: span 2` 加宽，突出重要性

---

## 三、战术设计 — 聚合内部结构

### 六类构件及样式规范

#### 1. 聚合根（Aggregate Root）
```css
border: 2px solid #f76707;  background: #fff4e6;  border-radius: 12px;
徽章: background #f76707, color white, 文字 "AR"
```
- 聚合根名称加粗显示，字段列出 ID + 核心状态字段

#### 2. 实体（Entity）
```css
background: #e7f5ff;  border: 1px solid #74c0fc;  border-radius: 6px;
名称色: #1c4a7a;  字段色: #555;  根标记: color #f76707, 文字 "[根]"
```
- 聚合根是特殊实体，加 `[根]` 标记
- 子实体只写关键字段（ID + 业务属性）

#### 3. 值对象（Value Object）
```css
background: #f3f0ff;  border: 1px dashed #9775fa;  border-radius: 6px;
名称色: #4a1a8c;
```
- 用**虚线边框**与实体区分（关键视觉差异）
- 注明不可变特性和值相等性语义

#### 4. 领域服务（Domain Service）
```css
background: #ebfbee;  border: 1px solid #69db7c;  border-radius: 6px;
名称色: #1b4d25;  字段色: #444;  备注色: #888, font-style italic
```
每个领域服务必须写清楚：
1. **职责说明** — 为何这个逻辑不能放进聚合（跨聚合 / 跨上下文）
2. **方法签名** — `+ methodName(params)`
3. **步骤说明** — 每个方法的执行步骤 ①②③
4. **依赖注释** — 协调了哪些聚合或上下文

#### 5. Repository
```css
background: #fff3bf;  border: 1px solid #ffd43b;  border-radius: 6px;
名称色: #7c5c00;
```
- 每个聚合根对应一个 Repository
- 列出标准方法：`findById` / `findByXxx` / `save` / `delete`

#### 6. 领域事件（Domain Event）
```css
background: #f76707;  color: white;  border-radius: 4px;  padding: 2px 8px;
display: inline-block;
```
- 使用过去式命名：`OrderPlaced` / `PaymentCompleted`
- 放在聚合卡片底部，集中展示

### 聚合卡片内部三列布局

```
[ 实体列 ]  |  [ 值对象列 ]  |  [ Repository + 领域事件列 ]
```

---

## 四、Event Storming — 流程泳道

### 颜色约定（遵循社区标准）

| 构件 | 颜色 | CSS |
|------|------|-----|
| Actor 触发者 | 黄色 | `background: #ffe066` |
| Command 命令 | 蓝色 | `background: #4dabf7; color: white` |
| Aggregate 聚合 | 金色 | `background: #f9c74f` |
| Event 领域事件 | 橙色 | `background: #f76707; color: white` |
| Policy 策略 | 紫色 | `background: #cc5de8; color: white` |

### 流程格式

```
Actor → Command → Aggregate → Event → Policy → Command → Aggregate → Event
```

- 每条业务流程单独一行（`es-flow` 卡片）
- 流程标题用 emoji 区分（🛒下单 / 💳支付 / 🚚发货 / ↩️退款）
- 箭头用 `→` 字符，`color: #aaa`

---

## 五、通用布局工具类

```css
.grid3  { display: grid; grid-template-columns: repeat(3,1fr); gap: 16px; }
.grid2  { display: grid; grid-template-columns: repeat(2,1fr); gap: 16px; }
.flex-row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.flex-col { display: flex; flex-direction: column; gap: 8px; }
.max-w  { max-width: 900px; margin: 0 auto; }
```

---

## 六、Tab 切换逻辑

```html
<!-- 按钮 -->
<button class="tab-btn active" onclick="showTab('strategy', this)">① 限界上下文地图</button>

<!-- 内容区（默认 display:none，首个显示） -->
<div id="tab-strategy">...</div>
<div id="tab-tactical" style="display:none">...</div>
<div id="tab-eventstorming" style="display:none">...</div>

<!-- JS -->
<script>
  function showTab(name, btn) {
    ['strategy','tactical','eventstorming'].forEach(t => {
      document.getElementById('tab-' + t).style.display = 'none';
    });
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    document.getElementById('tab-' + name).style.display = 'block';
    btn.classList.add('active');
  }
</script>
```

---

## 七、新项目使用步骤

1. **识别上下文** → 填写限界上下文地图，标注核心域 / 支撑域 / 通用域
2. **选定核心上下文** → 进入战术设计，拆解聚合根、实体、值对象
3. **标记领域服务** → 凡是跨聚合或跨上下文的业务逻辑，提取为领域服务并注明职责
4. **为每个聚合根配 Repository** → 列出常用查询方法
5. **整理领域事件** → 过去式命名，挂在对应聚合底部
6. **绘制 Event Storming** → 按业务流程逐条梳理 Actor→Command→Aggregate→Event→Policy 链路

---

## 八、关键设计原则（决策依据）

| 问题 | 原则 |
|------|------|
| 逻辑放聚合还是领域服务？ | 逻辑只涉及单聚合内部 → 放聚合方法；需跨聚合或跨上下文协调 → 领域服务 |
| 用实体还是值对象？ | 有唯一标识、生命周期 → 实体；无标识、不可变、值相等 → 值对象 |
| 上下文之间怎么集成？ | 己方被动接收 → ACL；己方主动开放 → OHS；完全跟随 → CF |
| Repository 放哪层？ | 接口定义在领域层，实现在基础设施层 |

---

## 九、本仓约定（落地）

- **目录**：每个版本一页，放 `sites/designs/<version>/index.html`（如 `sites/designs/v2.7/index.html`）。`sites/index.html` 作版本索引。
- **自包含**：单文件、`<style>` 内联、无外部 CSS/JS/字体/图片依赖；可双击直接在浏览器打开，也可被 GitHub Pages 直接托管。
- **真实性**：图中的限界上下文、聚合、状态机、领域事件须与该版本**真实代码/设计**一致（不沿用已退役的旧模型）。新版本另起一页，旧版本页保留以便对照演进。
- **统一语言**：构件名用项目 DDD 统一语言（中文名 + 英文名并列）。
