// Package billingexport produces the approved V1 CSV boundary without
// introducing invoicing, payments, or accounting synchronization.
package billingexport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strconv"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

var ErrApprovalRequired = errors.New("time-entry approval required")

type Policy struct {
	RequireApproval bool
}

type Entry struct {
	ID              string `json:"id"`
	WorkRecordID    string `json:"work_record_id"`
	TechnicianID    string `json:"technician_id"`
	DurationSeconds int64  `json:"duration_seconds"`
	Billable        bool   `json:"billable"`
	Approved        bool   `json:"approved"`
	Version         int64  `json:"version"`
}

func BuildCSV(
	principal authorization.Principal,
	target scope.Target,
	policy Policy,
	entries []Entry,
) ([]byte, error) {
	if err := authorization.Authorize(principal, "time_entry.export", target); err != nil {
		return nil, err
	}
	if policy.RequireApproval {
		for _, entry := range entries {
			if entry.Billable && !entry.Approved {
				return nil, ErrApprovalRequired
			}
		}
	}
	buffer := new(bytes.Buffer)
	writer := csv.NewWriter(buffer)
	if err := writer.Write([]string{
		"time_entry_id", "work_record_id", "technician_id",
		"duration_seconds", "billable", "approved",
	}); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if err := writer.Write([]string{
			entry.ID, entry.WorkRecordID, entry.TechnicianID,
			strconv.FormatInt(entry.DurationSeconds, 10),
			strconv.FormatBool(entry.Billable), strconv.FormatBool(entry.Approved),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
