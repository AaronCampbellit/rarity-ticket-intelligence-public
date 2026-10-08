package aiassist

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type WorkspacePlanKind string

const (
	WorkspacePlanHelp          WorkspacePlanKind = "help"
	WorkspacePlanClarification WorkspacePlanKind = "clarification"
	WorkspacePlanAction        WorkspacePlanKind = "action"
)

type WorkspaceMessagePlan struct {
	Kind                     WorkspacePlanKind
	Reply                    string
	ToolName                 string
	ProjectName              string
	ProjectRef               string
	ClientName               string
	ClientDisplayID          string
	ClientResourceName       string
	ClientResourceDisplayID  string
	ClientResourceKind       string
	LocationRef              string
	Email                    string
	Phone                    string
	AssetType                string
	SourceSystem             string
	ExternalID               string
	Criticality              string
	StartsOn                 string
	EndsOn                   string
	Reason                   string
	ClientResourcePatchField string
	ClientResourcePatchValue string
	ClientResourcePatchClear bool
	TaskTitle                string
	Tasks                    []string
	ArticleRef               string
	ArticleDisplayID         string
	ArticleTitle             string
	ArticleBody              string
	ProspectName             string
	ProspectDisplayID        string
	TicketRef                string
	TicketDisplayID          string
	TicketType               string
	TicketTitle              string
	TicketDescription        string
	TicketStatus             string
	TicketPriority           string
	ServiceRef               string
	ContractRef              string
	TechnicianRef            string
	OpportunityRef           string
	StageRef                 string
	ActivityKind             string
	ActivitySummary          string
	ActivityDetails          string
	ProposalRef              string
	ProposalDisplayID        string
	QueueRef                 string
	Reference                string
	Query                    string
	Value                    string
	Body                     string
	Limit                    int
	ExpectedVersion          int64
	TagIDs                   []string
}

var projectTaskPrefix = regexp.MustCompile(
	`(?i)^(?:the\s+)?(?:(?:one|two|three|four|five|six|seven|eight|nine|ten|\d+)\s+)?(?:setup\s+)?tasks?\s*[:,]?\s*`,
)

var highImpactWorkspaceToken = regexp.MustCompile(
	`(^|[^\pL\pN])(?:approv(?:e|ed|es|ing|al)|accept(?:ed|s|ing|ance)?|convert(?:ed|s|ing)?|conversion|pric(?:e|ed|es|ing)|financ(?:e|es|ial)|config(?:ure|ured|ures|uring|uration)?|integrat(?:e|ed|es|ing|ion|ions)|credential(?:s)?|secret(?:s)?|token(?:s)?|password(?:s)?|automat(?:e|ed|es|ing|ion|ions)|billing)([^\pL\pN]|$)`,
)

func PlanWorkspaceMessage(message string) WorkspaceMessagePlan {
	text := strings.TrimSpace(message)
	lower := strings.ToLower(text)
	if isHighImpactWorkspaceRequest(text, lower) {
		return WorkspaceMessagePlan{Kind: WorkspacePlanHelp}
	}
	if plan, matched := planSecondWaveMessage(text, lower); matched {
		return plan
	}
	if plan, matched := planOperationalReadMessage(text, lower); matched {
		return plan
	}
	if plan, matched := planExistingTicketWriteMessage(text, lower); matched {
		return plan
	}
	if plan, matched := planOperationalWriteMessage(text, lower); matched {
		return plan
	}
	if plan, matched := planClientResourceMessage(text, lower); matched {
		return plan
	}
	if plan, matched := planClientMessage(text, lower); matched {
		return plan
	}
	if plan, matched := planTaskMessage(text, lower); matched {
		return plan
	}
	const projectPrefix = "create a project"
	const shortProjectPrefix = "create project"
	var rest string
	switch {
	case strings.HasPrefix(lower, projectPrefix):
		rest = strings.TrimSpace(text[len(projectPrefix):])
	case strings.HasPrefix(lower, shortProjectPrefix):
		rest = strings.TrimSpace(text[len(shortProjectPrefix):])
	default:
		return WorkspaceMessagePlan{Kind: WorkspacePlanHelp}
	}

	for _, prefix := range []string{"titled ", "named "} {
		if strings.HasPrefix(strings.ToLower(rest), prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}
	forIndex := indexFold(rest, " for ")
	if forIndex < 0 {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What should I call the project, and which client is it for?",
		}
	}
	projectName := strings.TrimSpace(rest[:forIndex])
	if projectName == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What should I call the project?",
		}
	}

	clientAndTasks := strings.TrimSpace(rest[forIndex+len(" for "):])
	withIndex := indexFold(clientAndTasks, " with ")
	if withIndex < 0 {
		if strings.Trim(clientAndTasks, " .?!") == "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "Which client is this project for?",
			}
		}
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which tasks should I add to the project?",
		}
	}
	clientName := strings.TrimSpace(clientAndTasks[:withIndex])
	if clientName == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which client is this project for?",
		}
	}
	rawTasks := strings.TrimSpace(clientAndTasks[withIndex+len(" with "):])
	rawTasks = projectTaskPrefix.ReplaceAllString(rawTasks, "")
	rawTasks = strings.TrimSpace(strings.TrimRight(rawTasks, ".!?"))
	rawTasks, tagIDs := splitWorkspaceTagIDs(rawTasks)
	tasks := splitProjectTasks(rawTasks)
	if len(tasks) == 0 {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which tasks should I add to the project?",
		}
	}

	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "project.create",
		ProjectName: projectName, ClientName: clientName, Tasks: tasks, TagIDs: tagIDs,
	}
}

func planSecondWaveMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	switch {
	case strings.HasPrefix(lower, "create ticket "):
		return planTicketCreate(text), true
	case strings.HasPrefix(lower, "assign ticket "):
		return planTicketAssign(text), true
	case strings.HasPrefix(lower, "list opportunities"):
		return planOpportunityList(text), true
	case strings.HasPrefix(lower, "get opportunity "):
		return planOpportunityGet(text), true
	case strings.HasPrefix(lower, "transition opportunity "):
		return planOpportunityTransition(text), true
	case strings.HasPrefix(lower, "add ") &&
		strings.Contains(lower, " activity to opportunity "):
		return planOpportunityActivityCreate(text), true
	case strings.HasPrefix(lower, "list proposals"):
		return planProposalList(text), true
	case strings.HasPrefix(lower, "get proposal "):
		return planProposalGet(text), true
	case strings.HasPrefix(lower, "create proposal "):
		return planProposalCreate(text), true
	case strings.HasPrefix(lower, "publish knowledge article "):
		return planKnowledgePublish(text), true
	default:
		return WorkspaceMessagePlan{}, false
	}
}

func planTicketCreate(text string) WorkspaceMessagePlan {
	rest := strings.TrimSpace(text[len("create ticket "):])
	displayID, rest, hasClient := splitPlannerClause(rest, " for ")
	client, rest, hasType := splitPlannerClause(rest, " of type ")
	recordType, rest, hasTitle := splitPlannerClause(rest, " titled ")
	title, rest, hasDescription := splitPlannerClause(rest, " with description ")
	description, rest, hasStatus := splitPlannerClause(rest, " at status ")
	status, rest, hasPriority := splitPlannerClause(rest, " with priority ")
	rest, tagIDs := splitWorkspaceTagIDs(rest)
	priority, service, contract := parseTicketCreateResources(rest)
	if !hasClient || !hasType || !hasTitle || !hasDescription || !hasStatus ||
		!hasPriority || trimPlannerPunctuation(displayID) == "" ||
		trimPlannerPunctuation(client) == "" ||
		trimPlannerPunctuation(recordType) == "" ||
		trimPlannerPunctuation(title) == "" ||
		trimPlannerPunctuation(description) == "" ||
		trimPlannerPunctuation(status) == "" ||
		trimPlannerPunctuation(priority) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What type, title, description, initial status, and priority should I use for the Ticket?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "ticket.create",
		ClientName: trimPlannerPunctuation(client), TicketDisplayID: trimPlannerPunctuation(displayID),
		TicketType: trimPlannerPunctuation(recordType), TicketTitle: trimPlannerPunctuation(title),
		TicketDescription: trimPlannerPunctuation(description), TicketStatus: trimPlannerPunctuation(status),
		TicketPriority: trimPlannerPunctuation(priority), ServiceRef: trimPlannerPunctuation(service),
		ContractRef: trimPlannerPunctuation(contract), TagIDs: tagIDs,
	}
}

func parseTicketCreateResources(text string) (priority, service, contract string) {
	text = strings.TrimSpace(text)
	if value, resources, found := splitPlannerClause(text, " using service "); found {
		service, contract, _ = splitPlannerClause(resources, " and contract ")
		return value, service, contract
	}
	if value, resource, found := splitPlannerClause(text, " using contract "); found {
		return value, "", resource
	}
	return text, "", ""
}

func planTicketAssign(text string) WorkspaceMessagePlan {
	ticket, rest, hasClient := splitPlannerClause(
		strings.TrimSpace(text[len("assign ticket "):]), " for ",
	)
	client, rest, hasTechnician := splitPlannerClause(rest, " to ")
	technician, rest, hasVersion := splitPlannerClause(rest, " at version ")
	versionText, reason, hasReason := splitPlannerClause(rest, " because ")
	version, versionErr := strconv.ParseInt(trimPlannerPunctuation(versionText), 10, 64)
	if !hasClient || !hasTechnician || !hasVersion || !hasReason ||
		trimPlannerPunctuation(ticket) == "" ||
		trimPlannerPunctuation(client) == "" ||
		trimPlannerPunctuation(technician) == "" ||
		versionErr != nil || version < 1 ||
		trimPlannerPunctuation(reason) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What exact Ticket version and assignment reason should I use?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "ticket.assign",
		ClientName: trimPlannerPunctuation(client), TicketRef: trimPlannerPunctuation(ticket),
		TechnicianRef: trimPlannerPunctuation(technician), ExpectedVersion: version,
		Reason: trimPlannerPunctuation(reason),
	}
}

func planOpportunityList(text string) WorkspaceMessagePlan {
	const prefix = "list opportunities"
	rest := strings.TrimSpace(text[len(prefix):])
	if !strings.HasPrefix(strings.ToLower(rest), "for ") {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which Client should I list Opportunities for?",
		}
	}
	client := trimPlannerPunctuation(strings.TrimSpace(rest[len("for "):]))
	if client == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which Client should I list Opportunities for?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "opportunity.list", ClientName: client,
	}
}

func planOpportunityGet(text string) WorkspaceMessagePlan {
	opportunity, client, hasClient := splitPlannerClause(
		strings.TrimSpace(text[len("get opportunity "):]), " for ",
	)
	opportunity, client = trimPlannerPunctuation(opportunity), trimPlannerPunctuation(client)
	if opportunity == "" {
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanClarification, Reply: "Which exact Opportunity should I get?",
		}
	}
	if !hasClient || client == "" {
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanClarification, Reply: "Which Client is this Opportunity for?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "opportunity.get",
		ClientName: client, OpportunityRef: opportunity,
	}
}

