package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/comments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

type PriorityActions interface {
	Change(
		context.Context,
		workrecords.PriorityCommand,
	) (workrecords.Record, error)
}

type AssignmentActions interface {
	Assign(
		context.Context,
		workrecords.AssignCommand,
	) (workrecords.Record, error)
}

type TransitionActions interface {
	Transition(
		context.Context,
		workrecords.TransitionCommand,
	) (workrecords.Record, error)
}

type CommentActions interface {
	Create(
		context.Context,
		CommentCommand,
	) (string, error)
}

type ExternalActions interface {
	Call(
		context.Context,
		authorization.Principal,
		string,
		map[string]string,
		map[string]string,
	) (ActionResult, error)
}

type TagActions interface {
	Get(context.Context, tagging.GetCommand) (tagging.TaggedObject, error)
	ReplaceDirect(context.Context, tagging.ReplaceCommand) (tagging.TaggedObject, error)
}

type RuntimeActionExecutor struct {
	priorities  PriorityActions
	assignments AssignmentActions
	transitions TransitionActions
	comments    CommentActions
	external    ExternalActions
	tags        TagActions
}

var _ ActionExecutor = (*RuntimeActionExecutor)(nil)

func NewRuntimeActionExecutor(
	priorities PriorityActions,
	assignments AssignmentActions,
	transitions TransitionActions,
	commentActions CommentActions,
	external ExternalActions,
	tagActions ...TagActions,
) *RuntimeActionExecutor {
	executor := &RuntimeActionExecutor{
		priorities: priorities, assignments: assignments,
		transitions: transitions, comments: commentActions,
		external: external,
	}
	if len(tagActions) > 0 {
		executor.tags = tagActions[0]
	}
	return executor
}

func (e *RuntimeActionExecutor) Execute(
	ctx context.Context,
	principal authorization.Principal,
	action Action,
	snapshot map[string]string,
) (ActionResult, error) {
	if e == nil || e.priorities == nil || e.assignments == nil ||
		e.transitions == nil || e.comments == nil || e.external == nil {
		return ActionResult{}, ErrActionFailed
	}
	if action.Kind == ActionCallHTTP {
		if strings.TrimSpace(action.ConnectionRef) == "" {
			return ActionResult{}, ErrActionFailed
		}
		return e.external.Call(
			ctx, principal, action.ConnectionRef,
			copyStringMap(action.Parameters), sanitizeSnapshot(snapshot),
		)
	}
	target, subject, version, runID, err := automationTarget(
		principal, snapshot,
	)
	if err != nil {
		return ActionResult{}, err
	}
	actor := workrecords.Actor{
		Type: "automation", ID: principal.ID, Source: "automation",
	}
	switch action.Kind {
	case ActionAddTags, ActionRemoveTags:
		if e.tags == nil {
			return ActionResult{}, ErrActionFailed
		}
		return e.executeTags(ctx, principal, action, snapshot, subject, runID)
	case ActionUpdateField:
		if subject.ObjectType != tagging.ObjectWorkRecord {
			return ActionResult{}, ErrActionFailed
		}
		if strings.TrimSpace(action.Parameters["field"]) != "priority" ||
			strings.TrimSpace(action.Parameters["value"]) == "" ||
			strings.TrimSpace(action.Parameters["reason"]) == "" {
			return ActionResult{}, ErrActionFailed
		}
		record, err := e.priorities.Change(
			ctx,
			workrecords.PriorityCommand{
				Principal: principal, Target: target,
				WorkRecordID: subject.ObjectID, ExpectedVersion: version,
				Priority: action.Parameters["value"], Actor: actor,
				Reason: action.Parameters["reason"], CausationID: runID,
			},
		)
		return actionRecordResult(record, err)
	case ActionAddComment:
		if subject.ObjectType != tagging.ObjectWorkRecord {
			return ActionResult{}, ErrActionFailed
		}
		visibility := comments.Visibility(
			strings.TrimSpace(action.Parameters["visibility"]),
		)
		if visibility != comments.Internal &&
			visibility != comments.ClientVisible {
			return ActionResult{}, ErrActionFailed
		}
		stepID := strings.TrimSpace(snapshot["_automation_step_id"])
		if stepID == "" {
			return ActionResult{}, ErrActionFailed
		}
		commentID, err := e.comments.Create(
			ctx,
			CommentCommand{
				Principal: principal, WorkRecordID: subject.ObjectID,
				Visibility: visibility, Body: action.Parameters["body"],
				CausationID:    runID,
				IdempotencyKey: automationCommentIdempotencyKey(runID, stepID),
			},
		)
		if err != nil {
			return ActionResult{}, err
		}
		return ActionResult{ChangedObjectIDs: []string{commentID}}, nil
	case ActionAssign:
		if subject.ObjectType != tagging.ObjectWorkRecord {
			return ActionResult{}, ErrActionFailed
		}
		ownerID := strings.TrimSpace(action.Parameters["owner_id"])
		if ownerID == "" {
			ownerID = strings.TrimSpace(action.Parameters["team_id"])
		}
		if ownerID == "" {
			return ActionResult{}, ErrActionFailed
		}
		record, err := e.assignments.Assign(
			ctx,
			workrecords.AssignCommand{
				Principal: principal, Target: target,
				WorkRecordID: subject.ObjectID, ExpectedVersion: version,
				OwnerID: ownerID,
				Reason: assignmentReason(
					action.Parameters["reason"], principal.ID, runID,
				),
				Actor: actor, CausationID: runID,
			},
		)
		return actionRecordResult(record, err)
	case ActionTransition:
		if subject.ObjectType != tagging.ObjectWorkRecord {
			return ActionResult{}, ErrActionFailed
		}
		toStatus := strings.TrimSpace(action.Parameters["to_status"])
		if toStatus == "" {
			return ActionResult{}, ErrActionFailed
		}
		record, err := e.transitions.Transition(
			ctx,
			workrecords.TransitionCommand{
				Principal: principal, Target: target,
				WorkRecordID: subject.ObjectID, ExpectedVersion: version,
				ToStatus: toStatus, Actor: actor,
				Reason: action.Parameters["reason"], CausationID: runID,
			},
		)
		return actionRecordResult(record, err)
	default:
		return ActionResult{}, ErrActionFailed
	}
}

