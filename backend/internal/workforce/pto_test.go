package workforce

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type ptoRepositoryStub struct {
	managerID string
	request   PTORequest
	mutation  PTOMutation
}

func (r *ptoRepositoryStub) TechnicianInMSP(context.Context, string, string) (bool, error) {
	return true, nil
}
func (r *ptoRepositoryStub) ActiveManager(context.Context, string, string) (string, error) {
	return r.managerID, nil
}
func (r *ptoRepositoryStub) FindPTO(context.Context, string, string) (PTORequest, error) {
	return r.request, nil
}
func (r *ptoRepositoryStub) CreatePTOAtomic(_ context.Context, m PTOMutation) error {
	r.mutation = m
	return nil
}
func (r *ptoRepositoryStub) UpdatePTOAtomic(_ context.Context, m PTOMutation, _ int64) error {
	r.mutation = m
	return nil
}

func TestPTOApprovalRequiresConfiguredManagerOrAdmin(t *testing.T) {
	r := &ptoRepositoryStub{managerID: workforceManagerID, request: PTORequest{ID: workforcePTOID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, PTOType: "vacation", State: Requested, Version: 1}}
	s := NewPTOService(r, time.Now, wfIDs())
	_, err := s.Decide(context.Background(), DecidePTOCommand{Principal: workforcePrincipal("calendar.schedule"), RequestID: workforcePTOID, Decision: Approved, ActorID: workforceActorID, ExpectedVersion: 1, Source: "api", IdempotencyKey: "decision"})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error = %v", err)
	}
}

func TestPTOStateMachineCapacityAndRequesterCancel(t *testing.T) {
	r := &ptoRepositoryStub{managerID: workforceManagerID, request: PTORequest{ID: workforcePTOID, MSPID: workforceMSPID, TechnicianID: workforceRequesterID, PTOType: "vacation", State: Requested, Version: 1}}
	s := NewPTOService(r, time.Now, wfIDs())
	approved, err := s.Decide(context.Background(), DecidePTOCommand{Principal: authorization.Principal{ID: workforceManagerID, Scope: workforcePrincipal("calendar.schedule").Scope}, RequestID: workforcePTOID, Decision: Approved, ActorID: workforceManagerID, ExpectedVersion: 1, Source: "api", IdempotencyKey: "approve"})
	if err != nil || !approved.ReducesCapacity() {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	r.request = approved
	cancelled, err := s.Cancel(context.Background(), CancelPTOCommand{Principal: authorization.Principal{ID: workforceRequesterID, Scope: workforcePrincipal("calendar.schedule").Scope}, RequestID: workforcePTOID, ActorID: workforceRequesterID, ExpectedVersion: 2, Source: "api", IdempotencyKey: "cancel"})
	if err != nil || cancelled.State != Cancelled || cancelled.ReducesCapacity() {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	r.request.State = Rejected
	r.request.Version = 2
	if _, err = s.Decide(context.Background(), DecidePTOCommand{Principal: workforcePrincipal("calendar.workforce.manage"), RequestID: workforcePTOID, Decision: Approved, ActorID: workforceActorID, ExpectedVersion: 2, Source: "api", IdempotencyKey: "again"}); !errors.Is(err, ErrInvalidPTOTransition) {
		t.Fatalf("error=%v", err)
	}
}

func TestPTORejectsUnknownTypeBeforeRepository(t *testing.T) {
	r := &ptoRepositoryStub{}
	s := NewPTOService(r, time.Now, wfIDs())
	start, end := time.Now(), time.Now().Add(time.Hour)
	_, err := s.Request(context.Background(), RequestPTOCommand{Principal: authorization.Principal{ID: workforceRequesterID, Scope: workforcePrincipal("calendar.schedule").Scope}, TechnicianID: workforceRequesterID, PTOType: "holiday", StartsAt: &start, EndsAt: &end, Timezone: "UTC", ActorID: workforceRequesterID, Source: "api", IdempotencyKey: "unknown-pto"})
	if !errors.Is(err, ErrInvalidPTO) || r.mutation.Request.ID != "" {
		t.Fatalf("error=%v mutation=%+v", err, r.mutation)
	}
}