func planOpportunityTransition(text string) WorkspaceMessagePlan {
	opportunity, rest, hasClient := splitPlannerClause(
		strings.TrimSpace(text[len("transition opportunity "):]), " for ",
	)
	client, rest, hasStage := splitPlannerClause(rest, " to stage ")
	stage, rest, hasVersion := splitPlannerClause(rest, " at version ")
	versionText, reason, hasReason := splitPlannerClause(rest, " because ")
	version, versionErr := strconv.ParseInt(trimPlannerPunctuation(versionText), 10, 64)
	if !hasClient || !hasStage || !hasVersion || !hasReason ||
		trimPlannerPunctuation(opportunity) == "" ||
		trimPlannerPunctuation(client) == "" ||
		trimPlannerPunctuation(stage) == "" ||
		versionErr != nil || version < 1 ||
		trimPlannerPunctuation(reason) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What exact Opportunity version and transition reason should I use?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "opportunity.transition",
		ClientName: trimPlannerPunctuation(client), OpportunityRef: trimPlannerPunctuation(opportunity),
		StageRef: trimPlannerPunctuation(stage), ExpectedVersion: version,
		Reason: trimPlannerPunctuation(reason),
	}
}

func planOpportunityActivityCreate(text string) WorkspaceMessagePlan {
	kind, rest, hasOpportunity := splitPlannerClause(
		strings.TrimSpace(text[len("add "):]), " activity to opportunity ",
	)
	opportunity, rest, hasClient := splitPlannerClause(rest, " for ")
	client, rest, hasSummary := splitPlannerClause(rest, " with summary ")
	summary, details, hasDetails := splitPlannerClause(rest, " and details ")
	if !hasOpportunity || !hasClient || !hasSummary || !hasDetails ||
		trimPlannerPunctuation(kind) == "" ||
		trimPlannerPunctuation(opportunity) == "" ||
		trimPlannerPunctuation(client) == "" ||
		trimPlannerPunctuation(summary) == "" ||
		trimPlannerPunctuation(details) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What exact activity details should I add?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "opportunity.activity.create",
		ClientName: trimPlannerPunctuation(client), OpportunityRef: trimPlannerPunctuation(opportunity),
		ActivityKind:    strings.ToLower(trimPlannerPunctuation(kind)),
		ActivitySummary: trimPlannerPunctuation(summary), ActivityDetails: trimPlannerPunctuation(details),
	}
}

func planProposalList(text string) WorkspaceMessagePlan {
	const prefix = "list proposals"
	rest := strings.TrimSpace(text[len(prefix):])
	if !strings.HasPrefix(strings.ToLower(rest), "for ") {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which Client should I list Proposals for?",
		}
	}
	client := trimPlannerPunctuation(strings.TrimSpace(rest[len("for "):]))
	if client == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which Client should I list Proposals for?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "proposal.list", ClientName: client,
	}
}

func planProposalGet(text string) WorkspaceMessagePlan {
	proposal, client, hasClient := splitPlannerClause(
		strings.TrimSpace(text[len("get proposal "):]), " for ",
	)
	proposal, client = trimPlannerPunctuation(proposal), trimPlannerPunctuation(client)
	if proposal == "" {
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanClarification, Reply: "Which exact Proposal should I get?",
		}
	}
	if !hasClient || client == "" {
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanClarification, Reply: "Which Client is this Proposal for?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "proposal.get",
		ClientName: client, ProposalRef: proposal,
	}
}

type proposalCreateParts struct {
	proposal       string
	client         string
	opportunity    string
	hasClient      bool
	hasOpportunity bool
}

func parseProposalCreateParts(text string) proposalCreateParts {
	proposal, rest, hasClient := splitPlannerClause(
		strings.TrimSpace(text[len("create proposal "):]), " for ",
	)
	client, opportunity, hasOpportunity := splitPlannerClause(rest, " from opportunity ")
	return proposalCreateParts{
		proposal:       proposal,
		client:         client,
		opportunity:    opportunity,
		hasClient:      hasClient,
		hasOpportunity: hasOpportunity,
	}
}

func (parts proposalCreateParts) complete() bool {
	return parts.hasClient && parts.hasOpportunity &&
		trimPlannerPunctuation(parts.proposal) != "" &&
		trimPlannerPunctuation(parts.client) != "" &&
		trimPlannerPunctuation(parts.opportunity) != ""
}

func planProposalCreate(text string) WorkspaceMessagePlan {
	parts := parseProposalCreateParts(text)
	if !parts.hasClient || trimPlannerPunctuation(parts.proposal) == "" ||
		trimPlannerPunctuation(parts.client) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What Proposal display ID and Client should I use?",
		}
	}
	if !parts.hasOpportunity || trimPlannerPunctuation(parts.opportunity) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which Opportunity should the draft Proposal use?",
		}
	}
	opportunity, trailing, closed := parseProposalOpportunityReference(parts.opportunity)
	if !closed || !plannerSuffixHasOnlySeparators(trailing) {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Use an OPP-* display ID or quote the exact Opportunity name.",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "proposal.create",
		ClientName:        trimPlannerPunctuation(parts.client),
		OpportunityRef:    opportunity,
		ProposalDisplayID: trimPlannerPunctuation(parts.proposal),
	}
}

func planKnowledgePublish(text string) WorkspaceMessagePlan {
	article, rest, hasClient := splitPlannerClause(
		strings.TrimSpace(text[len("publish knowledge article "):]), " for ",
	)
	client, rest, hasVersion := splitPlannerClause(rest, " at version ")
	versionText, reason, hasReason := splitPlannerClause(rest, " because ")
	version, versionErr := strconv.ParseInt(trimPlannerPunctuation(versionText), 10, 64)
	if !hasClient || trimPlannerPunctuation(article) == "" ||
		trimPlannerPunctuation(client) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which exact internal draft Article should I publish, and for which Client?",
		}
	}
	if !hasVersion || !hasReason || versionErr != nil || version < 1 ||
		trimPlannerPunctuation(reason) == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What exact Article version and publication reason should I use?",
		}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "knowledge.publish",
		ClientName: trimPlannerPunctuation(client), ArticleRef: trimPlannerPunctuation(article),
		ExpectedVersion: version, Reason: trimPlannerPunctuation(reason),
	}
}