func automationCommentIdempotencyKey(runID, stepID string) string {
	digest := sha256.Sum256([]byte(runID + "\x00" + stepID + "\x00" + string(ActionAddComment)))
	return "automation-comment-" + hex.EncodeToString(digest[:])
}

func (e *RuntimeActionExecutor) executeTags(ctx context.Context, principal authorization.Principal, action Action, snapshot map[string]string, ref tagging.TargetRef, runID string) (ActionResult, error) {
	var requested []string
	if json.Unmarshal([]byte(action.Parameters["tag_ids"]), &requested) != nil || len(requested) == 0 {
		return ActionResult{}, ErrActionFailed
	}
	current, err := e.tags.Get(ctx, tagging.GetCommand{Principal: principal, Target: ref})
	if err != nil {
		return ActionResult{}, err
	}
	desired := make(map[string]struct{}, len(current.Direct)+len(requested))
	for _, assignment := range current.Direct {
		desired[assignment.Tag.ID] = struct{}{}
	}
	for _, id := range requested {
		if action.Kind == ActionAddTags {
			desired[id] = struct{}{}
			continue
		}
		if _, direct := desired[id]; !direct {
			for _, inherited := range current.Inherited {
				if inherited.Tag.ID == id {
					return ActionResult{}, ErrActionFailed
				}
			}
		}
		delete(desired, id)
	}
	ids := make([]string, 0, len(desired))
	for id := range desired {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fingerprint := runID + ":" + snapshot["_automation_step_id"] + ":" + string(action.Kind) + ":" + action.Parameters["tag_ids"]
	updated, err := e.tags.ReplaceDirect(ctx, tagging.ReplaceCommand{Principal: principal, Target: ref, ExpectedObjectVersion: current.ObjectVersion, TagIDs: ids, Source: tagging.SourceAutomation, Reason: "automation tag action", IdempotencyKey: fingerprint, CorrelationID: runID, CausationID: runID})
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{ChangedObjectIDs: []string{ref.ObjectID}, OutputSnapshot: map[string]string{"_classification_version": strconv.FormatInt(updated.ObjectVersion, 10)}}, nil
}

func assignmentReason(explicit, definitionID, runID string) string {
	if reason := strings.TrimSpace(explicit); reason != "" {
		return reason
	}
	return "Automation assignment by definition " +
		strings.TrimSpace(definitionID) + " (run " + runID + ")"
}

func automationTarget(
	principal authorization.Principal,
	snapshot map[string]string,
) (scope.Target, tagging.TargetRef, int64, string, error) {
	version, err := strconv.ParseInt(
		strings.TrimSpace(snapshot["_subject_version"]), 10, 64,
	)
	target := scope.Target{
		MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID,
	}
	objectID := strings.TrimSpace(snapshot["_subject_id"])
	objectType := tagging.ObjectType(strings.TrimSpace(snapshot["_subject_type"]))
	runID := strings.TrimSpace(snapshot["_automation_run_id"])
	validObjectType := false
	switch objectType {
	case tagging.ObjectWorkRecord, tagging.ObjectTask, tagging.ObjectProject,
		tagging.ObjectAsset, tagging.ObjectKnowledgeArticle, tagging.ObjectTimeEntry:
		validObjectType = true
	}
	if err != nil || version < 1 ||
		!validObjectType ||
		target.MSPID == "" || target.ClientID == "" ||
		objectID == "" || runID == "" || principal.ID == "" {
		return scope.Target{}, tagging.TargetRef{}, 0, "", ErrActionFailed
	}
	return target, tagging.TargetRef{
		MSPID: target.MSPID, ClientID: target.ClientID,
		ObjectType: objectType, ObjectID: objectID,
	}, version, runID, nil
}

func actionRecordResult(
	record workrecords.Record,
	err error,
) (ActionResult, error) {
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{
		ChangedObjectIDs: []string{record.ID},
		OutputSnapshot: map[string]string{
			"_subject_version": strconv.FormatInt(record.Version, 10),
		},
	}, nil
}

func copyStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
