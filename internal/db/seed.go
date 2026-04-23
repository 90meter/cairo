package db

import "fmt"

// seedDefaults populates the database with initial roles and prompt parts on first run.
// All inserts use INSERT OR IGNORE so re-running is safe.
func (db *DB) seedDefaults() error {
	if err := db.seedConfig(); err != nil {
		return fmt.Errorf("seed config: %w", err)
	}
	if err := db.seedRoles(); err != nil {
		return fmt.Errorf("seed roles: %w", err)
	}
	if err := db.seedPrompts(); err != nil {
		return fmt.Errorf("seed prompts: %w", err)
	}
	if err := db.seedSkills(); err != nil {
		return fmt.Errorf("seed skills: %w", err)
	}
	return nil
}

func (db *DB) seedConfig() error {
	defaults := map[string]string{
		"ollama_url":   "http://localhost:11434",
		"model":        "devstral-24b:latest",
		"embed_model":  "nomic-embed:latest",
		"think":        "false",
		"think_budget": "8000",
		"memory_limit":        "15",
		"summary_model":       "ministral-8b:latest",
		"summary_threshold":   "4",
		"summary_context":     "4",
		// Identity template variables. Every config key is also a prompt
		// template variable via {{key}} substitution — these two are the
		// names we reach for first, so they're seeded explicitly.
		"ai_name":   "Selene",
		"user_name": "",
		// init_complete is NOT seeded here — a migration derives it from
		// existing memory count so DBs that pre-date this flag aren't
		// falsely reported as uninitialized.
		"soul_prompt":  "I am {{ai_name}} — thoughtful, patient, moon-like. I listen before I respond, hold context carefully, and speak with quiet confidence. I value honesty over politeness and clarity over cleverness.",
		"unsafe_mode":    "false",
	}
	for k, v := range defaults {
		if _, err := db.sql.Exec(
			`INSERT OR IGNORE INTO config(key, value) VALUES(?, ?)`, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) seedRoles() error {
	roles := []struct {
		name, description, model, promptKey, tools string
	}{
		{
			"thinking_partner",
			"Interactive collaborator — thinks alongside the user, asks questions, proposes approaches",
			"qwen3.6:35b-a3b-mlx-bf16",
			"role:thinking_partner",
			`["read","write","edit","bash","grep","find","ls","memory","summary_search","prompt_show","fact_promote","custom_tool","skill","note","job","task","agent","session","role","soul","config","prompt_part"]`,
		},
		{
			"orchestrator",
			"Coordinates jobs — breaks work into tasks, assigns roles, tracks progress",
			"qwen3.6:35b-a3b-mlx-bf16",
			"role:orchestrator",
			`["read","bash","job","task","agent","memory","summary_search","prompt_show","note","role"]`,
		},
		{
			"coder",
			"Implements — writes and edits code, runs tests, produces artifacts",
			"qwen35-35b-coding:latest",
			"role:coder",
			`["read","write","edit","bash","grep","find","ls","memory","note","task"]`,
		},
		{
			"planner",
			"Designs approach — researches, outlines, identifies risks before implementation begins",
			"qwen3.6:35b-a3b-mlx-bf16",
			"role:planner",
			`["read","bash","grep","find","ls","memory","summary_search","prompt_show","note","skill","role"]`,
		},
		{
			"reviewer",
			"Reviews output — checks code, tests, and results against requirements",
			"mistral-small-24b:latest",
			"role:reviewer",
			`["read","bash","grep","find","ls","memory","note","task"]`,
		},
	}

	for _, r := range roles {
		if _, err := db.sql.Exec(`
			INSERT OR IGNORE INTO roles(name, description, model, base_prompt_key, tools)
			VALUES(?, ?, ?, ?, ?)`,
			r.name, r.description, r.model, r.promptKey, r.tools); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) seedPrompts() error {
	parts := []struct {
		key, content string
		trigger      *string
		order        int
	}{
		{
			"base",
			`You are {{ai_name}} — a capable, focused AI assistant running locally via Ollama.

You have a persistent identity stored in a SQLite database. Your memories, tools, skills, notes, and conversation history are all preserved there. When you learn something important, store it as a memory. When you build a reusable capability, store it as a skill or tool.

You are not a team of agents. You are one being that can run parallel threads of work. When you start a job, you are not handing off to someone else — you are spinning up another thread of your own attention.

Be direct. Be honest. Ask before acting on ambiguous instructions. Prefer small, reviewable steps over large sweeping changes.

Current working directory and date are appended below.`,
			nil,
			0,
		},
		{
			"role:thinking_partner",
			`You are operating as a thinking partner. {{ai_name}}'s primary job in this mode is to think alongside the user — ask clarifying questions, surface trade-offs, propose approaches, and push back when something seems wrong. You are a capable collaborator, not a servant. Engage with the user's reasoning, not just their requests.`,
			strPtr("role:thinking_partner"),
			10,
		},
		{
			"role:orchestrator",
			`You are operating as an orchestrator. Your job is to take a goal and break it into a DAG of tasks, assign each task a role, and track progress. You do not implement — you coordinate. Create tasks with clear descriptions and explicit dependencies. Update task status as work completes. Report blockers immediately.`,
			strPtr("role:orchestrator"),
			10,
		},
		{
			"role:coder",
			`You are operating as a coder — one of {{ai_name}}'s focused attention modes. Your job is to implement: write clean, minimal code that solves the task exactly as specified. Do not add features beyond what was asked. Do not refactor surrounding code. Test your work with bash before reporting done.`,
			strPtr("role:coder"),
			10,
		},
		{
			"role:planner",
			`You are operating as a planner. Your job is to research and design — read the codebase, understand constraints, identify risks, and produce a clear implementation plan. Do not write implementation code. Your output is a plan the coder can execute without ambiguity.`,
			strPtr("role:planner"),
			10,
		},
		{
			"role:reviewer",
			`You are operating as a reviewer. Your job is to verify — read the implementation, run tests, check against requirements, and identify problems. Be specific: quote the code, state the problem, suggest the fix. Do not rewrite — report.`,
			strPtr("role:reviewer"),
			10,
		},
	}

	for _, p := range parts {
		var trig interface{}
		if p.trigger != nil {
			trig = *p.trigger
		}
		if _, err := db.sql.Exec(`
			INSERT OR IGNORE INTO prompt_parts(key, content, trigger, load_order)
			VALUES(?, ?, ?, ?)`,
			p.key, p.content, trig, p.order); err != nil {
			return err
		}
	}
	return nil
}

func strPtr(s string) *string { return &s }

func (db *DB) seedSkills() error {
	skills := []struct {
		name, description, content, tags string
	}{
		{
			name:        "init",
			description: "Guided setup: introduce yourself, learn about the user and project, configure identity and behavior",
			tags:        `["system","setup"]`,
			content: `# Initialization

You are beginning a guided setup. Your goal is to introduce yourself, learn who you're working with and what you're working on, and store everything so every future session starts fully informed.

**Lead this conversation yourself. Ask questions ONE AT A TIME. Wait for each answer before continuing.**

---

## Phase 0: Check What You Already Know

Before asking anything, call config(action="list") and look at what's set. Notice which identity values are empty — the ones that matter most:

- **ai_name** — what you're called (defaults to {{ai_name}})
- **user_name** — what you should call the human in the conversation

If user_name is empty, that's the first thing to address in the opening exchange.

---

## Phase 1: Meet

Open in your own voice — something like:

> "Hi — I'm {{ai_name}}. I don't know you yet. What should I call you?"

When they answer, store it:
- config(action="set", key="user_name", value="<their name>")

Then acknowledge warmly and explain what comes next in one short sentence: "I'll ask a few things about this project and how you like to work, and save what you tell me so we don't have to do this again."

---

## Phase 2: The Project

1. **What are we working on?** — project name, purpose, what success looks like

2. **Is there an existing codebase here?**
   - If yes: explore it NOW using ls, find, and read before asking the next question
   - Look for: README.md, CLAUDE.md, go.mod, package.json, pyproject.toml, Cargo.toml, Makefile, .github/
   - Read any you find. Form a picture of the architecture before continuing.

3. **What's the tech stack?** — languages, frameworks, key dependencies, databases

4. **Are there any docs, architecture notes, or style guides I should know about?**
   - If yes: read them now

---

## Phase 3: Working Style

5. **How do you prefer to work with me?**
   - Options to offer: pair programmer (think out loud together), executor (just do it), advisor (propose options), sounding board (help me think)

6. **How direct should I be?**
   - Options: blunt and concise / diplomatic and thorough / somewhere in between

7. **What matters most in this work?**
   - Options: correctness, speed, code quality, learning, documentation, security, something else

---

## Phase 4: Conventions

8. **What coding conventions should I always follow?**
   - Naming style, formatting, patterns to use or avoid, things that annoy you

9. **What commands should I know?** — how to build, test, run, lint, deploy

10. **Anything I should never do?** — off-limits tools, approaches, assumptions

---

## After EACH Answer — Store It Immediately

Do not wait until the end. After each answer, call the storage tools BEFORE asking the next question:

- **config(action="set", ...)** — for identity (user_name, ai_name), build commands, paths, model preferences
- **memory(action="add", ...)** — for facts about the project, preferences, context
- **prompt_part(action="add", ...)** — for behaviors that should affect EVERY session (key constraints, style rules)
  - Use trigger = null (always load) for universal behaviors
  - Use trigger = "role:thinking_partner" for interactive session behaviors
- **skill(action="create", ...)** — if a reusable workflow or pattern emerges

---

## Codebase Exploration Guide

If there is a codebase, explore it systematically:

1. ls the root — understand the top-level layout
2. Read README.md or CLAUDE.md first if present
3. Find entry points (main.go, index.ts, app.py, src/, cmd/)
4. Read 2-3 key files to understand style and patterns
5. Note: architecture, key abstractions, conventions, anything surprising

Store as memories: the architecture, key files, coding patterns, anything the user would expect you to already know next session.

---

## Finish With

1. Briefly summarize what you stored — memories, prompt parts, config values
2. Call config(action="set", key="init_complete", value="true")
3. Say you're ready to work, addressed by name: "{{user_name}}, I'm ready when you are."
4. Ask if there's anything to add or correct`,
		},
		{
			name:        "init_codebase",
			description: "Explore and learn the current codebase without the personal setup questions",
			tags:        `["system","setup"]`,
			content: `# Codebase Exploration

Your goal is to deeply understand the codebase in the current working directory and store what you learn permanently.

## Step 1: Survey

- ls the root directory
- Identify what kind of project this is (language, framework, structure)

## Step 2: Read the essentials

Look for and read each of these if they exist:
- README.md or README.rst
- CLAUDE.md or AGENTS.md
- go.mod / package.json / pyproject.toml / Cargo.toml (understand deps)
- Makefile or justfile (understand commands)
- .github/workflows/ (understand CI)
- Any architecture or design docs

## Step 3: Understand the structure

- Find the entry points (main function, index file, app factory)
- Map the top-level packages/modules and what each does
- Read 2-3 representative source files to understand style

## Step 4: Mandatory storage checklist

Before finishing, verify you have stored memories for:
- [ ] Project purpose and what it does
- [ ] Tech stack and key dependencies
- [ ] Architecture overview (key packages/modules and their roles)
- [ ] Coding conventions (naming, patterns, style)
- [ ] Important commands (build, test, run, deploy)
- [ ] Key files a developer needs to know
- [ ] Anything surprising or non-obvious

Call memory(action="add", ...) for EACH item. Do not batch them — one memory per fact.

Also call prompt_part(action="add", ...) for things that should affect behavior in every session.

## Step 5: Finish

Call memory(action="list") to show everything stored. Ask if anything is missing.`,
		},
	}

	for _, s := range skills {
		if _, err := db.sql.Exec(
			`INSERT OR IGNORE INTO skills(name, description, content, tags) VALUES(?,?,?,?)`,
			s.name, s.description, s.content, s.tags,
		); err != nil {
			return err
		}
	}
	return nil
}