func isHighImpactWorkspaceRequest(text, lower string) bool {
	return highImpactWorkspaceToken.MatchString(lower) ||
		hasStructuredHighImpactWorkspaceClause(text)
}

type workspaceClauseToken struct {
	value string
}

func hasStructuredHighImpactWorkspaceClause(text string) bool {
	tokens := scanWorkspaceClauseTokens(text)
	for index, token := range tokens {
		if token.value == "line" && index+1 < len(tokens) &&
			(tokens[index+1].value == "item" || tokens[index+1].value == "items") {
			return true
		}
	}
	lower := strings.ToLower(text)
	if !strings.HasPrefix(lower, "create proposal ") {
		return false
	}
	parts := parseProposalCreateParts(text)
	return parts.complete() &&
		opportunityReferenceHasIssueClause(parts.opportunity)
}

func scanWorkspaceClauseTokens(text string) []workspaceClauseToken {
	tokens := make([]workspaceClauseToken, 0, 16)
	var word strings.Builder
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		tokens = append(tokens, workspaceClauseToken{
			value: strings.ToLower(word.String()),
		})
		word.Reset()
	}
	for _, current := range text {
		if unicode.IsLetter(current) || unicode.IsNumber(current) ||
			unicode.IsMark(current) {
			word.WriteRune(current)
			continue
		}
		flushWord()
	}
	flushWord()
	return tokens
}

func opportunityReferenceHasIssueClause(reference string) bool {
	_, trailing, closed := parseProposalOpportunityReference(reference)
	if !closed {
		return false
	}
	for _, token := range scanWorkspaceClauseTokens(trailing) {
		if isIssueToken(token.value) {
			return true
		}
	}
	return false
}

func parseProposalOpportunityReference(reference string) (string, string, bool) {
	reference = strings.TrimSpace(reference)
	if value, trailing, ok := splitQuotedOpportunityName(reference, `"`, `"`); ok {
		return value, trailing, true
	}
	return splitOpportunityDisplayID(reference)
}

func splitQuotedOpportunityName(reference, opening, closing string) (string, string, bool) {
	if !strings.HasPrefix(reference, opening) {
		return "", "", false
	}
	remainder := reference[len(opening):]
	closingIndex := strings.Index(remainder, closing)
	if closingIndex < 0 {
		return "", "", false
	}
	value := strings.TrimSpace(remainder[:closingIndex])
	if value == "" {
		return "", "", false
	}
	return value, remainder[closingIndex+len(closing):], true
}

func splitOpportunityDisplayID(reference string) (string, string, bool) {
	const prefix = "opp-"
	if len(reference) < len(prefix) ||
		!strings.EqualFold(reference[:len(prefix)], prefix) {
		return "", "", false
	}
	index := len(prefix)
	valueStart := index
	for index < len(reference) {
		current, size := utf8.DecodeRuneInString(reference[index:])
		if !unicode.IsLetter(current) && !unicode.IsNumber(current) &&
			!unicode.IsMark(current) && current != '-' && current != '_' {
			break
		}
		index += size
	}
	if index == valueStart {
		return "", "", false
	}
	return reference[:index], reference[index:], true
}

func plannerSuffixHasOnlySeparators(suffix string) bool {
	for _, current := range suffix {
		if unicode.IsLetter(current) || unicode.IsNumber(current) ||
			unicode.IsMark(current) {
			return false
		}
	}
	return true
}

func isIssueToken(value string) bool {
	return value == "issue" || value == "issued" || value == "issuing"
}

func planOperationalReadMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	for _, read := range []struct {
		prefix, tool, referenceLabel string
	}{
		{"get ticket ", "ticket.get", "Ticket"},
		{"get project ", "project.get", "Project"},
		{"get knowledge article ", "knowledge.get", "knowledge Article"},
	} {
		if !strings.HasPrefix(lower, read.prefix) {
			continue
		}
		reference, client, found := splitPlannerClause(
			strings.TrimSpace(text[len(read.prefix):]), " for ",
		)
		reference, client = trimPlannerPunctuation(reference), trimPlannerPunctuation(client)
		if !found || reference == "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "Which exact " + read.referenceLabel + " should I get?",
			}, true
		}
		if client == "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "Which Client is this " + read.referenceLabel + " for?",
			}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: read.tool,
			ClientName: client, Reference: reference, TicketRef: reference,
		}, true
	}

	if strings.HasPrefix(lower, "search tickets for ") {
		client, query, found := splitPlannerClause(
			strings.TrimSpace(text[len("search tickets for "):]), " matching ",
		)
		client, query = trimPlannerPunctuation(client), trimPlannerPunctuation(query)
		if client == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which Client should I search for Tickets?"}, true
		}
		if !found || query == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact Ticket search should I run?"}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: "ticket.search",
			ClientName: client, Query: query, Limit: 20,
		}, true
	}

	if strings.HasPrefix(lower, "list projects for ") {
		client := trimPlannerPunctuation(text[len("list projects for "):])
		if client == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which Client should I list Projects for?"}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: "project.search",
			ClientName: client, Limit: 25,
		}, true
	}

	if strings.HasPrefix(lower, "search knowledge for ") {
		client, query, found := splitPlannerClause(
			strings.TrimSpace(text[len("search knowledge for "):]), " matching ",
		)
		client, query = trimPlannerPunctuation(client), trimPlannerPunctuation(query)
		if client == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which Client should I search Knowledge for?"}, true
		}
		if !found || query == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact Knowledge search should I run?"}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: "knowledge.search",
			ClientName: client, Query: query, Limit: 25,
		}, true
	}

	if strings.HasPrefix(lower, "list prospects") {
		if trimPlannerPunctuation(text[len("list prospects"):]) != "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "Prospect listing is MSP-global; remove the Client reference.",
			}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: "prospect.list", Limit: 25,
		}, true
	}
	return WorkspaceMessagePlan{}, false
}

func planExistingTicketWriteMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	for _, action := range []struct {
		prefix, tool string
	}{
		{"transition ticket ", "ticket.transition"},
		{"change priority of ticket ", "ticket.priority"},
	} {
		if !strings.HasPrefix(lower, action.prefix) {
			continue
		}
		ticket, rest, hasClient := splitPlannerClause(
			strings.TrimSpace(text[len(action.prefix):]), " for ",
		)
		client, rest, hasValue := splitPlannerClause(rest, " to ")
		value, rest, hasVersion := splitPlannerClause(rest, " at version ")
		versionText, reason, hasReason := splitPlannerClause(rest, " because ")
		version, versionErr := strconv.ParseInt(trimPlannerPunctuation(versionText), 10, 64)
		if !hasClient || !hasValue || !hasVersion || !hasReason ||
			trimPlannerPunctuation(ticket) == "" ||
			trimPlannerPunctuation(client) == "" ||
			trimPlannerPunctuation(value) == "" ||
			versionErr != nil || version < 1 ||
			trimPlannerPunctuation(reason) == "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "What exact Ticket version and reason should I use?",
			}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: action.tool,
			ClientName:      trimPlannerPunctuation(client),
			TicketRef:       trimPlannerPunctuation(ticket),
			Value:           trimPlannerPunctuation(value),
			Reason:          trimPlannerPunctuation(reason),
			ExpectedVersion: version,
		}, true
	}

	for _, comment := range []struct {
		prefix, tool, label string
	}{
		{"add internal note to ticket ", "ticket.note", "internal note"},
		{"add client-visible reply to ticket ", "ticket.reply", "client-visible reply"},
	} {
		if !strings.HasPrefix(lower, comment.prefix) {
			continue
		}
		ticket, rest, hasClient := splitPlannerClause(
			strings.TrimSpace(text[len(comment.prefix):]), " for ",
		)
		client, rest, hasVersion := splitPlannerClause(rest, " at version ")
		versionText, body, hasBody := splitPlannerClause(rest, " with body ")
		version, versionErr := strconv.ParseInt(trimPlannerPunctuation(versionText), 10, 64)
		if !hasClient || !hasVersion ||
			trimPlannerPunctuation(ticket) == "" ||
			trimPlannerPunctuation(client) == "" ||
			versionErr != nil || version < 1 {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "Which exact Ticket and version should receive the " + comment.label + "?",
			}, true
		}
		if !hasBody || trimPlannerPunctuation(body) == "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "What exact " + comment.label + " should I add?",
			}, true
		}
		return WorkspaceMessagePlan{
			Kind: WorkspacePlanAction, ToolName: comment.tool,
			ClientName:      trimPlannerPunctuation(client),
			TicketRef:       trimPlannerPunctuation(ticket),
			Body:            trimPlannerPunctuation(body),
			ExpectedVersion: version,
		}, true
	}
	return WorkspaceMessagePlan{}, false
}

func planOperationalWriteMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	if strings.HasPrefix(lower, "create a knowledge draft") ||
		strings.HasPrefix(lower, "create knowledge draft") {
		return planKnowledgeDraftCreate(text), true
	}
	if strings.HasPrefix(lower, "revise knowledge article ") ||
		strings.HasPrefix(lower, "revise knowledge draft ") {
		return planKnowledgeDraftRevise(text), true
	}
	if strings.HasPrefix(lower, "create a prospect") ||
		strings.HasPrefix(lower, "create prospect") {
		return planProspectCreate(text), true
	}
	if strings.HasPrefix(lower, "route ticket ") {
		return planTicketRoute(text), true
	}
	return WorkspaceMessagePlan{}, false
}

func planKnowledgeDraftCreate(text string) WorkspaceMessagePlan {
	rest := strings.TrimSpace(text)
	for _, prefix := range []string{"create a knowledge draft", "create knowledge draft"} {
		if strings.HasPrefix(strings.ToLower(rest), prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}
	const displayPrefix = "with display id "
	if !strings.HasPrefix(strings.ToLower(rest), displayPrefix) {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What display ID should I use for the knowledge draft?"}
	}
	rest = strings.TrimSpace(rest[len(displayPrefix):])
	displayID, rest, found := splitPlannerClause(rest, " titled ")
	if !found || trimPlannerPunctuation(displayID) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What should I title the knowledge draft?"}
	}
	title, rest, found := splitPlannerClause(rest, " for ")
	if !found || trimPlannerPunctuation(title) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which client is this knowledge draft for?"}
	}
	client, body, found := splitPlannerClause(rest, " with body ")
	if !found || trimPlannerPunctuation(client) == "" || trimPlannerPunctuation(body) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact body should the knowledge draft contain?"}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "knowledge.draft.create",
		ClientName: trimPlannerPunctuation(client), ArticleDisplayID: trimPlannerPunctuation(displayID),
		ArticleTitle: trimPlannerPunctuation(title), ArticleBody: trimPlannerPunctuation(body),
	}
}

func planKnowledgeDraftRevise(text string) WorkspaceMessagePlan {
	rest := strings.TrimSpace(text)
	for _, prefix := range []string{"revise knowledge article ", "revise knowledge draft "} {
		if strings.HasPrefix(strings.ToLower(rest), prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}
	article, rest, found := splitPlannerClause(rest, " for ")
	if !found || trimPlannerPunctuation(article) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which exact knowledge article should I revise?"}
	}
	client, rest, found := splitPlannerClause(rest, " at version ")
	if !found || trimPlannerPunctuation(client) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact Article version should I revise?"}
	}
	versionText, rest, found := splitPlannerClause(rest, " titled ")
	version, versionErr := strconv.ParseInt(strings.TrimSpace(versionText), 10, 64)
	if !found || versionErr != nil || version < 1 {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact Article version should I revise?"}
	}
	title, body, found := splitPlannerClause(rest, " with body ")
	if !found || trimPlannerPunctuation(title) == "" || trimPlannerPunctuation(body) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact title and body should the revised draft contain?"}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "knowledge.draft.revise",
		ClientName: trimPlannerPunctuation(client), ArticleRef: trimPlannerPunctuation(article),
		ExpectedVersion: version, ArticleTitle: trimPlannerPunctuation(title),
		ArticleBody: trimPlannerPunctuation(body),
	}
}

