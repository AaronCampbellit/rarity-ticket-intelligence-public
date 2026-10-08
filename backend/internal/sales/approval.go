package sales

import "time"

type AcceptanceMethod string

const (
	AcceptanceElectronic AcceptanceMethod = "electronic"
	AcceptanceOffline    AcceptanceMethod = "offline"
)

type Acceptance struct {
	ID                string
	ProposalID        string
	ProposalVersionID string
	MSPID             string
	ClientID          string
	Method            AcceptanceMethod
	SignerName        string
	SignerEmail       string
	AcceptedAt        time.Time
	RecordedBy        string
	Evidence          map[string]string
	PDFSnapshotID     string
}
