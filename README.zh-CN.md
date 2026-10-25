![csquad：给 Claude Code 和 Codex 用的一组后台任务。你的会话派出 T1 到 T4 四个任务：一个已完成，一个等你回复，两个正在运行。](https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/assets/banner.png)

[English](https://github.com/ShunL12324/c-squad/blob/main/README.md) | 简体中文

<p>
  <a href="https://www.npmjs.com/package/csquad"><img src="https://img.shields.io/npm/v/csquad?color=78e8af&label=npm" alt="npm version"></a>
  <a href="https://github.com/ShunL12324/c-squad/blob/main/LICENSE"><img src="https://img.shields.io/npm/l/csquad?color=78e8af" alt="MIT license"></a>
</p>

csquad 让你在 [Claude Code](https://claude.com/claude-code) 或 [Codex](https://github.com/openai/codex) 的对话里，把编码任务交给后台去做。任务以 Claude Code 后台会话或 Codex 线程的形式运行。每个任务有一个名字（`T1`、`T2`……）、自己的 git worktree，以及 Claude Code 状态栏里的一行。你继续你的对话，任务并行工作。

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-80-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-80-light.svg" alt="80 列的状态栏，4 个任务排成一栏。第 1 行：T9，Install migration from 1.x，needs you，merge conflict，1h35m。第 2 行：T4，readme demo recording，done，290k token，3m。第 3 行：T1，Codex app-server reconnect，working，326k token，+4.1k，7m。第 4 行：T7，Windows PATH resolution，fixing 2/3，210k token，idle 3m，31m。">
</picture>

![动画：用户在 Claude Code 里输入三次 /create-task。T1 开始运行。T2 排在 T1 之后，显示 “waiting T1”。T3 在 Codex 上运行，带 ⬡ 标记。各行显示 “working” 和已用的 token。T1 变为 “done”。用户输入 /finish-task T1，Claude 合并 T1，T1 这一行消失，T2 开始运行。](https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/demo.gif)

csquad 是一个二进制文件，没有自己的后台服务。它安装五个 skill，你要建任务时 Claude Code 和 Codex 会用到它们；它还把任务行加到你自己的状态栏里。

- [快速开始](#快速开始)
- [示例](#示例)
- [看懂状态栏](#看懂状态栏)
- [通知](#通知)
- [命令参考](#命令参考)
- [配置](#配置)
- [更新和卸载](#更新和卸载)
- [故障排查和常见问题](#故障排查和常见问题)

## 快速开始

### 环境要求

- 支持后台会话的 Claude Code（`claude --bg`、`claude agents`），版本 2.1.260 或更新。版本更旧时，`csquad finish` 可能留下已结束任务的会话。
- Codex：只有用 Codex 任务或 Codex 审查时才需要。csquad 用 codex-cli 0.163.0 测试过。没有 Codex 也能用。
- Node.js：只用于 `npx` 和 `npm`，csquad 本身不用 Node.js。
- git：每个任务在你仓库的一个 git worktree 里工作。
- Linux、macOS 或 Windows，amd64 或 arm64。

### 安装

1. 全局安装 npm 包，然后运行安装程序：

   ```sh
   npm i -g csquad
   csquad install
   ```

   或者用 `npx` 直接运行一次安装程序：

   ```sh
   npx csquad@latest install
   ```

2. 检查安装结果：

   ```sh
   ~/.csquad/bin/csquad status-install
   ```

3. 在 git 仓库里启动 `claude`；如果已经在运行，就重启它。
4. 输入 `/create-task`，再用一句话说要做的事，例如：`/create-task 给 list 加一个 --done 参数`。

安装程序把二进制文件复制到 `~/.csquad/bin/csquad`，skill 和状态栏都用这份副本。安装程序不改你的 `PATH`。不经过 npm 也想在 shell 里直接输入 `csquad`，就把 `~/.csquad/bin` 加进 `PATH`。

安装程序还会做这些事：

- 把 skill 装到 `~/.claude/skills`。
- 包装你的状态栏：你原来的状态栏保持在最上面，任务行显示在它下面。
- 如果 `~/.codex` 存在，也把 skill 装到 `~/.agents/skills`，供 Codex 使用。

从源码构建，见 [CONTRIBUTING.md](https://github.com/ShunL12324/c-squad/blob/main/CONTRIBUTING.md)。

## 示例

每个示例给出你在 Claude Code 里输入的内容、Claude 的回复，以及 Claude 运行的 csquad 命令。你先输入斜杠命令，再用一句话描述工作。“after T1”“with codex” 这类选项也写在同一句话里。不用斜杠命令也行，例如“建个任务给 HTTP 客户端加重试”。这些命令你也可以自己在 shell 里运行。

命令从 stdin 读取任务提示。在 PowerShell 里，把 here-string（`@' … '@`）用管道传给 `csquad launch`。

### 创建任务

```text
you     /create-task 给 HTTP 客户端加重试
claude  已启动 T1 · add http client retry，它在后台工作。
```

这个任务是一个名为 `T1 · add http client retry` 的 Claude Code 后台会话。它自己创建 worktree 和分支，完成工作、提交，然后停下。它不会主动汇报，但它的那一行会出现在你的状态栏里。

```sh
csquad launch --title "add http client retry" <<'PROMPT'
Add retry with exponential backoff to the Client in client.go.
Retry GET requests on network errors, 429 and 5xx. Add tests and run go test ./...
PROMPT
```

```text
launched T1 · add http client retry · session 93da1028 · ~/demo/httpclient
```

### 关注任务

状态栏始终显示任务。只有想在任务变化时收到消息，才需要 `/watch-task`。

```text
you     /watch-task T1
claude  正在关注 T1。它完成或需要你回复时我会告诉你。
        ...
claude  T1 完成了：带退避的重试在分支 worktree-t1-http-client-retry 上，go test 通过。
        要我合并并收尾吗？
```

Claude 在后台运行 `csquad wait`。这个命令每 10 秒检查一次任务，不消耗 token。任务需要输入或需要你处理、变为 done、ready、idle 或 stopped、失败或被删除时，它就返回；最长等 4 小时。

```sh
csquad wait T1                # 每个有变化的任务输出一行，例如 "T1 · add http client retry · done"
csquad status                 # 本会话的任务：状态、时长、会话、最新消息
csquad peek T1 -n 8           # 任务的最后 8 条消息，工具调用折叠显示
```

随时想看汇报，输入 `/task-status`。

### 合并并结束任务

```text
you     /finish-task T1
claude  已把 worktree-t1-http-client-retry 合并到 main，go test 通过。
        T1 已结束：它的 worktree、分支和会话都已删除。
```

Claude 把任务的分支合并到你主 checkout 所在的分支，然后运行 `csquad finish`。只有确认工作安全时，这个命令才会删除任务：

1. 确认任务的每个分支都已合并到目标分支。如果目标分支对该分支的每个提交都有相同的改动（相同的 patch），例如 cherry-pick 或 rebase 之后，也算已合并。
2. 确认 worktree 没有未提交的改动。
3. 删除 worktree、已合并的分支和会话。

只要有一项检查不通过，`csquad finish` 就停下，什么也不删。它绝不删除未合并的工作。它会列出不在目标分支里的提交、未提交的文件，以及两种后续做法：

```text
csquad: T2 is not finished, nothing was removed:
  - branch worktree-t2-layout is not merged into main:
      3f2a91c try a grid layout
      8be0d14 add debug screenshots
      2 commits already in main by patch (cherry-picked)
  - worktree ~/demo/app/.claude/worktrees/t2-layout has uncommitted changes: only untracked files: .pristine/ (14 files)
  keep the work and stop tracking it: csquad cancel T2
  delete it:                          csquad cancel T2 --discard
```

- 只想保留部分提交：把它们 cherry-pick 到目标分支，再运行 `csquad finish T2`。如果剩下的是你不要的提交，运行 `csquad cancel T2 --discard`。
- `csquad cancel T2` 把任务从列表中移除，worktree 和分支留给你。
- `csquad cancel T2 --discard` 删除任务的 worktree、分支和会话，包括未合并的工作。

不要手动删除任务的分支、worktree 或会话，请用这些命令。

```sh
git merge worktree-t1-http-client-retry
csquad finish T1
```

```text
finished T1 · add http client retry · merged into main: worktree-t1-http-client-retry · removed worktree ~/demo/httpclient/.claude/worktrees/t1-http-client-retry, branch worktree-t1-http-client-retry, removed session 93da1028
```

输入 `/finish-task 所有已完成的任务`，可以结束本会话中所有 done、idle 或 ready 的任务。

### 并行运行两个任务

```text
you     /create-task 加一个请求超时选项
claude  已启动 T2 · add request timeout option。
you     /create-task 修复重定向时 Authorization 头泄露
claude  已启动 T3 · fix auth header on redirect。
```

每个任务有自己的会话、worktree 和分支，所以不会在磁盘上改同一份文件。改同一处代码的工作，Claude 会放进同一个任务。

```sh
csquad launch --title "add request timeout option" <<'PROMPT'
Add a Timeout option to the Client in client.go. Add a test and run go test ./...
PROMPT
csquad launch --title "fix auth header on redirect" <<'PROMPT'
Drop the Authorization header when a redirect goes to another host. Add a test.
PROMPT
```

### 在另一个任务之后开始

```text
you     /create-task after T1，在文档里写清重试选项
claude  已排队 T4 · document retry options，T1 完成后开始。
```

状态栏显示 `waiting T1`。T1 合并并结束后，T4 才开始，它的提示里会带上已合并的分支和 T1 的最后一条消息。T1 只是 `ready` 时，T4 不会开始。

```sh
csquad launch --title "document retry options" --after T1 <<'PROMPT'
Document the retry options in README.md: what is retried, the defaults, and an example.
PROMPT
```

```text
queued T4 · document retry options · after T1 · ~/demo/httpclient
```

### 让 Codex 审查任务，然后合并

```text
you     /create-task 加一个熔断器，让 codex 审查，最多 3 轮，然后合并
claude  已启动 T5 · add circuit breaker。它停下后由 Codex 检查，最多 3 轮，
        检查通过后 csquad 把它合并到 main。
```

完整流程如下：

1. 任务工作，然后停下。
2. csquad 启动一个新的只读 Codex 审查者。状态栏显示 `checking 1/3`。
3. 审查不通过时，csquad 把发现的问题发给任务。状态栏显示 `fixing 2/3`。
4. 审查通过时，csquad 在任务的 worktree 里把分支 rebase 到 `main`。状态栏显示 `merging`。rebase 有冲突时，由任务来解决。
5. csquad 快进（fast-forward）`main`，确认合并完整，然后删除任务。

如果最后一轮仍不通过，或冲突始终没解决，任务会停在 `needs you · 原因`。

```sh
csquad launch --title "add circuit breaker" --check codex --max 3 --merge <<'PROMPT'
Add a circuit breaker to the Client: stop retries after N consecutive failures
for a cool-down period. Add tests and run go test ./...
PROMPT
```

```text
launched T5 · add circuit breaker · session 2876bd02 · ~/demo/httpclient
  then: check codex, at most 3 rounds · merge into main
```

其他检查方式：

- `--check claude` 或 `--check claude:sonnet`：Claude Code 审查者，可指定模型。
- `--check codex:MODEL`：指定模型的 Codex 审查者。
- `--check 'cmd:go test ./...'`：在任务的 worktree 里运行一条命令，退出码为 0 即通过。
- 不加 `--merge` 时，通过检查的任务显示 `ready`。按[合并并结束任务](#合并并结束任务)的方法合并它。

### 在 Codex 里运行任务

```text
you     /create-task with codex: 用 log/slog 加请求日志
claude  已启动 T6 · add request logging（Codex）。用 codex resume <thread> 打开它。
```

这个任务是一个名为 `T6 · add request logging` 的 Codex 线程，运行在 Codex 的 app-server 上；app-server 没运行时，csquad 会启动它。线程在分支 `task/t6-slug` 上创建 worktree `<repo>-wt/t6-slug`。状态栏用 `⬡` 标记 Codex 任务，用 `✻` 标记 Claude Code 任务。

```sh
csquad launch --title "add request logging" --engine codex -- --effort high <<'PROMPT'
Add structured request logging to the Client with log/slog. Add tests.
PROMPT
```

`--` 之后，Codex 任务接受 `-m/--model`、`--effort`、`-s/--sandbox`、`-a/--ask-for-approval` 和 `-c/--config KEY=VALUE`。默认值是 `danger-full-access` 和 `never`。`status`、`peek`、`send`、`wait`、`finish`、`cancel` 和 `retry` 对 Codex 任务和 Claude Code 任务用法相同。

### 给任务发消息

```text
you     /message-task T2 改用 log/slog
claude  已把改用 log/slog 的指令发给 T2。
```

csquad 把消息投递到任务的会话。已停下的会话会带着这条消息重新开始。Codex 任务在当前轮次或新的一轮里收到消息。

```sh
csquad send T2 "Use log/slog for the request logging, not a custom Logger interface."
```

```text
sent to T2 · structured request logging: delivered to the inbox of session 9efe5bda (no receipt)
```

csquad 在每个任务的设置里写入 `"crossSessionInbound": "accept"`，所以 Claude Code 会立即投递消息。如果你用 `-- --settings` 设了别的值，Claude Code 可能会扣住消息，等你在 agent view 里批准。

### 更多说法

| 在 `/create-task` 后加上 | Claude 加的参数 | 效果 |
| --- | --- | --- |
| “用 opus，high effort” | `-- --model opus --effort high` | `--` 之后的参数原样传给 `claude --bg`。 |
| “就地改，我想实时看” | `--isolation none` | 任务直接改你的 checkout，不建分支，也不提交。 |
| “fork 这个对话” | `--fork` | 任务从当前对话 fork 出来。 |
| “在 ~/work/api 里” | `--cwd ~/work/api` | 任务在那个目录里运行。 |

## 看懂状态栏

本页顶部的状态栏是 80 列下的 4 个任务。每个任务占一行，从左到右依次是：

1. 彩色竖条和表示状态的图标。
2. 任务 ID 和引擎标记：`✻` 表示 Claude Code，`⬡` 表示 Codex。
3. 标题。比本栏最长标题短 3 格或以上的标题，后面会用点线连到状态。
4. 状态。
5. 用量：总量加新增 token、只有总量，或不显示。
6. 进入当前状态后的时长。

最紧急的任务排在最前。状态、用量和时长各自成列，每列和它最宽的值一样宽。

<details>
<summary>更多宽度</summary>

80 列，一个任务：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-1-task-80-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-1-task-80-light.svg" alt="80 列的状态栏，只有一个任务。第 1 行：T4，readme demo recording，done，290k token，3m。">
</picture>

160 列，九个任务。三栏、每栏最多 4 行，9 个任务全部显示。标题截到 16 格，徽标里的原因也被截短：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-160-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-160-light.svg" alt="160 列的状态栏，9 个任务分三栏。第一栏：T2，修复 Windows 下（截断），needs input，18m；T9，Install migrati（截断），needs you，mer（截断），1h35m；T10，Peek transcript（截断），failed，61k token，2h10m；T4，readme demo rec（截断），done，290k token，3m。第二栏：T1，Codex app-serve（截断），working，326k token，+4.1k，7m；T6，Retry SQLITE_BU（截断），checking 1/3，88k token，+0，12m；T7，Windows PATH re（截断），fixing 2/3，210k token，idle 3m，31m；T8，Status line chi（截断），merging，token 数未知，+?，3m。第三栏：T5，Document hooks（截断），waiting T1，7m。">
</picture>

124 列，九个任务。放不下三栏，于是两栏显示 6 个任务。标题截到 16 格，徽标里的原因也被截短。最后一行列出其余 3 个任务：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-124-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-124-light.svg" alt="124 列的状态栏，9 个任务分两栏。左栏：T2，needs input，18m；T9，needs you，merge conflict（截断），1h35m；T10，failed，61k token，2h10m。右栏：T4，done，290k token，3m；T1，working，326k token，+4.1k，7m；T6，checking 1/3，88k token，+0，12m。第 4 行：+3 more，T7、T8 和 T5。标题被截短。">
</picture>

56 列，一个很长的徽标。标题截到 12 格，徽标里的原因被截短，时长仍留在它那一列：

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-long-badge-56-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-long-badge-56-light.svg" alt="56 列的状态栏，3 个任务。第 1 行：T9，Install migration（截断），needs you，merge conflict（截断），1h35m。第 2 行：T4，readme demo（截断），done，290k token，3m。第 3 行：T1，Codex app-server（截断），working，326k token，+4.1k，7m。">
</picture>

</details>

| 图标 | 状态 | 含义 | 怎么办 |
| --- | --- | --- | --- |
| `◆` | `needs input` | 任务在问你问题。 | 在 agent view 里回答，或把答案告诉 Claude。 |
| `◆` | `needs you · 原因` | csquad 暂停了任务的流程。 | 看原因，然后运行 `csquad retry T7`。 |
| `✗` | `failed` | 会话失败。 | 用 `csquad peek T7` 查看。 |
| `✓` | `done` | 任务做完了。 | 合并并结束它。 |
| `✓` | `ready` | 任务通过了检查。 | 合并并结束它。 |
| `⇡` | `merging` | csquad 正在合并任务的分支。 | 等待。 |
| `◑` | `fixing 2/3` | 检查没通过，任务在第 2 轮（共 3 轮）修复问题。 | 等待。 |
| `◐` | `checking 1/3` | 检查在运行，第 1 轮（共 3 轮）。 | 等待。 |
| `●` | `working` | 任务在工作。 | 等待。 |
| `◇` | `idle` | 任务结束了这一轮，在等待。 | 看看结果，然后发消息或结束它。 |
| `◌` | `waiting T5` | 任务在等 T5，还没有会话。 | 结束 T5。 |
| `■` | `stopped` | 会话没做完就停了。 | 发一条消息让它重新开始，或取消它。 |
| `□` | `gone` | 会话在 csquad 之外被删除，但它的部分工作还在。 | 运行 `csquad cancel T7`。 |

### “needs you” 徽标

`needs input` 和 `needs you` 显示为琥珀色徽标。`needs you` 后面是原因的开头，例如 `needs you · max rounds` 或 `needs you · merge conflict`。运行 `csquad status` 查看完整原因，运行 `csquad retry T7` 从任务停下的地方继续：

- `max rounds`、`not converging`：任务再修复一轮。
- `no verdict`：重新运行检查。
- `merge conflict`、`merge stopped`：重新合并。
- `dependency gone`：任务重新等待。
- `did not start`：重新启动任务。

其他原因 `retry` 无法继续。`message lost`：用 `csquad send T7` 重新发送消息。`session gone`：取消任务。

### 用量

处于 `working`、`checking`、`fixing` 或 `merging` 的任务，先显示总量，再显示距上次刷新（约 10 秒）新增的 token，例如 `326k +4.1k`。总量后面这部分至少占 7 格，变化时整行不会跳动：

| 总量后面 | 含义 |
| --- | --- |
| `+4.1k` | 上次刷新以来新增的 token。 |
| `+0` | 没有变化，淡色显示。 |
| `+?` | 会话有变化，但提供方没有报告 token 数，或只报告了一部分。 |
| `idle 3m` | 2 分钟或更久没有活动。时长用琥珀色显示，状态不变。`checking` 的任务不显示 `idle`：它在等检查结果。 |

其他任务只显示总量，淡色：`326k`、`1.2M`，总量未知时显示 `?`。总量包括输入、缓存写入和输出，不包括缓存读取。

`csquad status` 显示每个运行中任务的完整用量：

```text
## T365 · cancel smoke
state: working · started 27s ago · session ca47ac83 · ~/project
usage: 51.8k fresh · +0 since the last refresh (0 s) · last activity 13s ago
       input 4 · cache write 51.1k · output 622  cache read 50.5k (not counted)
       context 26% (51k of 200k) · status line +0 in 10 s
```

### 宽度和行数

- 状态栏先占满终端宽度：用放得下的最少行数（最多 4 行），栏数尽量多，最多每个任务一栏。例如在 280 列的终端里，两个任务排成一行两栏。行数相同时，用最少的栏数。
- 只有每个标题仍能保留 16 格或以上时，状态栏才会多加一栏。
- 4 行放不下所有任务时，就少用一栏，最后一行显示 `+N more`，并列出其余每个任务的图标和 ID。
- 各行按栏依次填充，所以第一栏是最紧急的任务。只有一行时，从左往右读。
- 每栏和它最长的一行一样宽。
- 每行最多比终端窄 4 格：Claude Code 会缩进状态栏，并截掉更长的行。
- `needs you` 徽标可能比状态和用量更宽，这时它会压过用量那一列，时长仍留在自己那一列。
- 终端较窄时，先缩短标题；然后用量先去掉新增 token，再去掉总量，`needs you` 徽标里的原因也会缩短。时长永远不截。
- `NO_COLOR` 去掉颜色，图标、粗体、淡色和反色保留。

### 显示哪些任务

状态栏只显示当前对话创建的任务。`/clear` 会开始一个新对话，之前的任务就从状态栏消失了，但它们仍在运行。要看全部任务，打开 agent view（`claude agents`）、运行 `csquad status --all`，或者让 Claude “显示所有任务”。

## 通知

任务需要你或者完成时，csquad 会弹出系统通知。你在会话里工作时也能知道。

| 事件 | 什么时候 |
| --- | --- |
| `needs input` | 任务提了问题、显示了权限请求，或有一条消息在等你批准。正文是它最后一条消息的第一行。 |
| `needs you` | 任务停下来等你。正文写明原因。 |
| `ready` | 检查通过了。 |
| `done` | 没有检查也没有合并的任务结束了这一轮。正文是它最后一条消息的第一行。 |
| `failed` | 会话失败了。 |
| `merged` | `--merge` 合并了工作。 |

标题是任务：`T2 · fix login redirect`。每次变化只通知一次，由最先看到它的 csquad 进程发出：状态栏、Stop hook 或某个命令。任务在工作时不发通知。

| 系统 | 显示方式 |
| --- | --- |
| Windows | 来自 “csquad” 的 toast，带图标。`install` 为当前用户注册应用 ID `csquad`（不需要管理员权限），并把图标写到 `%LOCALAPPDATA%\csquad`。 |
| WSL | 同样的 Windows toast，通过 `powershell.exe` 发出。 |
| macOS | 来自 `~/.csquad/csquad.app` 的通知。这是 `install` 写入并签名的一个小应用包。macOS 会问一次权限：选 **允许**。如果错过了，在 系统设置 › 通知 里打开 csquad。 |
| Linux | 桌面的通知服务，通过 D-Bus。没有会话总线时不显示。 |

```sh
csquad notify test      # 发一条测试通知；它显示用的后端，或者为什么没有
csquad notify input     # 只通知 needs input 和 needs you
csquad notify off       # 关闭通知；csquad notify on 重新打开
```

环境变量 `CSQUAD_NOTIFY=on|input|off` 优先于这个设置。通知失败不会让命令失败；错误写到 `~/.csquad/logs/notify.log`。

## 命令参考

`csquad COMMAND --help` 显示单个命令的用法。`T12` 是任务 ID。大多数命令接受 `--all`，用于操作其他会话的任务。

| 命令 | 说明 |
| --- | --- |
| `launch --title T [flags] [-- ENGINE_ARGS]` | 启动任务。提示来自 stdin 或 `--prompt-file`。 |
| `status [--all] [--json]` | 显示本会话的任务：状态、时长、会话、最新消息。 |
| `peek T12 [-n 6]` | 显示任务的最后几条消息，工具调用折叠，最后一条回复完整显示。 |
| `wait T12 [T13...] [--timeout MIN]` | 任务需要输入、结束、失败或被删除时返回。 |
| `send T12 "text"` | 发送一条后续指令。省略 text 时从 stdin 读取。 |
| `finish T12 [T13...] [--into BRANCH]` | 确认工作已合并且干净，然后删除 worktree、分支和会话。 |
| `cancel T12 [--discard]` | 停止任务并把它从列表中移除。未合并的工作保留，除非加 `--discard`；`--discard` 对已取消的任务也有效。 |
| `retry T12` | 让需要你处理的任务从停下的地方继续。 |
| `statusline` | 状态栏命令，从 stdin 读取 Claude Code 的 JSON。 |
| `install` | 把二进制文件装到 `~/.csquad/bin`，并安装 skill 和状态栏包装。 |
| `update` | 对已有的安装做同样的事。 |
| `uninstall` | 恢复状态栏，删除 `install` 写入的内容。保留 `csquad.db` 和 `models.md`。 |
| `status-install` | 显示安装了什么。 |
| `notify [test\|on\|input\|off]` | 显示通知设置和后端，发一条测试通知，或选择发哪些通知。 |
| `version`、`help` | 显示版本或用法。 |

`hook`、`check` 和 `advance` 是内部命令，供 Stop hook 和状态栏使用。

`launch` 的参数：

| 参数 | 说明 |
| --- | --- |
| `--title T` | 任务标题，前面会加上 ID：`T12 · title`。必填。 |
| `--cwd DIR` | 在 DIR 里运行任务。默认是当前目录。 |
| `--engine claude\|codex` | 引擎。默认是 `claude`。 |
| `--model M` | 任务使用的模型。 |
| `--after T3,T4` | 等这些任务完成后再开始。 |
| `--check SPEC` | 任务停下时检查工作：`codex[:MODEL]`、`claude[:MODEL]` 或 `cmd:COMMAND`。 |
| `--max N` | 检查的最大轮数。默认是 3。 |
| `--merge [BRANCH]` | 工作完成并通过检查后合并。默认是主 checkout 所在的分支。 |
| `--isolation none` | 直接在工作目录里改。不建 worktree，不建分支，不提交。 |
| `--fork` | 从当前对话 fork 出任务。不能和 `--after` 一起用。 |
| `--prompt-file F [--rm-prompt]` | 从 F 读取提示。`--rm-prompt` 在读取后删除 F。 |
| `--no-worker-rules` | 不加入 worker 规则，用于自带规则的提示。 |
| `-- ENGINE_ARGS` | 传给引擎的参数：`claude --bg` 的选项，或 Codex 线程的选项。 |

## 配置

### 模型路由：`~/.csquad/models.md`

这个文件归你所有，csquad 从不写它。Claude 每次启动任务前都会读它。用它告诉 Claude 什么样的工作用哪个引擎、哪个模型、哪些参数。保持简短，并明确写出每个模型的名字。不指定模型时，任务使用引擎的默认模型。

```markdown
# Model routing

- Small fixes, docs, tests: claude, `-- --model haiku`
- Features and refactors: claude, `-- --model opus --effort high`
- Reviews: `--check codex`

## When to use create-task

Also use it when I say "farm this out" or "background this".
```

`install` 和 `update` 会把 `## When to use create-task` 这一节加到 create-task skill 的描述里。用它写你自己的触发说法。改完后运行 `csquad update`。

### 环境变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `CSQUAD_HOME` | `~/.csquad` | 二进制文件、任务数据库 `csquad.db`、`models.md`、日志和缓存。 |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Claude Code 的配置。skill 装到它的 `skills` 目录。 |
| `CODEX_HOME` | `~/.codex` | Codex 的配置。如果它存在，`install` 也会设置 Codex。 |
| `NO_COLOR` | 未设置 | 设为任意值即可去掉状态栏的颜色。 |
| `CSQUAD_NOTIFY` | `on` | `input`：只通知 needs input 和 needs you。`off`：不通知。优先于 `csquad notify`。 |

Claude Code 的后台服务看不到你的 shell 环境。所以 `csquad launch` 会把当前会话的以下变量复制到任务的设置里：

- `ANTHROPIC_*`
- `CLAUDE_CODE_USE_*`
- `CLAUDE_CODE_OAUTH_TOKEN`
- `CLAUDE_CODE_SUBAGENT_MODEL`

这样任务就和你的会话使用同一个提供方。csquad 把它们写到 `~/.csquad/settings/T12.json`（权限 600），从不写到命令行里。任务结束或取消时，这个文件会被删除。如果任务的设置、你的设置或项目设置已经选了提供方，csquad 一个变量也不复制。

### Worktree

Claude Code 任务从你本地的 `HEAD` 创建 worktree，所以能看到你还没推送的提交。为此 csquad 把 `worktree.baseRef` 设为 `"head"`。要改的话，在你的设置、项目设置或 `--` 之后设置 `worktree.baseRef`。

### 文件

```text
~/.csquad/
  bin/csquad         skill 和状态栏运行的二进制文件
  csquad.db          任务数据库（SQLite）
  models.md          你的模型路由（可选，csquad 从不写它）
  notify             通知设置（csquad notify on|input|off）
  csquad.app/        macOS：显示通知的应用包
  csquad.png         Linux：通知图标
  manifest.json      install 写入了什么
  statusline.json    你原来的状态栏
  settings.json.bak  首次安装时 Claude Code settings.json 的副本（权限 600）
  settings/T12.json  任务存在期间的会话设置（权限 600）
  prompts/T12.md     太长、放不进命令行的提示
  cache/             状态栏最近一次的输出和用量读数
  logs/              检查日志、events.jsonl（每次阶段变化一行）、advance.log、notify.log
~/.claude/skills/    create-task、task-status、message-task、watch-task、finish-task
```

### 状态栏包装

`install` 保留你自己的状态栏：它把原来的命令存到 `~/.csquad/statusline.json`，用同样的输入先运行它。它不改 `settings.json` 里的其他键。`uninstall` 会原样恢复 `statusLine`；如果你原来没有，就删除这个键。

### Codex

`$CODEX_HOME` 存在时，`install` 还会：

- 把 skill 装到 `~/.agents/skills`，让 Codex 会话也能创建任务。如果 `~/.claude/skills` 和 `~/.agents/skills` 是同一个目录（一个链接到另一个），它在那里每个 skill 只写一份，里面同时保留给两个 agent 的说明，分别标为 **Claude Code:** 和 **Codex:**。
- 在 `~/.codex/hooks.json` 里加一组 Stop hook，标记为 `"__csquad__": true`。

在 Codex 里用 `/hooks` 批准一次这个 hook。Codex 必须在沙箱外运行 `csquad`。Codex 没有状态栏，所以 Codex 会话里看不到任务行。用 `csquad status`，或者问“我的任务进展如何？”。

## 更新和卸载

### 更新

运行以下任一命令：

```sh
npx csquad@latest update
~/.csquad/bin/csquad update     # 通过 npm 安装时，它会运行 npx --yes csquad@latest update
```

`npx csquad@3 install` 会停留在 3.x 版本。`install` 和 `update` 可以重复运行。如果你改过某个 skill，`update` 会把你的版本另存为旁边的 `.bak` 文件，并告诉你。

### 卸载

```sh
~/.csquad/bin/csquad uninstall
```

`uninstall` 恢复你的状态栏，删除 skill、二进制文件和 Codex hook，也删除 `~/.csquad` 里的日志、设置、提示和缓存，以及通知的设置：应用 ID 和它的图标（Windows、WSL）或 `csquad.app`（macOS）。它保留 `~/.csquad/csquad.db`、`~/.csquad/models.md` 和通知设置。正在运行的任务继续运行。

### 从 csquad 1.x 迁移

csquad 3 不会迁移 1.x 的安装（`~/.claude/csquad/`）。

1. 卸载 1.x，这会恢复你自己的状态栏：

   ```sh
   npx csquad@1 uninstall
   ```

2. 安装 csquad 3：

   ```sh
   npx csquad@latest install
   ```

如果你先运行了 `install`，它会停下，不做任何改动，并提示你先卸载 1.x。

## 故障排查和常见问题

**状态栏不显示任务行。**
运行 `~/.csquad/bin/csquad status-install`。如果项目里的 `.claude/settings.json` 设置了自己的 `statusLine`，在这个项目里它会取代 csquad 的包装。另外，状态栏只显示当前对话的任务。`/clear` 之后，运行 `csquad status --all`。

**启动失败，提示工作区不受信任。**
在那个目录里打开一次 `claude`，接受信任提示，然后再试。

**任务报 “Login expired”，或用错了提供方。**
任务使用创建它的会话的提供方变量。从带有这些变量的 shell 启动 `claude`，再重新创建任务。

**我发了消息，任务却没有反应。**
如果任务的设置不接受跨会话消息，Claude Code 会扣住消息等你批准。csquad 默认接受；你在 `-- --settings` 里设的 `crossSessionInbound` 值会改变这一点。打开 agent view（`claude agents`），选中任务并批准这条消息。这样的任务在 `csquad status` 和状态栏里显示 `needs input`，即使它已经完成。

**任务显示 `needs you · max rounds`。**
每一轮检查都没通过。运行 `csquad status` 查看发现的问题。发一条带提示的消息，再运行 `csquad retry T7` 多跑一轮。或者取消任务。

**`csquad finish` 拒绝删除任务。**
输出里列出了不在目标分支里的提交和未提交的文件。合并、cherry-pick 或提交这些工作，然后再运行 `csquad finish`。想保留工作、不再跟踪任务，运行 `csquad cancel T7`；想删除这些工作，运行 `csquad cancel T7 --discard`。

**任务显示 `gone`。**
任务的会话在 csquad 之外被删除了（例如在 agent view 里），但它的部分工作（worktree 或分支）还在。运行 `csquad cancel T7` 把任务从列表中移除；它会保留未合并的工作，并告诉你在哪里。`csquad cancel T7 --discard` 会删除这些工作。如果任务什么都没留下，csquad 会自动把它从列表中移除。带检查或合并的任务这时显示的是 `needs you · session gone`。

**最多能同时运行多少个任务？**
csquad 不设上限。每个任务用自己的会话和 token，跑多少由你决定。

**任务会自己合并吗？**
不会。只有加了 `--merge`，也就是你要求时（“通过后合并”），任务才会合并。否则由你让 Claude 去合并。

**任务的历史在哪里？**
`~/.csquad/logs/events.jsonl` 每次阶段变化记一行。`csquad peek T12` 显示任务的对话。

**`npm i -g csquad` 没装上二进制文件。**
npm 把二进制文件作为可选依赖安装，例如 `@shunl12324/csquad-linux-x64`。关闭可选依赖（`--omit=optional`）时，npm 不会安装二进制文件，`csquad` 命令会提示你。请带上可选依赖重新安装。

## 参与贡献

构建 csquad、运行测试或发布版本，见 [CONTRIBUTING.md](https://github.com/ShunL12324/c-squad/blob/main/CONTRIBUTING.md)。

MIT 许可证，见 [LICENSE](https://github.com/ShunL12324/c-squad/blob/main/LICENSE)。