func planProspectCreate(text string) WorkspaceMessagePlan {
	rest := strings.TrimSpace(text)
	for _, prefix := range []string{"create a prospect", "create prospect"} {
		if strings.HasPrefix(strings.ToLower(rest), prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}
	const namedPrefix = "named "
	if !strings.HasPrefix(strings.ToLower(rest), namedPrefix) {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What should I call the prospect?"}
	}
	name, rest, found := splitPlannerClause(strings.TrimSpace(rest[len(namedPrefix):]), " with display id ")
	if !found || trimPlannerPunctuation(name) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What display ID should I use for the prospect?"}
	}
	displayID, email, phone := rest, "", ""
	if before, after, hasEmail := splitPlannerClause(rest, " with email "); hasEmail {
		displayID, email = before, after
		if emailPart, phonePart, hasPhone := splitPlannerClause(email, " and phone "); hasPhone {
			email, phone = emailPart, phonePart
		}
	} else if before, after, hasPhone := splitPlannerClause(rest, " with phone "); hasPhone {
		displayID, phone = before, after
	}
	displayID = trimPlannerPunctuation(displayID)
	if displayID == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What display ID should I use for the prospect?"}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "prospect.create",
		ProspectName: trimPlannerPunctuation(name), ProspectDisplayID: displayID,
		Email: trimPlannerPunctuation(email), Phone: trimPlannerPunctuation(phone),
	}
}

func planTicketRoute(text string) WorkspaceMessagePlan {
	rest := strings.TrimSpace(text[len("route ticket "):])
	ticket, rest, found := splitPlannerClause(rest, " for ")
	if !found || trimPlannerPunctuation(ticket) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which Client and Ticket should I route?"}
	}
	client, rest, found := splitPlannerClause(rest, " to queue ")
	if !found || trimPlannerPunctuation(client) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which exact Queue should receive the Ticket?"}
	}
	queue, rest, hasVersion := splitPlannerClause(rest, " at version ")
	if !hasVersion {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact Ticket version and routing reason should I use?"}
	}
	versionText, reason, hasReason := splitPlannerClause(rest, " because ")
	version, versionErr := strconv.ParseInt(strings.TrimSpace(versionText), 10, 64)
	if !hasReason || versionErr != nil || version < 1 || trimPlannerPunctuation(reason) == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact Ticket version and routing reason should I use?"}
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "ticket.route",
		TicketRef: trimPlannerPunctuation(ticket), ClientName: trimPlannerPunctuation(client),
		QueueRef: trimPlannerPunctuation(queue), ExpectedVersion: version,
		Reason: trimPlannerPunctuation(reason),
	}
}

func planClientResourceMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	if plan, matched := parseClientResourceLifecycle(text, lower); matched {
		return plan, true
	}
	if plan, matched := parseClientResourceUpdate(text, lower); matched {
		return plan, true
	}
	resourceTypes := []struct {
		kind, article string
	}{
		{"location", "a"}, {"contact", "a"}, {"asset", "an"},
		{"service", "a"}, {"contract", "a"},
	}
	for _, resourceType := range resourceTypes {
		prefix := "create " + resourceType.article + " " + resourceType.kind
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(text[len(prefix):])
		return parseClientResourceCreate(resourceType.kind, rest)
	}
	for _, resource := range []struct {
		plural string
		kind   string
	}{
		{"locations", "location"}, {"contacts", "contact"}, {"assets", "asset"}, {"services", "service"}, {"contracts", "contract"},
	} {
		prefix := "list " + resource.plural + " for "
		if strings.HasPrefix(lower, prefix) {
			clientName := strings.TrimSpace(strings.TrimRight(text[len(prefix):], ".!?"))
			if clientName == "" {
				return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which client should I list " + resource.plural + " for?"}, true
			}
			return WorkspaceMessagePlan{Kind: WorkspacePlanAction, ToolName: "client_resource.list", ClientName: clientName, ClientResourceKind: resource.kind}, true
		}
	}
	for _, prefix := range []string{"list client resources", "list resources"} {
		if strings.HasPrefix(lower, prefix) {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which resource kind should I list: locations, contacts, assets, services, or contracts?"}, true
		}
	}
	return WorkspaceMessagePlan{}, false
}

func parseClientResourceLifecycle(text, lower string) (WorkspaceMessagePlan, bool) {
	for _, operation := range []string{"deactivate", "reactivate"} {
		for _, kind := range []string{"location", "contact", "asset", "service", "contract"} {
			prefix := operation + " " + kind + " "
			if !strings.HasPrefix(lower, prefix) {
				continue
			}
			rest := strings.TrimSpace(text[len(prefix):])
			forIndex := indexFold(rest, " for ")
			if forIndex < 0 || strings.TrimSpace(rest[:forIndex]) == "" {
				return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which " + kind + " should I " + operation + "?"}, true
			}
			resource := strings.TrimSpace(rest[:forIndex])
			clientAndReason := strings.TrimSpace(rest[forIndex+len(" for "):])
			becauseIndex := indexFold(clientAndReason, " because ")
			if becauseIndex < 0 {
				return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Why should I " + operation + " this " + kind + "?"}, true
			}
			client := strings.TrimSpace(clientAndReason[:becauseIndex])
			reason := trimPlannerPunctuation(clientAndReason[becauseIndex+len(" because "):])
			if client == "" {
				return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which client is this " + kind + " for?"}, true
			}
			if reason == "" {
				return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Why should I " + operation + " this " + kind + "?"}, true
			}
			return WorkspaceMessagePlan{Kind: WorkspacePlanAction, ToolName: kind + "." + operation,
				ClientName: trimPlannerPunctuation(client), ClientResourceDisplayID: trimPlannerPunctuation(resource), Reason: reason}, true
		}
	}
	return WorkspaceMessagePlan{}, false
}

