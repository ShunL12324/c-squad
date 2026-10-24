# csquad

[English](https://github.com/ShunL12324/c-squad/blob/main/README.md) | 简体中文

<p>
  <a href="https://www.npmjs.com/package/csquad"><img src="https://img.shields.io/npm/v/csquad?color=78e8af&label=npm" alt="npm version"></a>
  <a href="https://github.com/ShunL12324/c-squad/blob/main/LICENSE"><img src="https://img.shields.io/npm/l/csquad?color=78e8af" alt="MIT license"></a>
</p>

在 [Claude Code](https://claude.com/claude-code) 或 Codex 里输入 `/create-task`，再用一句话描述要做的事。这件事会作为后台任务 T1、T2……运行，每个任务有自己的 git worktree，支持依赖、检查和自动合并。状态栏实时显示每个任务的进度。

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-80-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-80-light.svg" alt="状态栏显示 4 个任务：T9 等你处理（合并冲突），T4 已完成，T1 运行中，T7 第 2/3 轮修复中。">
</picture>

- **并行工作**：每个任务有独立的会话、worktree 和分支，你的对话不受打扰。
- **实时状态栏**：每个任务一行，显示状态、token 用量和耗时。
- **任务依赖**：说“after T1”，任务会等 T1 合并后再开始。
- **检查与合并**：由 Codex、Claude 或测试命令检查结果；你要求时，csquad 自动合并。
- **安全清理**：`/finish-task` 只在工作已合并时才删除任务，绝不删除未合并的代码。

## 安装

```sh
npm i -g csquad
csquad install
```

也可以直接运行 `npx csquad@latest install`。安装程序会加入五个 skill，并把任务行加在你原有状态栏的下方。装好后，在 git 仓库里启动（或重启）`claude`。

环境要求：Claude Code 2.1.260 及以上、git、Node.js（仅 npm 需要）。Codex 可选。支持 Linux、macOS 和 Windows。

## 使用

| 输入 | 效果 |
| --- | --- |
| `/create-task 给 HTTP 客户端加重试` | T1 在后台开始运行。 |
| `/create-task after T1，在文档里写清重试选项` | T2 等 T1 完成后再开始。 |
| `/create-task with codex: 加请求日志` | T3 作为 Codex 线程运行。 |
| `/create-task 加一个熔断器，让 codex 审查，通过后合并` | Codex 审查 T4，T4 修复问题，csquad 合并。 |
| `/task-status` | Claude 汇报每个任务的进度。 |
| `/message-task T2 改用 log/slog` | 给 T2 追加一条指令。 |
| `/finish-task T1` | Claude 合并 T1，然后删除它的 worktree、分支和会话。 |

不用斜杠命令也行，直接说“建个任务给 list 加一个 --done 参数”。输入 `/watch-task T1`，T1 完成或需要你回复时会通知你。

![动画：用户输入三次 /create-task。T1 开始运行，T2 等待 T1，T3 在 Codex 上运行。各行显示状态和 token 用量。T1 完成后，用户输入 /finish-task T1，Claude 合并 T1，T1 这一行消失，T2 开始运行。](https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/demo.gif)

<details>
<summary><b>看懂状态栏</b></summary>

每行依次是：状态图标、任务 ID、引擎（`✻` Claude Code，`⬡` Codex）、标题、状态、token 用量和处于该状态的时长。最紧急的任务排在最前。

| 图标 | 状态 | 怎么办 |
| --- | --- | --- |
| `◆` | `needs input` | 任务在提问。在 agent view 里回答，或告诉 Claude。 |
| `◆` | `needs you · 原因` | csquad 暂停了任务。看原因，然后运行 `csquad retry T7`。 |
| `✗` | `failed` | 会话失败。用 `csquad peek T7` 查看。 |
| `✓` | `done`、`ready` | 已完成，或已通过检查。可以收尾。 |
| `⇡` `◑` `◐` `●` | `merging`、`fixing 2/3`、`checking 1/3`、`working` | 等待即可。 |
| `◇` | `idle` | 任务在等待。发消息或收尾。 |
| `◌` | `waiting T5` | T5 完成后开始。 |
| `■` | `stopped` | 发一条消息让它继续，或取消它。 |
| `□` | `gone` | 会话在 csquad 之外被删除。运行 `csquad cancel T7`。 |

`326k +4.1k` 表示总 token 数和上次刷新后新增的 token 数。琥珀色的 `idle 3m` 表示 3 分钟没有活动。状态栏只显示当前对话创建的任务；`/clear` 之后，运行 `csquad status --all` 查看全部任务。

80 列，一个任务：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-1-task-80-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-1-task-80-light.svg" alt="状态栏只有一个任务：T4 已完成。">
</picture>

160 列，九个任务分两栏，最后一行列出其余任务：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-160-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-160-light.svg" alt="状态栏分两栏显示 9 个任务，最后一行显示另外 3 个。">
</picture>

124 列，标题被截短：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-124-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-124-light.svg" alt="状态栏分两栏显示 9 个任务，标题截短到 16 格。">
</picture>

56 列，较长的提示标签，时长仍对齐：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-long-badge-56-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-long-badge-56-light.svg" alt="56 列的状态栏显示 3 个任务，标题和 needs you 的原因被截短。">
</picture>

</details>

<details>
<summary><b>命令参考</b></summary>

这些命令由 Claude 替你运行，你也可以在终端里直接运行：程序位于 `~/.csquad/bin/csquad`。`csquad COMMAND --help` 查看用法。大多数命令接受 `--all`，用来操作其他对话的任务。

| 命令 | 说明 |
| --- | --- |
| `launch --title T [flags] [-- ENGINE_ARGS]` | 启动任务，提示词从 stdin 读取。 |
| `status [--all] [--json]` | 列出任务：状态、时长、会话、最新消息。 |
| `peek T12 [-n 6]` | 查看任务最近的消息。 |
| `wait T12 [T13...] [--timeout MIN]` | 任务需要输入、完成、失败或被删除时返回。 |
| `send T12 "text"` | 追加一条指令。 |
| `finish T12 [T13...] [--into BRANCH]` | 确认工作已合并且无未提交改动，然后删除 worktree、分支和会话。 |
| `cancel T12 [--discard]` | 停止任务并移出列表。未合并的工作会保留，除非加 `--discard`。 |
| `retry T12` | 从暂停处继续一个 `needs you` 的任务。 |
| `install`、`update`、`uninstall`、`status-install` | 管理安装。 |

`launch` 的参数：

| 参数 | 说明 |
| --- | --- |
| `--after T3,T4` | 这些任务完成后再开始。 |
| `--check SPEC` | 任务停下时检查结果：`codex[:MODEL]`、`claude[:MODEL]` 或 `cmd:COMMAND`。 |
| `--max N` | 最多检查几轮，默认 3。 |
| `--merge [BRANCH]` | 检查通过后合并，默认合并到主 checkout 当前的分支。 |
| `--engine claude\|codex`、`--model M` | 引擎和模型，默认 `claude`。 |
| `--cwd DIR` | 在 DIR 中运行任务。 |
| `--isolation none` | 直接改当前 checkout：不建 worktree、不建分支、不提交。 |
| `--fork` | 从当前对话 fork 出任务。 |
| `--prompt-file F [--rm-prompt]` | 从文件 F 读取提示词。 |
| `-- ENGINE_ARGS` | 传给 `claude --bg` 的参数，或 Codex 选项，如 `--effort high`。 |

</details>

<details>
<summary><b>配置</b></summary>

**`~/.csquad/models.md`** 告诉 Claude 哪类工作用哪个引擎、模型和参数。Claude 每次启动任务前都会读它，csquad 从不写入。

```markdown
- 小修复、文档、测试：claude，`-- --model haiku`
- 新功能和重构：claude，`-- --model opus --effort high`
- 代码审查：`--check codex`

## When to use create-task

我说“丢到后台”时也用它。
```

`## When to use create-task` 一节里的说法会加入 skill 的触发词。修改后运行 `csquad update`。

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `CSQUAD_HOME` | `~/.csquad` | 程序、任务数据库、`models.md` 和日志。 |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Claude Code 配置，skill 装在其中的 `skills` 目录。 |
| `CODEX_HOME` | `~/.codex` | Codex 配置。目录存在时，`install` 也会配置 Codex。 |
| `NO_COLOR` | 未设置 | 去掉状态栏的颜色。 |

任务使用与创建它的会话相同的模型服务商：csquad 会把服务商相关的变量（`ANTHROPIC_*`、`CLAUDE_CODE_USE_*` 等）复制到任务的设置中。使用 Codex 时，需在 Codex 里用 `/hooks` 批准一次 csquad 的 Stop hook。

</details>

<details>
<summary><b>更新与卸载</b></summary>

```sh
npx csquad@latest update
~/.csquad/bin/csquad uninstall
```

`uninstall` 会恢复你原来的状态栏，删除 skill、程序和 Codex hook，但保留 `~/.csquad/csquad.db` 和 `~/.csquad/models.md`。正在运行的任务不受影响。

**从 csquad 1.x 升级**：先运行 `npx csquad@1 uninstall`，再运行 `npx csquad@latest install`。

</details>

<details>
<summary><b>常见问题</b></summary>

**状态栏没有任务行。**
运行 `~/.csquad/bin/csquad status-install`。如果项目的 `.claude/settings.json` 自己设置了 `statusLine`，它会在该项目中替换 csquad 的状态栏。

**启动失败，提示工作区不受信任。**
在该目录里打开一次 `claude`，接受信任提示。

**任务报 “Login expired”，或用错了服务商。**
从带有服务商环境变量的 shell 启动 `claude`，再重新创建任务。

**发了消息，任务没有反应。**
Claude Code 可能在等你批准这条消息。打开 agent view（`claude agents`），选中任务，批准消息。

**`csquad finish` 拒绝删除任务。**
工作还没合并，或 worktree 里有未提交的改动。合并或提交后再试。`csquad cancel T7` 保留工作；`csquad cancel T7 --discard` 删除工作。

**任务会自己合并吗？**
只有你要求时才会（“通过后合并”）。否则由 `/finish-task` 合并。

**最多能同时跑几个任务？**
csquad 不设上限。每个任务都消耗自己的 token。

**`npm i -g csquad` 没有装上程序。**
程序是可选依赖（optional dependency）。去掉 `--omit=optional` 重新安装。

</details>

## 参与贡献

构建、测试和发布 csquad，请看 [CONTRIBUTING.md](https://github.com/ShunL12324/c-squad/blob/main/CONTRIBUTING.md)。采用 MIT 许可证，见 [LICENSE](https://github.com/ShunL12324/c-squad/blob/main/LICENSE)。
