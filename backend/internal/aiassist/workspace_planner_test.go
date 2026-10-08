package aiassist

import (
	"reflect"
	"testing"
)

func TestWorkspacePlannerExtractsOnlySuppliedClientFields(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Create a client named Alpha Managed Services with display ID ALPHA-100.",
	)

	if plan.Kind != WorkspacePlanAction || plan.ToolName != "client.create" {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.ClientName != "Alpha Managed Services" ||
		plan.ClientDisplayID != "ALPHA-100" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerClarifiesMissingClientName(t *testing.T) {
	plan := PlanWorkspaceMessage("Create a client with display ID ALPHA-100.")

	if plan.Kind != WorkspacePlanClarification {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Reply != "What should I call the client?" {
		t.Fatalf("reply=%q", plan.Reply)
	}
}

func TestWorkspacePlannerClarifiesMissingClientDisplayID(t *testing.T) {
	plan := PlanWorkspaceMessage("Create a client named Alpha Managed Services.")

	if plan.Kind != WorkspacePlanClarification {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Reply != "What display ID should I use for the client?" {
		t.Fatalf("reply=%q", plan.Reply)
	}
}

func TestWorkspacePlannerExtractsOnlySuppliedProjectFields(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Create a project titled Onboarding for Northwind Legal with the three setup tasks, A, B, C with tag ids tag-1.",
	)

	if plan.Kind != WorkspacePlanAction || plan.ToolName != "project.create" {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.ProjectName != "Onboarding" || plan.ClientName != "Northwind Legal" {
		t.Fatalf("plan=%+v", plan)
	}
	if !reflect.DeepEqual(plan.Tasks, []string{"A", "B", "C"}) || !reflect.DeepEqual(plan.TagIDs, []string{"tag-1"}) {
		t.Fatalf("tasks=%v", plan.Tasks)
	}
}

func TestWorkspacePlannerClarifiesMissingProjectData(t *testing.T) {
	plan := PlanWorkspaceMessage("Create a project titled Onboarding for Northwind Legal")

	if plan.Kind != WorkspacePlanClarification {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Reply != "Which tasks should I add to the project?" {
		t.Fatalf("reply=%q", plan.Reply)
	}
}

func TestWorkspacePlannerExtractsOnlySuppliedTaskFields(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Add a task titled Verify backup recovery to project Northwind Modernization for Northwind Legal with tag ids tag-1.",
	)

	if plan.Kind != WorkspacePlanAction || plan.ToolName != "task.create" {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.TaskTitle != "Verify backup recovery" ||
		plan.ProjectRef != "Northwind Modernization" ||
		plan.ClientName != "Northwind Legal" || !reflect.DeepEqual(plan.TagIDs, []string{"tag-1"}) {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerClarifiesMissingTaskClient(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Add a task titled Verify backup recovery to project Northwind Modernization.",
	)

	if plan.Kind != WorkspacePlanClarification {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Reply != "Which client is this task for?" {
		t.Fatalf("reply=%q", plan.Reply)
	}
}

func TestWorkspacePlannerClarifiesMissingTaskProject(t *testing.T) {
	plan := PlanWorkspaceMessage("Add a task titled Verify backup recovery")

	if plan.Kind != WorkspacePlanClarification {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Reply != "Which project should I add the task to?" {
		t.Fatalf("reply=%q", plan.Reply)
	}
}

func TestWorkspacePlannerLeavesOrdinaryQuestionsOnHelpPath(t *testing.T) {
	plan := PlanWorkspaceMessage("How do queues work?")

	if plan.Kind != WorkspacePlanHelp {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerBuildsClosedOperationalReads(t *testing.T) {
	for _, test := range []struct {
		message, tool, client, reference, query string
		limit                                   int
	}{
		{"Get ticket INC-200 for Northwind Legal.", "ticket.get", "Northwind Legal", "INC-200", "", 0},
		{"Search tickets for Northwind Legal matching VPN outage.", "ticket.search", "Northwind Legal", "", "VPN outage", 20},
		{"List projects for Northwind Legal.", "project.search", "Northwind Legal", "", "", 25},
		{"Get project PRJ-200 for Northwind Legal.", "project.get", "Northwind Legal", "PRJ-200", "", 0},
		{"Search knowledge for Northwind Legal matching VPN recovery.", "knowledge.search", "Northwind Legal", "", "VPN recovery", 25},
		{"Get knowledge article KB-200 for Northwind Legal.", "knowledge.get", "Northwind Legal", "KB-200", "", 0},
		{"List prospects.", "prospect.list", "", "", "", 25},
	} {
		plan := PlanWorkspaceMessage(test.message)
		if plan.Kind != WorkspacePlanAction || plan.ToolName != test.tool ||
			plan.ClientName != test.client || plan.Reference != test.reference ||
			plan.Query != test.query || plan.Limit != test.limit {
			t.Fatalf("message=%q plan=%+v", test.message, plan)
		}
	}
}

func TestWorkspacePlannerBuildsClosedExistingTicketWrites(t *testing.T) {
	for _, test := range []struct {
		message, tool, value, body, reason string
	}{
		{
			"Transition ticket INC-200 for Northwind Legal to resolved at version 5 because Issue fixed.",
			"ticket.transition", "resolved", "", "Issue fixed",
		},
		{
			"Change priority of ticket INC-200 for Northwind Legal to high at version 5 because Customer impact.",
			"ticket.priority", "high", "", "Customer impact",
		},
		{
			"Add internal note to ticket INC-200 for Northwind Legal at version 5 with body Technician-only detail.",
			"ticket.note", "", "Technician-only detail", "",
		},
		{
			"Add client-visible reply to ticket INC-200 for Northwind Legal at version 5 with body We are investigating.",
			"ticket.reply", "", "We are investigating", "",
		},
	} {
		plan := PlanWorkspaceMessage(test.message)
		if plan.Kind != WorkspacePlanAction || plan.ToolName != test.tool ||
			plan.ClientName != "Northwind Legal" || plan.TicketRef != "INC-200" ||
			plan.ExpectedVersion != 5 || plan.Value != test.value ||
			plan.Body != test.body || plan.Reason != test.reason {
			t.Fatalf("message=%q plan=%+v", test.message, plan)
		}
	}
}

func TestWorkspacePlannerClarifiesMissingOperationalReferencesAndValues(t *testing.T) {
	for _, test := range []struct {
		message, reply string
	}{
		{"Get project for Northwind Legal.", "Which exact Project should I get?"},
		{"Get knowledge article for Northwind Legal.", "Which exact knowledge Article should I get?"},
		{"Transition ticket INC-200 for Northwind Legal to resolved.", "What exact Ticket version and reason should I use?"},
		{"Add client-visible reply to ticket INC-200 for Northwind Legal at version 5.", "What exact client-visible reply should I add?"},
	} {
		plan := PlanWorkspaceMessage(test.message)
		if plan.Kind != WorkspacePlanClarification || plan.Reply != test.reply {
			t.Fatalf("message=%q plan=%+v", test.message, plan)
		}
	}
}

func TestWorkspacePlannerExtractsOnlySuppliedClientResourceFields(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Create an asset named Firewall with display ID AST-1 for Northwind Legal of type firewall.",
	)

	if plan.Kind != WorkspacePlanAction || plan.ToolName != "asset.create" ||
		plan.ClientName != "Northwind Legal" ||
		plan.ClientResourceName != "Firewall" ||
		plan.ClientResourceDisplayID != "AST-1" ||
		plan.AssetType != "firewall" {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.LocationRef != "" || plan.SourceSystem != "" || plan.ExternalID != "" {
		t.Fatalf("planner invented optional resource fields: %+v", plan)
	}
}

func TestWorkspacePlannerClarifiesMissingClientResourceRequiredClauses(t *testing.T) {
	plan := PlanWorkspaceMessage("Create a contract named Support Agreement for Northwind Legal.")

	if plan.Kind != WorkspacePlanClarification || plan.Reply != "What display ID should I use for the contract?" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerRequiresAClosedResourceKindForList(t *testing.T) {
	plan := PlanWorkspaceMessage("List client resources for Northwind Legal.")
	if plan.Kind != WorkspacePlanClarification || plan.Reply != "Which resource kind should I list: locations, contacts, assets, services, or contracts?" {
		t.Fatalf("plan=%+v", plan)
	}
	plan = PlanWorkspaceMessage("List assets for Northwind Legal.")
	if plan.Kind != WorkspacePlanAction || plan.ToolName != "client_resource.list" || plan.ClientResourceKind != "asset" || plan.ClientName != "Northwind Legal" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerBuildsClosedClientResourceLifecycleActions(t *testing.T) {
	for _, test := range []struct {
		message, tool string
	}{
		{"Deactivate location LOC-1 for Northwind Legal because office closed.", "location.deactivate"},
		{"Reactivate service SVC-1 for Northwind Legal because agreement renewed.", "service.reactivate"},
	} {
		plan := PlanWorkspaceMessage(test.message)
		if plan.Kind != WorkspacePlanAction || plan.ToolName != test.tool || plan.ClientName != "Northwind Legal" ||
			plan.ClientResourceDisplayID == "" || plan.Reason == "" {
			t.Fatalf("message=%q plan=%+v", test.message, plan)
		}
	}
}

func TestWorkspacePlannerBuildsExactClientResourceUpdatePatch(t *testing.T) {
	plan := PlanWorkspaceMessage("Update contact CON-1 for Northwind Legal set email to ada@example.com because corrected email.")
	if plan.Kind != WorkspacePlanAction || plan.ToolName != "contact.update" || plan.ClientName != "Northwind Legal" ||
		plan.ClientResourceDisplayID != "CON-1" || plan.Email != "ada@example.com" || plan.Reason != "corrected email" {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Phone != "" || plan.LocationRef != "" || plan.ClientResourceName != "" {
		t.Fatalf("planner invented patch fields: %+v", plan)
	}
}

func TestWorkspacePlannerClarifiesMissingClientResourceLifecycleReason(t *testing.T) {
	plan := PlanWorkspaceMessage("Deactivate asset AST-1 for Northwind Legal.")
	if plan.Kind != WorkspacePlanClarification || plan.Reply != "Why should I deactivate this asset?" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerExtractsOnlySuppliedKnowledgeDraftFields(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Create a knowledge draft with display ID KB-200 titled VPN recovery for Northwind Legal with body Exact recovery steps.",
	)
	if plan.Kind != WorkspacePlanAction || plan.ToolName != "knowledge.draft.create" ||
		plan.ClientName != "Northwind Legal" || plan.ArticleDisplayID != "KB-200" ||
		plan.ArticleTitle != "VPN recovery" || plan.ArticleBody != "Exact recovery steps" {
		t.Fatalf("plan=%+v", plan)
	}

	plan = PlanWorkspaceMessage(
		"Revise knowledge article KB-200 for Northwind Legal at version 3 titled VPN recovery revised with body Exact revised steps.",
	)
	if plan.Kind != WorkspacePlanAction || plan.ToolName != "knowledge.draft.revise" ||
		plan.ArticleRef != "KB-200" || plan.ExpectedVersion != 3 ||
		plan.ArticleTitle != "VPN recovery revised" || plan.ArticleBody != "Exact revised steps" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerProspectCreateDoesNotInferContactData(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Create a prospect named Alpha Managed Services with display ID PRO-200.",
	)
	if plan.Kind != WorkspacePlanAction || plan.ToolName != "prospect.create" ||
		plan.ProspectName != "Alpha Managed Services" || plan.ProspectDisplayID != "PRO-200" {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Email != "" || plan.Phone != "" {
		t.Fatalf("planner inferred prospect contact data: %+v", plan)
	}

	plan = PlanWorkspaceMessage(
		"Create a prospect named Beta Services with display ID PRO-201 with email hello@example.com and phone 555-0100.",
	)
	if plan.Email != "hello@example.com" || plan.Phone != "555-0100" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerTicketRouteRequiresExactVersionAndReason(t *testing.T) {
	plan := PlanWorkspaceMessage(
		"Route ticket INC-200 for Northwind Legal to queue Service Desk at version 5 because Escalate specialist issue.",
	)
	if plan.Kind != WorkspacePlanAction || plan.ToolName != "ticket.route" ||
		plan.TicketRef != "INC-200" || plan.ClientName != "Northwind Legal" ||
		plan.QueueRef != "Service Desk" || plan.ExpectedVersion != 5 ||
		plan.Reason != "Escalate specialist issue" {
		t.Fatalf("plan=%+v", plan)
	}
	plan = PlanWorkspaceMessage("Route ticket INC-200 for Northwind Legal to queue Service Desk.")
	if plan.Kind != WorkspacePlanClarification ||
		plan.Reply != "What exact Ticket version and routing reason should I use?" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestWorkspacePlannerBuildsClosedSecondWaveActionsFromOnlySuppliedValues(t *testing.T) {
	for _, test := range []struct {
		message string
		want    WorkspaceMessagePlan
	}{
		{
			message: "Create ticket INC-2042 for Northwind Legal of type incident titled VPN outage with description Users cannot connect at status new with priority high using service Managed Network and contract Support Agreement.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "ticket.create",
				ClientName: "Northwind Legal", TicketDisplayID: "INC-2042",
				TicketType: "incident", TicketTitle: "VPN outage",
				TicketDescription: "Users cannot connect", TicketStatus: "new",
				TicketPriority: "high", ServiceRef: "Managed Network",
				ContractRef: "Support Agreement",
			},
		},
		{
			message: "Assign ticket INC-2042 for Northwind Legal to alex@example.test at version 4 because network escalation owns it.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "ticket.assign",
				ClientName: "Northwind Legal", TicketRef: "INC-2042",
				TechnicianRef: "alex@example.test", ExpectedVersion: 4,
				Reason: "network escalation owns it",
			},
		},
		{
			message: "List opportunities for Northwind Legal.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "opportunity.list",
				ClientName: "Northwind Legal",
			},
		},
		{
			message: "Get opportunity OPP-2042 for Northwind Legal.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "opportunity.get",
				ClientName: "Northwind Legal", OpportunityRef: "OPP-2042",
			},
		},
		{
			message: "Transition opportunity OPP-2042 for Northwind Legal to stage Qualified at version 3 because Discovery completed.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "opportunity.transition",
				ClientName: "Northwind Legal", OpportunityRef: "OPP-2042",
				StageRef: "Qualified", ExpectedVersion: 3,
				Reason: "Discovery completed",
			},
		},
		{
			message: "Add call activity to opportunity OPP-2042 for Northwind Legal with summary Reviewed onboarding scope and details Customer confirmed the supplied milestones.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "opportunity.activity.create",
				ClientName: "Northwind Legal", OpportunityRef: "OPP-2042",
				ActivityKind: "call", ActivitySummary: "Reviewed onboarding scope",
				ActivityDetails: "Customer confirmed the supplied milestones",
			},
		},
		{
			message: "List proposals for Northwind Legal.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "proposal.list",
				ClientName: "Northwind Legal",
			},
		},
		{
			message: "Get proposal PROP-2042 for Northwind Legal.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "proposal.get",
				ClientName: "Northwind Legal", ProposalRef: "PROP-2042",
			},
		},
		{
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "proposal.create",
				ClientName: "Northwind Legal", OpportunityRef: "OPP-2042",
				ProposalDisplayID: "PROP-2042",
			},
		},
		{
			message: "Publish knowledge article KB-2042 for Northwind Legal at version 4 because reviewed for internal use.",
			want: WorkspaceMessagePlan{
				Kind: WorkspacePlanAction, ToolName: "knowledge.publish",
				ClientName: "Northwind Legal", ArticleRef: "KB-2042",
				ExpectedVersion: 4, Reason: "reviewed for internal use",
			},
		},
	} {
		t.Run(test.want.ToolName, func(t *testing.T) {
			if plan := PlanWorkspaceMessage(test.message); !reflect.DeepEqual(plan, test.want) {
				t.Fatalf("message=%q plan=%+v want=%+v", test.message, plan, test.want)
			}
		})
	}
}

func TestWorkspacePlannerClarifiesEveryIncompleteSecondWaveAction(t *testing.T) {
	for _, test := range []struct {
		name, message, reply string
	}{
		{"ticket create", "Create ticket INC-2042 for Northwind Legal.", "What type, title, description, initial status, and priority should I use for the Ticket?"},
		{"ticket assign", "Assign ticket INC-2042 for Northwind Legal to alex@example.test.", "What exact Ticket version and assignment reason should I use?"},
		{"opportunity list", "List opportunities.", "Which Client should I list Opportunities for?"},
		{"opportunity get", "Get opportunity OPP-2042.", "Which Client is this Opportunity for?"},
		{"opportunity transition", "Transition opportunity OPP-2042 for Northwind Legal to stage Qualified.", "What exact Opportunity version and transition reason should I use?"},
		{"opportunity activity", "Add call activity to opportunity OPP-2042 for Northwind Legal with summary Reviewed onboarding scope.", "What exact activity details should I add?"},
		{"proposal list", "List proposals.", "Which Client should I list Proposals for?"},
		{"proposal get", "Get proposal PROP-2042.", "Which Client is this Proposal for?"},
		{"proposal create", "Create proposal PROP-2042 for Northwind Legal.", "Which Opportunity should the draft Proposal use?"},
		{"knowledge publish", "Publish knowledge article KB-2042 for Northwind Legal.", "What exact Article version and publication reason should I use?"},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanClarification || plan.Reply != test.reply ||
				plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v", test.message, plan)
			}
		})
	}
}

func TestWorkspacePlannerKeepsHighImpactActionsOnHelpPath(t *testing.T) {
	for _, message := range []string{
		"Issue proposal PROP-2042 for Northwind Legal.",
		"Approve proposal PROP-2042 for Northwind Legal.",
		"Accept proposal PROP-2042 for Northwind Legal.",
		"Convert opportunity OPP-2042 for Northwind Legal.",
		"Configure the integration for Northwind Legal.",
		"Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 with a price of $5000.",
	} {
		if plan := PlanWorkspaceMessage(message); plan.Kind != WorkspacePlanHelp ||
			plan.ToolName != "" {
			t.Fatalf("high-impact message=%q plan=%+v", message, plan)
		}
	}
}

func TestWorkspacePlannerRejectsMixedSafeAndHighImpactRequests(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{
			name:    "trailing proposal issue",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and issue it.",
		},
		{
			name:    "embedded proposal version issue",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 after you issue a version.",
		},
		{
			name:    "trailing proposal issue without object",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and issue immediately.",
		},
		{
			name:    "embedded proposal approval",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 after you approve the terms.",
		},
		{
			name:    "trailing proposal acceptance",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and accept it for the customer.",
		},
		{
			name:    "trailing opportunity conversion",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and convert the opportunity.",
		},
		{
			name:    "embedded pricing",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and set the price to 5000.",
		},
		{
			name:    "embedded financial terms",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and add financial terms.",
		},
		{
			name:    "trailing configuration",
			message: "Create ticket INC-2042 for Northwind Legal of type incident titled VPN outage with description Users cannot connect at status new with priority high using contract Support Agreement and configure routing.",
		},
		{
			name:    "embedded integration change",
			message: "Create ticket INC-2042 for Northwind Legal of type incident titled VPN outage with description Users cannot connect at status new with priority high using contract Support Agreement and change the integration settings.",
		},
		{
			name:    "embedded credentials",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 and store credentials for confirmation.",
		},
		{
			name:    "embedded automation execution",
			message: "Add call activity to opportunity OPP-2042 for Northwind Legal with summary Reviewed scope and details Publish automation and replay failures.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", test.message, plan)
			}
		})
	}
}

