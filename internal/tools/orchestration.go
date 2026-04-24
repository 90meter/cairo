package tools

import (
	"fmt"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// orchestration.go — consolidated job and task tools.
// A job is a unit of work (goal + status + result). A task is a step within
// a job (with an assigned role and optional dependencies on other tasks).

// --- job ---

type jobTool struct{ db *db.DB }

func Job(database *db.DB) agent.Tool { return jobTool{db: database} }

func (jobTool) Name() string { return "job" }
func (jobTool) Description() string {
	return `Manage jobs — units of orchestrated work.
Actions:
- create: start a new job. Args: title, description (both required); orchestrator_role (optional, default "orchestrator").
- list: return all jobs with status. No extra args.
- update: set a job's status and optionally its result. Args: id, status (required); result (optional).
- delete: remove a job and its tasks. Args: id (required).`
}
func (jobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "list", "update", "delete"},
				"description": "Operation to perform.",
			},
			"id":                prop("integer", "Job ID — required for update, delete."),
			"title":             prop("string", "Job title — required for create."),
			"description":       prop("string", "Job description — required for create."),
			"orchestrator_role": prop("string", "Role that will orchestrate — optional for create (default 'orchestrator')."),
			"status":            prop("string", "New status (pending|running|done|failed|cancelled) — required for update."),
			"result":            prop("string", "Result or summary — optional for update."),
		},
		"required": []string{"action"},
	}
}

func (t jobTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "create":
		return t.doCreate(args)
	case "list":
		return t.doList()
	case "update":
		return t.doUpdate(args)
	case "delete":
		return t.doDelete(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (create|list|update|delete)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: create|list|update|delete", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t jobTool) doCreate(args map[string]any) agent.ToolResult {
	title := strArg(args, "title")
	description := strArg(args, "description")
	if title == "" || description == "" {
		return agent.ToolResult{Content: "error: title and description are required for create", IsError: true}
	}
	role := strArg(args, "orchestrator_role")
	if role == "" {
		role = db.RoleOrchestrator
	}
	j, err := t.db.Jobs.Create(title, description, role, nil)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("job created (id: %d): %s", j.ID, j.Title), Details: j}
}

func (t jobTool) doList() agent.ToolResult {
	jobs, err := t.db.Jobs.List()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(jobs) == 0 {
		return agent.ToolResult{Content: "no jobs"}
	}
	var b strings.Builder
	for _, j := range jobs {
		fmt.Fprintf(&b, "[%d] [%s] %s\n", j.ID, j.Status, j.Title)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: jobs}
}

func (t jobTool) doUpdate(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	status := strArg(args, "status")
	if id == 0 || status == "" {
		return agent.ToolResult{Content: "error: id and status are required for update", IsError: true}
	}
	if err := t.db.Jobs.SetStatus(id, status); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if result := strArg(args, "result"); result != "" {
		if err := t.db.Jobs.SetResult(id, result); err != nil {
			return agent.ToolResult{Content: fmt.Sprintf("error setting result: %v", err), IsError: true}
		}
	}
	return agent.ToolResult{Content: fmt.Sprintf("job %d → %s", id, status)}
}

func (t jobTool) doDelete(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for delete", IsError: true}
	}
	if err := t.db.Jobs.Delete(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("job %d deleted", id)}
}

// --- task ---

type taskTool struct{ db *db.DB }

func Task(database *db.DB) agent.Tool { return taskTool{db: database} }

func (taskTool) Name() string { return "task" }
func (taskTool) Description() string {
	return `Manage tasks — individual steps within a job, each assigned to a role.
Actions:
- create: add a task to a job. Args: job_id, title, description (required); assigned_role (optional, default "coder"); depends_on (optional JSON array of task IDs, default []).
- list: list all tasks for a job. Args: job_id (required).
- update: change status and optionally set result. Args: id, status (required); result (optional).
- delete: remove a task. Args: id (required).
- ready: list tasks in a job whose dependencies are all done. Args: job_id (required).
- artifacts: list files/output produced by a background task. Args: id (required).`
}
func (taskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "list", "update", "delete", "ready", "artifacts"},
				"description": "Operation to perform.",
			},
			"id":            prop("integer", "Task ID — required for update, delete, artifacts."),
			"job_id":        prop("integer", "Job ID — required for create, list, ready."),
			"title":         prop("string", "Task title — required for create."),
			"description":   prop("string", "What this task involves — required for create."),
			"assigned_role": prop("string", "Role that will execute — optional for create (default 'coder')."),
			"depends_on":    prop("string", "JSON array of prereq task IDs — optional for create."),
			"status":        prop("string", "New status (pending|running|done|failed|blocked) — required for update."),
			"result":        prop("string", "Result or notes — optional for update."),
		},
		"required": []string{"action"},
	}
}

