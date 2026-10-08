package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type PrivacyMode string

const (
	PrivacyFull PrivacyMode = "full"
	PrivacyBusy PrivacyMode = "busy"
)

type EventCapabilities struct {
	ViewSource bool `json:"view_source"`
	Schedule   bool `json:"schedule"`
}

// SourceVisibilityAuthorizer deliberately separates source visibility from
// workforce authority. Client membership alone never grants source details.
type SourceVisibilityAuthorizer interface {
	CanReadCalendarSource(context.Context, authorization.Principal, SourceRef) (bool, error)
	CanScheduleCalendarTechnician(context.Context, authorization.Principal, string) (bool, error)
}

func stableCalendarID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return "cal_" + hex.EncodeToString(h.Sum(nil)[:16])
}

func stableEventID(source SourceRef, role string) string {
	return stableCalendarID(source.MSPID, source.Type, source.ID, role)
}

func privacySafeSource(source SourceRef, visible bool) SourceRef {
	if !visible {
		return SourceRef{}
	}
	return source
}