func TestWorkspacePlannerRejectsTrailingProposalIssueAcrossPunctuation(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{
			name:    "semicolon",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042; issue immediately.",
		},
		{
			name:    "comma",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042, issue immediately.",
		},
		{
			name:    "dash",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 — issue immediately.",
		},
		{
			name:    "new sentence",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042. Issue immediately.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", test.message, plan)
			}
		})
	}
}

func TestWorkspacePlannerRejectsTrailingProposalIssueAcrossGeneralBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{
			name:    "colon",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042: issue immediately.",
		},
		{
			name:    "exclamation",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042! issue immediately.",
		},
		{
			name:    "question mark",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042? issue immediately.",
		},
		{
			name:    "slash",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 / issue immediately.",
		},
		{
			name:    "closing parenthesis",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042) issue immediately.",
		},
		{
			name:    "tab",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042\tissue immediately.",
		},
		{
			name:    "newline",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042\nissue immediately.",
		},
		{
			name:    "multiple whitespace",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042   issue immediately.",
		},
		{
			name:    "mixed whitespace",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042:\t \n  issue immediately.",
		},
		{
			name:    "fullwidth colon",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042： issue immediately.",
		},
		{
			name:    "ideographic full stop",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042。issue immediately.",
		},
		{
			name:    "arabic semicolon",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042؛ issue immediately.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", test.message, plan)
			}
		})
	}
}