func parseClientResourceUpdate(text, lower string) (WorkspaceMessagePlan, bool) {
	for _, kind := range []string{"location", "contact", "asset", "service", "contract"} {
		prefix := "update " + kind + " "
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(text[len(prefix):])
		forIndex := indexFold(rest, " for ")
		if forIndex < 0 || strings.TrimSpace(rest[:forIndex]) == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which " + kind + " should I update?"}, true
		}
		resource := trimPlannerPunctuation(rest[:forIndex])
		clientAndChange := rest[forIndex+len(" for "):]
		setIndex := indexFold(clientAndChange, " set ")
		if setIndex < 0 {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What should I change on this " + kind + "?"}, true
		}
		client := trimPlannerPunctuation(clientAndChange[:setIndex])
		changeAndReason := clientAndChange[setIndex+len(" set "):]
		becauseIndex := indexFold(changeAndReason, " because ")
		if becauseIndex < 0 {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Why should I update this " + kind + "?"}, true
		}
		change := strings.TrimSpace(changeAndReason[:becauseIndex])
		reason := trimPlannerPunctuation(changeAndReason[becauseIndex+len(" because "):])
		toIndex := indexFold(change, " to ")
		if toIndex < 0 || reason == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What exact value should I set?"}, true
		}
		field := strings.ToLower(strings.TrimSpace(change[:toIndex]))
		value := trimPlannerPunctuation(change[toIndex+len(" to "):])
		field = strings.ReplaceAll(field, " ", "_")
		if !validPlannerResourcePatchField(kind, field) || value == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What approved " + kind + " field should I change?"}, true
		}
		plan := WorkspaceMessagePlan{Kind: WorkspacePlanAction, ToolName: kind + ".update", ClientName: client,
			ClientResourceDisplayID: resource, ClientResourcePatchField: field, ClientResourcePatchValue: value, Reason: reason}
		if kind == "contact" && field == "email" {
			plan.Email = value
		}
		if kind == "contact" && field == "phone" {
			plan.Phone = value
		}
		return plan, true
	}
	return WorkspaceMessagePlan{}, false
}

func validPlannerResourcePatchField(kind, field string) bool {
	allowed := map[string]map[string]bool{
		"location": {"name": true},
		"contact":  {"display_name": true, "email": true, "phone": true, "location": true},
		"asset":    {"name": true, "asset_type": true, "location": true},
		"service":  {"name": true, "criticality": true},
		"contract": {"name": true, "starts_on": true, "ends_on": true},
	}
	return allowed[kind][field]
}

func parseClientResourceCreate(kind, rest string) (WorkspaceMessagePlan, bool) {
	if !strings.HasPrefix(strings.ToLower(rest), "named ") {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What should I call the " + kind + "?"}, true
	}
	rest = strings.TrimSpace(rest[len("named "):])
	displayIndex := indexFold(rest, " with display id ")
	if displayIndex < 0 {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What display ID should I use for the " + kind + "?"}, true
	}
	name := strings.TrimSpace(rest[:displayIndex])
	if name == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What should I call the " + kind + "?"}, true
	}
	afterDisplay := strings.TrimSpace(rest[displayIndex+len(" with display id "):])
	forIndex := indexFold(afterDisplay, " for ")
	if forIndex < 0 {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which client is this " + kind + " for?"}, true
	}
	displayID := strings.TrimSpace(afterDisplay[:forIndex])
	if displayID == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What display ID should I use for the " + kind + "?"}, true
	}
	clientAndClauses := strings.TrimSpace(afterDisplay[forIndex+len(" for "):])
	plan := WorkspaceMessagePlan{Kind: WorkspacePlanAction, ToolName: kind + ".create", ClientResourceName: name, ClientResourceDisplayID: displayID}

	switch kind {
	case "location":
		plan.ClientName = trimPlannerPunctuation(clientAndClauses)
	case "asset":
		client, assetType, found := splitPlannerClause(clientAndClauses, " of type ")
		if !found || trimPlannerPunctuation(assetType) == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "What type of asset is this?"}, true
		}
		plan.ClientName, plan.AssetType = trimPlannerPunctuation(client), trimPlannerPunctuation(assetType)
	case "contract":
		client, startsOn, found := splitPlannerClause(clientAndClauses, " starting ")
		if !found || trimPlannerPunctuation(startsOn) == "" {
			return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "When does the contract start?"}, true
		}
		plan.ClientName, plan.StartsOn = trimPlannerPunctuation(client), trimPlannerPunctuation(startsOn)
		if startsOn, endsOn, hasEnd := splitPlannerClause(plan.StartsOn, " ending "); hasEnd {
			plan.StartsOn, plan.EndsOn = trimPlannerPunctuation(startsOn), trimPlannerPunctuation(endsOn)
		}
	case "contact":
		plan.ClientName, plan.LocationRef, plan.Email, plan.Phone = parseContactClauses(clientAndClauses)
	case "service":
		client, criticality, found := splitPlannerClause(clientAndClauses, " with criticality ")
		plan.ClientName = trimPlannerPunctuation(client)
		if found {
			plan.Criticality = trimPlannerPunctuation(criticality)
		}
	default:
		return WorkspaceMessagePlan{}, false
	}
	if plan.ClientName == "" {
		return WorkspaceMessagePlan{Kind: WorkspacePlanClarification, Reply: "Which client is this " + kind + " for?"}, true
	}
	return plan, true
}