func (t taskTool) Execute(args map[string]any, _ *agent.ToolContext) agent.ToolResult {
	switch strArg(args, "action") {
	case "create":
		return t.doCreate(args)
	case "list":
		return t.doList(args)
	case "update":
		return t.doUpdate(args)
	case "delete":
		return t.doDelete(args)
	case "ready":
		return t.doReady(args)
	case "artifacts":
		return t.doArtifacts(args)
	case "":
		return agent.ToolResult{Content: "error: action is required (create|list|update|delete|ready|artifacts)", IsError: true}
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("error: unknown action %q — valid: create|list|update|delete|ready|artifacts", strArg(args, "action")),
			IsError: true,
		}
	}
}

func (t taskTool) doCreate(args map[string]any) agent.ToolResult {
	jobID := int64(intArg(args, "job_id", 0))
	title := strArg(args, "title")
	description := strArg(args, "description")
	if jobID == 0 || title == "" || description == "" {
		return agent.ToolResult{Content: "error: job_id, title, and description are required for create", IsError: true}
	}
	role := strArg(args, "assigned_role")
	if role == "" {
		role = db.RoleCoder
	}
	dependsOn := strArg(args, "depends_on")
	if dependsOn == "" {
		dependsOn = "[]"
	}
	task, err := t.db.Tasks.Create(jobID, title, description, role, dependsOn)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{
		Content: fmt.Sprintf("task created (id: %d): %s → role: %s", task.ID, task.Title, task.AssignedRole),
		Details: task,
	}
}

func (t taskTool) doList(args map[string]any) agent.ToolResult {
	jobID := int64(intArg(args, "job_id", 0))
	if jobID == 0 {
		return agent.ToolResult{Content: "error: job_id is required for list", IsError: true}
	}
	tasks, err := t.db.Tasks.ForJob(jobID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(tasks) == 0 {
		return agent.ToolResult{Content: fmt.Sprintf("no tasks for job %d", jobID)}
	}
	var b strings.Builder
	for _, task := range tasks {
		dep := ""
		if task.DependsOn != "[]" && task.DependsOn != "" {
			dep = fmt.Sprintf(" (needs: %s)", task.DependsOn)
		}
		fmt.Fprintf(&b, "[%d] [%s] %s → %s%s\n", task.ID, task.Status, task.Title, task.AssignedRole, dep)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: tasks}
}

func (t taskTool) doUpdate(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	status := strArg(args, "status")
	if id == 0 || status == "" {
		return agent.ToolResult{Content: "error: id and status are required for update", IsError: true}
	}
	if err := t.db.Tasks.SetStatus(id, status); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if result := strArg(args, "result"); result != "" {
		if err := t.db.Tasks.SetResult(id, result); err != nil {
			return agent.ToolResult{Content: fmt.Sprintf("error setting result: %v", err), IsError: true}
		}
	}
	return agent.ToolResult{Content: fmt.Sprintf("task %d → %s", id, status)}
}

func (t taskTool) doDelete(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for delete", IsError: true}
	}
	if err := t.db.Tasks.Delete(id); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	return agent.ToolResult{Content: fmt.Sprintf("task %d deleted", id)}
}

func (t taskTool) doReady(args map[string]any) agent.ToolResult {
	jobID := int64(intArg(args, "job_id", 0))
	if jobID == 0 {
		return agent.ToolResult{Content: "error: job_id is required for ready", IsError: true}
	}
	tasks, err := t.db.Tasks.ReadyTasks(jobID)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(tasks) == 0 {
		return agent.ToolResult{Content: "no tasks are ready to run"}
	}
	var b strings.Builder
	for _, task := range tasks {
		fmt.Fprintf(&b, "[%d] %s → %s\n", task.ID, task.Title, task.AssignedRole)
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: tasks}
}

func (t taskTool) doArtifacts(args map[string]any) agent.ToolResult {
	id := int64(intArg(args, "id", 0))
	if id == 0 {
		return agent.ToolResult{Content: "error: id is required for artifacts", IsError: true}
	}
	artifacts, err := t.db.TaskArtifacts.ForTask(id)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}
	}
	if len(artifacts) == 0 {
		return agent.ToolResult{Content: fmt.Sprintf("no artifacts for task %d", id)}
	}
	var b strings.Builder
	for _, a := range artifacts {
		switch a.Type {
		case "file":
			fmt.Fprintf(&b, "[file] %s (via %s)\n", a.Path, a.ToolName)
		case "output":
			preview := a.Content
			if len(preview) > 200 {
				preview = preview[:200] + "…"
			}
			fmt.Fprintf(&b, "[output] %s\n", preview)
		default:
			fmt.Fprintf(&b, "[%s] %s\n", a.Type, a.Path)
		}
	}
	return agent.ToolResult{Content: strings.TrimSpace(b.String()), Details: artifacts}
}