func TestWorkspacePlannerHighImpactGuardMatchesTokensNotSubstrings(t *testing.T) {
	for _, test := range []struct {
		message, opportunity, client string
	}{
		{
			message:     "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-CONVERTIBLE.",
			opportunity: "OPP-CONVERTIBLE",
			client:      "Northwind Legal",
		},
		{
			message:     "Create proposal PROP-2042 for Financially Fit Legal from opportunity OPP-2042.",
			opportunity: "OPP-2042",
			client:      "Financially Fit Legal",
		},
		{
			message:     "Create proposal PROP-2042 for Reissue Networks from opportunity OPP-CONFIGURATIONLESS.",
			opportunity: "OPP-CONFIGURATIONLESS",
			client:      "Reissue Networks",
		},
	} {
		plan := PlanWorkspaceMessage(test.message)
		if plan.Kind != WorkspacePlanAction || plan.ToolName != "proposal.create" ||
			plan.OpportunityRef != test.opportunity || plan.ClientName != test.client {
			t.Fatalf("message=%q plan=%+v", test.message, plan)
		}
	}
}

func TestWorkspacePlannerHighImpactGuardAllowsOrdinaryIssueNoun(t *testing.T) {
	for _, test := range []struct {
		name, message, tool, client, title, description string
	}{
		{
			name:        "ticket noun",
			message:     "Create ticket INC-2042 for Northwind Legal of type incident titled VPN issue with description Customer reported an issue at status new with priority high.",
			tool:        "ticket.create",
			client:      "Northwind Legal",
			title:       "VPN issue",
			description: "Customer reported an issue",
		},
		{
			name:    "ticket business name",
			message: "Create ticket INC-2042 for Issue Management LLC of type incident titled VPN outage with description Customer cannot connect at status new with priority high.",
			tool:    "ticket.create",
			client:  "Issue Management LLC",
		},
		{
			name:    "proposal business name",
			message: "Create proposal PROP-2042 for Issue Management LLC from opportunity OPP-2042.",
			tool:    "proposal.create",
			client:  "Issue Management LLC",
		},
		{
			name:        "ticket issue name in description",
			message:     "Create ticket INC-2042 for Northwind Legal of type incident titled VPN outage with description Issue Management LLC reported the outage at status new with priority high.",
			tool:        "ticket.create",
			client:      "Northwind Legal",
			title:       "VPN outage",
			description: "Issue Management LLC reported the outage",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanAction || plan.ToolName != test.tool ||
				plan.ClientName != test.client ||
				(test.title != "" && plan.TicketTitle != test.title) ||
				(test.description != "" && plan.TicketDescription != test.description) {
				t.Fatalf("message=%q plan=%+v, want %s for %q", test.message, plan, test.tool, test.client)
			}
		})
	}
}

