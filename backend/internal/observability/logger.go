package observability

import (
	"io"
	"log/slog"
	"strings"
	"sync"
)

func NewLogger(writer io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactSecretAttributes,
	}))
}

func redactSecretAttributes(_ []string, attribute slog.Attr) slog.Attr {
	key := strings.ToLower(attribute.Key)
	switch key {
	case "authorization", "proxy-authorization", "cookie", "set-cookie",
		"body", "source_body", "preview", "token_adjacent_text":
		return slog.String(attribute.Key, "[REDACTED]")
	}
	for _, suffix := range []string{"_url", "_key", "_secret", "_token", "_password"} {
		if strings.HasSuffix(key, suffix) {
			return slog.String(attribute.Key, "[REDACTED]")
		}
	}
	return attribute
}

// MentionMetric is intentionally content-free. It admits only operational
// identity and bounded outcome dimensions; source text has no representable
// field and therefore cannot accidentally enter mention telemetry.
type MentionMetric struct {
	Name         string
	TargetType   string
	ParentType   string
	State        string
	Channel      string
	Outcome      string
	MSPID        string
	ClientID     string
	ObjectID     string
	OccurrenceID string
}

type mentionMetricKey struct {
	name, targetType, parentType, state, channel, outcome string
}

type MentionTelemetry struct {
	logger *slog.Logger
	mu     sync.RWMutex
	values map[mentionMetricKey]uint64
}

func NewMentionTelemetry(logger *slog.Logger) *MentionTelemetry {
	return &MentionTelemetry{logger: logger, values: make(map[mentionMetricKey]uint64)}
}

func (telemetry *MentionTelemetry) Count(metric MentionMetric) {
	telemetry.CountN(metric, 1)
}

func (telemetry *MentionTelemetry) CountN(metric MentionMetric, count uint64) {
	if telemetry == nil {
		return
	}
	if count == 0 {
		return
	}
	key := mentionMetricKey{
		name:       normalizedMentionMetricName(metric.Name),
		targetType: normalizedMentionDimension(metric.TargetType),
		parentType: normalizedMentionDimension(metric.ParentType),
		state:      normalizedMentionDimension(metric.State),
		channel:    normalizedMentionDimension(metric.Channel),
		outcome:    normalizedMentionDimension(metric.Outcome),
	}
	telemetry.mu.Lock()
	telemetry.values[key] += count
	telemetry.mu.Unlock()
	if telemetry.logger != nil {
		telemetry.logger.Info(
			"mention metric",
			"metric", key.name,
			"target_type", key.targetType,
			"parent_type", key.parentType,
			"state", key.state,
			"channel", key.channel,
			"outcome", key.outcome,
			"msp_id", metric.MSPID,
			"client_id", metric.ClientID,
			"object_id", metric.ObjectID,
			"occurrence_id", metric.OccurrenceID,
			"count", count,
		)
	}
}

func (telemetry *MentionTelemetry) Value(name, parentType, outcome string) uint64 {
	if telemetry == nil {
		return 0
	}
	wanted := mentionMetricKey{
		name:       normalizedMentionMetricName(name),
		parentType: normalizedMentionDimension(parentType),
		outcome:    normalizedMentionDimension(outcome),
	}
	telemetry.mu.RLock()
	defer telemetry.mu.RUnlock()
	var result uint64
	for key, count := range telemetry.values {
		if key.name == wanted.name && key.parentType == wanted.parentType && key.outcome == wanted.outcome {
			result += count
		}
	}
	return result
}

func normalizedMentionMetricName(value string) string {
	switch value {
	case "occurrence_target", "resolution", "deduplication",
		"item_state_transition", "remention", "suppression", "preview",
		"deep_link", "notification":
		return value
	default:
		return "unknown"
	}
}

func normalizedMentionDimension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "none"
	}
	if len(value) > 64 {
		return "other"
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
			return "other"
		}
	}
	return value
}
