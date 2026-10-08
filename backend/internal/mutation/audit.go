package mutation

import "reflect"

// SafeDiff records changed business fields without making sensitive values
// available to the general audit or event streams.
type SafeDiff map[string]map[string]any

// BuildSafeDiff returns only changed fields. Sensitive fields retain evidence
// that they changed while omitting both values.
func BuildSafeDiff(before, after map[string]any, sensitiveFields ...string) SafeDiff {
	sensitive := make(map[string]struct{}, len(sensitiveFields))
	for _, field := range sensitiveFields {
		sensitive[field] = struct{}{}
	}
	diff := SafeDiff{}
	for field, afterValue := range after {
		beforeValue := before[field]
		if reflect.DeepEqual(beforeValue, afterValue) {
			continue
		}
		if _, redacted := sensitive[field]; redacted {
			diff[field] = map[string]any{"changed": true, "redacted": true}
			continue
		}
		diff[field] = map[string]any{"before": beforeValue, "after": afterValue}
	}
	return diff
}