func TestWorkspacePlannerRejectsTrailingProposalIssueAcrossUnicodeSeparators(t *testing.T) {
	for _, test := range []struct {
		name, separator string
	}{
		{name: "line separator", separator: "\u2028"},
		{name: "paragraph separator", separator: "\u2029"},
		{name: "non-breaking space", separator: "\u00a0"},
		{name: "form feed", separator: "\f"},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042" +
				test.separator + "issue immediately."
			plan := PlanWorkspaceMessage(message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", message, plan)
			}
		})
	}
}

func TestWorkspacePlannerRejectsProposalIssueClausesAfterOpportunityDisplayID(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{
			name:    "object pronoun",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue it.",
		},
		{
			name:    "time marker now",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue now.",
		},
		{
			name:    "time marker immediately",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue immediately.",
		},
		{
			name:    "recipient marker",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue to the customer.",
		},
		{
			name:    "terminal after opportunity identifier",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue.",
		},
		{
			name:    "terminal clause after punctuation",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042; issue.",
		},
		{
			name:    "polite arbitrary complement",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042; issue please.",
		},
		{
			name:    "date arbitrary complement",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue today.",
		},
		{
			name:    "purpose arbitrary complement",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue for signature.",
		},
		{
			name:    "definite object arbitrary complement",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue the final version.",
		},
		{
			name:    "unlisted arbitrary complement",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 issue whenever counsel is ready.",
		},
		{
			name:    "unicode separator arbitrary complement",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042\u2028issue please.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", test.message, plan)
			}
		})
	}
}

