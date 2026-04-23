# Background work

Cairo can parallelize. A long-running piece of work can be broken into tasks, each assigned to a role, each running in its own subprocess with its own session. The parent session stays responsive; completed tasks surface as inbox notes on its next turn.

This is the rhizomatic model in action: the being doesn't hand work off to colleagues — it spins up another thread of its own attention.

---

## Three concepts

**Job** — the high-level intent. "Refactor the memory subsystem" or "Add TUI panels for prompt preview, memory browser, and threads list." A job lives in the `jobs` table; its `orchestrator_role` (default `orchestrator`) indicates which role is coordinating.

**Task** — one chunk of work within a job. "Write the memory panel," "Test it," "Review it." A task has an `assigned_role`, a `depends_on` array of other task ids, and a terminal status (`done` | `failed`).

**Agent** — a running background subprocess. Spawned against a task. Has its own session, its own role-specific model, its own event log. Completes, writes result to the task, exits.

The analogy: **jobs** are what you'd write on a sticky note, **tasks** are the checklist, **agents** are the people doing the work — except all three are the same being in different modes.

---

## A worked example

Say you want to add a new TUI panel. The high-level shape:

```
Job: "Add soul editor panel to TUI"
  ├─ Task 1: plan the panel design (role: planner)
  ├─ Task 2: implement panel_soul.go (role: coder, depends on 1)
  ├─ Task 3: review implementation (role: reviewer, depends on 2)
  └─ Task 4: integration test (role: coder, depends on 3)
```

Interactive session:

```
> I want to add a soul editor panel to the TUI. Can you break this into tasks
  and run them in parallel where dependencies allow?

  [orchestrator mode]
  [calls job(action="create", title="Add soul editor panel to TUI")]
  [calls task(action="create") 4 times with dependencies]

Orchestrator: created job 12 with 4 tasks. Task 1 has no deps — spawning now.
  [calls agent(action="spawn", id=1)]

  (task 1 runs in the background, agent subprocess logs to ~/.cairo2/logs/task_1.log)
  (you can keep chatting in the foreground)

> (few minutes later) what's the status?

  [calls task(action="list", job_id=12)]
  task 1 [done]    planner    "plan the panel design"
  task 2 [pending] coder      "implement panel_soul.go"
  task 3 [pending] reviewer   "review implementation"
  task 4 [pending] coder      "integration test"

> ok, kick off task 2 — I want to see the plan first though.

  [calls agent(action="log", id=1, tail=40)]
  (shows task 1's output — the plan)

  Orchestrator: here's the plan ... [summarizes]

  ...
```

Meanwhile, when a task completes, the next turn you take includes an automatic note:

```
[background] while you were idle, these tasks reached a terminal state:
- task 1 [done] "plan the panel design" (role: planner): [first 300 chars of result]…

Weave into your response if relevant, or just acknowledge and continue.
```

This is the **background inbox** — the rhizomatic signal from parallel threads back to the coordinating thread.

---

## How spawning actually works

When `agent(action="spawn", id=N)` fires:

1. **Atomic claim.** `Tasks.ClaimForSpawn(N)` does a single `UPDATE tasks SET status='running' WHERE id=N AND status IN ('pending','blocked') AND <deps done>` and checks rows-affected. If zero, the task wasn't ready or someone else already claimed it — error out.
2. **Log file created.** `~/.cairo2/logs/task_N.log` is truncated/created; path saved to `tasks.log_path`.
3. **Subprocess spawned.** `exec.Command(cairo_binary, -task=N, -background, -new)` with detached process attrs (platform-specific — see `spawn_unix.go`). Stdout and stderr redirect to the log file.
4. **PID stored.** `tasks.pid = cmd.Process.Pid`, `cmd.Process.Release()` — we don't wait.
5. **Parent returns immediately.** The tool result is `"task N spawned (pid X, role: Y)\nlog: <path>"`. The parent session continues unimpeded.

The subprocess then runs `runTask` in `cmd/cairo/main.go`, which:
- Opens the DB (its own connection; competes for the file lock via WAL+busy_timeout)
- Creates a session for the task
- Resolves the model from the task's `assigned_role`
- Loads tools filtered by that role
- Runs the task description through the agent as a single prompt
- Captures the result (assistant text) and writes it to `tasks.result`
- Sets `tasks.status = 'done' | 'failed'`
- Exits

---

## Waiting on a task

`agent(action="wait", id=N, timeout=300)` polls every 2 seconds until the task is `done` or `failed`, or the timeout elapses.

The polling subscriber also publishes `EventToolUpdate` events with the current status on every poll, so the TUI's tool line shows `agent: task 3: running` while the wait is in progress.

A timeout returns an error without killing the subprocess — it just gives up waiting.

---

## The dependency DAG

Tasks declare their dependencies as a JSON array of task ids:

```
task(action="create", job_id=12, title="review", assigned_role="reviewer", depends_on=[2])
```

When spawning task 3, `ClaimForSpawn` verifies every id in `depends_on` has a task row with `status='done'`. If any isn't done, claim fails — the task stays `pending` and can't be spawned.

`task(action="ready", job_id=12)` returns the subset of pending tasks whose deps are all done — the orchestrator's "what can I run now?" query.

There's no cycle detection. If you write a cyclic DAG, no task will ever become ready. The orchestrator is expected to get this right; validation lives in the prompt, not the code.

---

## What the orchestrator actually does

The `orchestrator` role has a prompt part that frames it:

> You are operating as an orchestrator. Your job is to take a goal and break it into a DAG of tasks, assign each task a role, and track progress. You do not implement — you coordinate. Create tasks with clear descriptions and explicit dependencies. Update task status as work completes. Report blockers immediately.

And a restricted tool set: `read, bash, job, task, agent, memory, summary_search, prompt_show, note, role`. No `write`, no `edit`. The orchestrator can read code but can't change it — that's the coder's job.

---

## Reaping orphaned tasks

If the cairo process (foreground or background) crashes while a task is `running`, the DB is left with a stale row. On the next `Open()`, `ReapOrphanedTasks` checks every `status='running'` row: if `pid` is 0 or the process is no longer alive, status is flipped to `failed` with a result note.

So a crashed background worker doesn't leave the job stuck at "task N: still in flight forever."

---

## When not to use background work

Overkill for:
- Any flow where you want to see the model's thinking in real-time (background tasks log but you have to read the log)
- Anything under ~30 seconds of expected work (spawn+DB-reconnect overhead costs more than it saves)
- Exploration / "what happens if..." (parallel experiments are hard to compare; do them inline)

Good fit for:
- Multi-file refactors with verification steps
- Codebase audits ("review every *.go file for X")
- Anything you want to start and come back to

---

## Known rough edges

- **No heartbeats from running tasks.** A running task shows `status='running'` and nothing else until it completes. You read the log with `agent(action="log", id=N)` for progress. Heartbeats are on the roadmap — see [ROADMAP](../../ROADMAP.md), mid term.
- **`collectArtifacts` assumes sequential tool execution.** The artifact collector in `cmd/cairo/main.go` correlates tool-start with tool-end by goroutine-local state. If a future backend executes tool calls in parallel, this mis-pairs.
- **Logs are unbounded.** A long-running task's log grows without rotation. Delete old `~/.cairo2/logs/task_*.log` manually if disk pressure matters.
- **`role` inheritance is simple.** A spawned task uses its `assigned_role`. There's no mechanism for "inherit the parent's identity template variables" — they all come from the same config table, which is shared, so this is rarely a problem, but worth naming.