func splitPlannerClause(value, separator string) (string, string, bool) {
	index := indexFold(value, separator)
	if index < 0 {
		return value, "", false
	}
	return value[:index], value[index+len(separator):], true
}

func parseContactClauses(value string) (client, location, email, phone string) {
	client = value
	if before, after, found := splitPlannerClause(client, " with email "); found {
		client, email = before, trimPlannerPunctuation(after)
		if emailPart, phonePart, hasPhone := splitPlannerClause(email, " and phone "); hasPhone {
			email, phone = trimPlannerPunctuation(emailPart), trimPlannerPunctuation(phonePart)
		}
	} else if before, after, found := splitPlannerClause(client, " with phone "); found {
		client, phone = before, trimPlannerPunctuation(after)
	}
	if before, after, found := splitPlannerClause(client, " at "); found {
		client, location = before, trimPlannerPunctuation(after)
	}
	return trimPlannerPunctuation(client), location, email, phone
}

func trimPlannerPunctuation(value string) string {
	return strings.TrimSpace(strings.TrimRight(value, ".!?"))
}

func planClientMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	var rest string
	for _, prefix := range []string{"create a client", "create client"} {
		if strings.HasPrefix(lower, prefix) {
			rest = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	if rest == "" {
		if lower != "create a client" && lower != "create client" {
			return WorkspaceMessagePlan{}, false
		}
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What should I call the client, and what display ID should I use?",
		}, true
	}

	const namedPrefix = "named "
	if !strings.HasPrefix(strings.ToLower(rest), namedPrefix) {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What should I call the client?",
		}, true
	}
	nameAndDisplayID := strings.TrimSpace(rest[len(namedPrefix):])
	const displaySeparator = " with display id "
	displayIndex := indexFold(nameAndDisplayID, displaySeparator)
	if displayIndex < 0 {
		if strings.Trim(strings.TrimRight(nameAndDisplayID, ".!?"), " ") == "" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "What should I call the client?",
			}, true
		}
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What display ID should I use for the client?",
		}, true
	}

	clientName := strings.TrimSpace(nameAndDisplayID[:displayIndex])
	if clientName == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What should I call the client?",
		}, true
	}
	clientDisplayID := strings.TrimSpace(strings.TrimRight(
		nameAndDisplayID[displayIndex+len(displaySeparator):], ".!?",
	))
	if clientDisplayID == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What display ID should I use for the client?",
		}, true
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "client.create",
		ClientName: clientName, ClientDisplayID: clientDisplayID,
	}, true
}

func planTaskMessage(text, lower string) (WorkspaceMessagePlan, bool) {
	var rest string
	for _, prefix := range []string{
		"add a task", "create a task", "add task", "create task",
	} {
		if strings.HasPrefix(lower, prefix) {
			rest = strings.TrimSpace(text[len(prefix):])
			break
		}
	}
	if rest == "" {
		if lower == "add a task" || lower == "create a task" ||
			lower == "add task" || lower == "create task" {
			return WorkspaceMessagePlan{
				Kind:  WorkspacePlanClarification,
				Reply: "What should I call the task?",
			}, true
		}
		return WorkspaceMessagePlan{}, false
	}
	for _, prefix := range []string{"titled ", "named "} {
		if strings.HasPrefix(strings.ToLower(rest), prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			break
		}
	}
	projectIndex := indexFold(rest, " to project ")
	projectSeparator := " to project "
	if projectIndex < 0 {
		projectIndex = indexFold(rest, " in project ")
		projectSeparator = " in project "
	}
	if projectIndex < 0 {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which project should I add the task to?",
		}, true
	}
	taskTitle := strings.TrimSpace(rest[:projectIndex])
	if taskTitle == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "What should I call the task?",
		}, true
	}
	projectAndClient := strings.TrimSpace(rest[projectIndex+len(projectSeparator):])
	forIndex := lastIndexFold(projectAndClient, " for ")
	if forIndex < 0 {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which client is this task for?",
		}, true
	}
	projectRef := strings.TrimSpace(projectAndClient[:forIndex])
	if projectRef == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which project should I add the task to?",
		}, true
	}
	clientName, tagIDs := splitWorkspaceTagIDs(strings.TrimSpace(strings.TrimRight(
		projectAndClient[forIndex+len(" for "):], ".!?",
	)))
	if clientName == "" {
		return WorkspaceMessagePlan{
			Kind:  WorkspacePlanClarification,
			Reply: "Which client is this task for?",
		}, true
	}
	return WorkspaceMessagePlan{
		Kind: WorkspacePlanAction, ToolName: "task.create",
		TaskTitle: taskTitle, ProjectRef: projectRef, ClientName: clientName, TagIDs: tagIDs,
	}, true
}

func indexFold(value, separator string) int {
	return strings.Index(strings.ToLower(value), separator)
}

func lastIndexFold(value, separator string) int {
	return strings.LastIndex(strings.ToLower(value), separator)
}

func splitProjectTasks(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	tasks := make([]string, 0, len(parts))
	for _, part := range parts {
		task := strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(task), "and ") {
			task = strings.TrimSpace(task[len("and "):])
		}
		if task != "" {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func splitWorkspaceTagIDs(value string) (string, []string) {
	const marker = " with tag ids "
	index := lastIndexFold(value, marker)
	if index < 0 {
		return strings.TrimSpace(value), nil
	}
	base := strings.TrimSpace(value[:index])
	parts := strings.FieldsFunc(value[index+len(marker):], func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	})
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		if tagID := trimPlannerPunctuation(part); tagID != "" {
			tags = append(tags, tagID)
		}
	}
	return base, tags
}