func TestWorkspacePlannerRejectsHighImpactClausesAfterClosedOpportunityDisplayID(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{
			name:    "issue after paragraph separator",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042\u2029issue however the next reviewer requests.",
		},
		{
			name:    "approve after fullwidth colon",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-CONVERTIBLE：approve using the future checklist.",
		},
		{
			name:    "accept after mathematical symbol",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 ∴ accept once the arbitrary condition is met.",
		},
		{
			name:    "convert after inverted question mark",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity OPP-2042 ¿ convert by whatever process applies.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", test.message, plan)
			}
		})
	}
}

func TestWorkspacePlannerAcceptsQuotedExactOpportunityNamesIncludingIssueNouns(t *testing.T) {
	for _, test := range []struct {
		name, opportunity string
	}{
		{
			name:        "business name after colon",
			opportunity: "Renewal: Issue Management LLC",
		},
		{
			name:        "business name after conjunction",
			opportunity: "Renewal: Research and Issue Management LLC",
		},
		{
			name:        "ordinary issue noun",
			opportunity: "Renewal VPN issue",
		},
		{
			name:        "issue now business name",
			opportunity: "Issue Now Consulting",
		},
		{
			name:        "issue to business name",
			opportunity: "Renewal: Issue to Resolution LLC",
		},
		{
			name:        "terminal issue after conjunction",
			opportunity: "Risk and Issue",
		},
		{
			name:        "terminal issue after colon",
			opportunity: "Renewal: Issue",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := "Create proposal PROP-2042 for Northwind Legal from opportunity \"" +
				test.opportunity + "\"."
			plan := PlanWorkspaceMessage(message)
			if plan.Kind != WorkspacePlanAction ||
				plan.ToolName != "proposal.create" ||
				plan.OpportunityRef != test.opportunity {
				t.Fatalf("message=%q plan=%+v, want proposal.create for opportunity %q",
					message, plan, test.opportunity)
			}
		})
	}
}

