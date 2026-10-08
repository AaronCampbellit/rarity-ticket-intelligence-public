package tagging

import (
	"context"
	"strings"
	"time"
)

func CanonicalTagPair(first, second string) (string, string, bool) {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	if first == "" || second == "" || first == second {
		return "", "", false
	}
	if first > second {
		first, second = second, first
	}
	return first, second, true
}

type ProjectionResult struct {
	Processed        int       `json:"processed"`
	InheritedEffects int       `json:"inherited_effects"`
	ProjectionAsOf   time.Time `json:"projection_as_of"`
	LagSeconds       int64     `json:"lag_seconds"`
}
type ProjectionRepository interface {
	ProjectTagEvents(context.Context, int) (ProjectionResult, error)
}
type ProjectionWorker struct{ repository ProjectionRepository }

func NewProjectionWorker(repository ProjectionRepository) *ProjectionWorker {
	return &ProjectionWorker{repository: repository}
}
func (w *ProjectionWorker) RunOnce(ctx context.Context, limit int) (ProjectionResult, error) {
	if w == nil || w.repository == nil || limit < 1 || limit > 500 {
		return ProjectionResult{}, ErrInvalidReportFilter
	}
	return w.repository.ProjectTagEvents(ctx, limit)
}
