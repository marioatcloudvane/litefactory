package factorylib

import (
	"slices"
	"strings"
)

// Role names one factory agent of the library. The set is closed: only the
// constants below exist, and each maps to exactly one fixed file.
type Role string

// The factory roles. None of them belongs to a project team.
const (
	ConversationNative         Role = "conversation-native"
	ConversationFactory        Role = "conversation-factory"
	ConversationCorrection     Role = "conversation-correction"
	ConversationActionOutcome  Role = "conversation-action-outcome"
	ConversationReasoner       Role = "conversation-reasoner"
	ConversationInboxOrganizer Role = "conversation-inbox-organizer"
	Judge                      Role = "judge"
	MergeConflictAdvisor       Role = "merge-conflict-advisor"
	ScheduleProposalAdvisor    Role = "schedule-proposal-advisor"
	ChatDraft                  Role = "chat-draft"
	StandardsAuthor            Role = "standards-author"
	LogAnalyst                 Role = "log-analyst"
	TaskAdvisory               Role = "task-advisory"
	DriftAnalyst               Role = "drift-analyst"
)

// Fixed library paths that callers read besides a role's own file.
const (
	// InboxOrganizerSchema is the organizer's result contract, captured with it.
	InboxOrganizerSchema = "resources/inbox-organizer/schema.md"
	// StandardsAuthorSchema is the standards author's response schema, captured with it.
	StandardsAuthorSchema = "resources/standards-author/schema.md"
	// StandardsAuthoringSkill guides the factory conversation through authoring a standard.
	StandardsAuthoringSkill = "standards-authoring"
	// TaskAdvisorySkill is the task advisory role's result skill.
	TaskAdvisorySkill = "task-advisory"
	// DoctorDir holds the Doctor workflow: its graph, role and skill.
	DoctorDir = "doctor"
	// DoctorGraph is the Doctor graph file inside DoctorDir.
	DoctorGraph = "doctor.dot"
	// ConnectorDir holds the chat connector guidance; its files sit under guidance/.
	ConnectorDir = "connector"
	// ReadmePath documents the layout; it is the one file no code reads.
	ReadmePath = "README.md"
	// ManifestPath is written by Install next to the installed files.
	ManifestPath = "manifest.json"
)

type roleSpec struct {
	path      string
	resources []string
	skills    []string
}

var roles = map[Role]roleSpec{
	ConversationNative:         {path: "roles/conversation-native.md"},
	ConversationFactory:        {path: "roles/conversation-factory.md", skills: []string{StandardsAuthoringSkill}},
	ConversationCorrection:     {path: "roles/conversation-correction.md"},
	ConversationActionOutcome:  {path: "roles/conversation-action-outcome.md"},
	ConversationReasoner:       {path: "roles/conversation-reasoner.md"},
	ConversationInboxOrganizer: {path: "roles/conversation-inbox-organizer.md", resources: []string{InboxOrganizerSchema}},
	Judge:                      {path: "roles/judge.md"},
	MergeConflictAdvisor:       {path: "roles/merge-conflict-advisor.md"},
	ScheduleProposalAdvisor:    {path: "roles/schedule-proposal-advisor.md"},
	ChatDraft:                  {path: "roles/chat-draft.md"},
	StandardsAuthor:            {path: "roles/standards-author.md", resources: []string{StandardsAuthorSchema}},
	LogAnalyst:                 {path: "roles/log-analyst.md"},
	TaskAdvisory:               {path: "roles/task-advisory.md", skills: []string{TaskAdvisorySkill}},
	DriftAnalyst:               {path: "drift/analyst.md"},
}

// skillFiles are every skill file the role skills above reach.
var skillFiles = []string{
	"skills/standards-authoring/SKILL.md",
	"skills/task-advisory/SKILL.md",
	"skills/task-advisory/result.md",
}

var doctorFiles = []string{"doctor/doctor.dot", "doctor/doctor.md", "doctor/skills/doctor-result/SKILL.md"}

// connectorGuidance lists the connector guidance files, without guidance/ and .md.
var connectorGuidance = []string{
	"action_cancel", "action_confirm", "action_history", "action_inspect", "action_propose",
	"advisory_job_cancel", "advisory_job_get", "ask_task_agent",
	"brainstorm_add", "brainstorm_ask", "brainstorm_close", "brainstorm_draft", "brainstorm_drafts_get",
	"brainstorm_entries", "brainstorm_get", "brainstorm_list", "brainstorm_open", "brainstorm_scan_cancel",
	"brainstorm_scan_get", "brainstorm_scans", "brainstorm_tasks_get", "bug_file_propose",
	"discussion_open", "discussion_summary_get", "discussion_summary_publish",
	"factory_guide", "factory_overview",
	"guide/01_how", "guide/10_review", "guide/11_running", "guide/13_feature", "guide/14_feature_unavailable",
	"guide/15_bug", "guide/16_bug_unavailable", "guide/17_usage", "guide/18_workflow", "guide/20_brainstorm",
	"guide/21_advisory", "guide/22_code", "guide/23_standards", "guide/24_publish", "guide/25_recover",
	"issue_search", "judge_task",
	"overview/brainstorm", "overview/factory", "overview/missing_scopes", "overview/scopes_complete",
	"prompts/brainstorm_feature", "prompts/review_task", "prompts/whats_waiting",
	"repo_diff", "repo_grep", "repo_log", "repo_read", "skill/header",
	"standards_read", "standards_role_counts", "task_evidence_read", "usage_read",
	"workflow_continue_propose", "workflow_describe", "workflow_job_get", "workflow_propose", "workflows_list",
}

// Roles lists every registered role in a stable order.
func Roles() []Role {
	out := make([]Role, 0, len(roles))
	for role := range roles {
		out = append(out, role)
	}
	slices.Sort(out)
	return out
}

// Valid reports whether role is registered.
func (r Role) Valid() bool { _, ok := roles[r]; return ok }

// Path is the role's fixed library path, or "" for an unregistered role.
func (r Role) Path() string { return roles[r].path }

// Files lists every path the library must hold, sorted. An installed library
// holds exactly these files plus its manifest.
func Files() []string {
	set := map[string]bool{ReadmePath: true}
	for _, spec := range roles {
		set[spec.path] = true
		for _, name := range spec.resources {
			set[name] = true
		}
	}
	for _, name := range append(append(slices.Clone(skillFiles), doctorFiles...), connectorFiles()...) {
		set[name] = true
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func connectorFiles() []string {
	out := make([]string, 0, len(connectorGuidance))
	for _, name := range connectorGuidance {
		out = append(out, ConnectorDir+"/guidance/"+name+".md")
	}
	return out
}

// legacyRole maps a role name retained by an older capture to its library role.
func legacyRole(name string) Role {
	if name == "challenger" {
		return Judge
	}
	return Role(name)
}

// legacyPath maps a file path retained by an older capture to its library path;
// "" drops a file that has no library counterpart (catalogue, metadata, retired
// judge resources).
func legacyPath(name string) string {
	switch {
	case strings.HasPrefix(name, "agents/") && strings.HasSuffix(name, ".md"):
		role := legacyRole(strings.TrimSuffix(strings.TrimPrefix(name, "agents/"), ".md"))
		return role.Path()
	case strings.HasPrefix(name, "skills/"), name == InboxOrganizerSchema, name == StandardsAuthorSchema:
		return name
	default:
		return ""
	}
}