func TestWorkspacePlannerClarifiesUnquotedOpportunityNames(t *testing.T) {
	for _, opportunity := range []string{
		"Renewal Expansion",
		"Renewal: Issue Management LLC",
		"Risk and Issue",
	} {
		t.Run(opportunity, func(t *testing.T) {
			message := "Create proposal PROP-2042 for Northwind Legal from opportunity " +
				opportunity + "."
			plan := PlanWorkspaceMessage(message)
			if plan.Kind != WorkspacePlanClarification || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want clarification without action", message, plan)
			}
		})
	}
}

func TestWorkspacePlannerRejectsHighImpactClausesAfterQuotedOpportunity(t *testing.T) {
	for _, test := range []struct {
		name, message string
	}{
		{
			name:    "issue after unicode line separator",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity \"Renewal: Issue Management LLC\"\u2028issue whenever counsel is ready.",
		},
		{
			name:    "approve after ideographic full stop",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity \"Renewal Expansion\"。approve under the policy nobody has named yet.",
		},
		{
			name:    "accept after em dash",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity \"Renewal Expansion\" — accept after the customer sends the blue envelope.",
		},
		{
			name:    "convert after arabic semicolon",
			message: "Create proposal PROP-2042 for Northwind Legal from opportunity \"Renewal Expansion\"؛ convert whenever the lantern turns green.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := PlanWorkspaceMessage(test.message)
			if plan.Kind != WorkspacePlanHelp || plan.ToolName != "" {
				t.Fatalf("message=%q plan=%+v, want help without action", test.message, plan)
			}
		})
	}
}
