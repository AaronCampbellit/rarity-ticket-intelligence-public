package main

import (
	"bytes"
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"testing"
)

func TestCalendarReconcileDefaultsToReadOnlyAndBoundsWork(t *testing.T) {
	for _, test := range []struct {
		args    []string
		repair  bool
		invalid bool
	}{
		{[]string{"--msp-id", "019fdb80-0000-7000-8000-000000000001"}, false, false},
		{[]string{"--msp-id", "019fdb80-0000-7000-8000-000000000001", "--repair"}, true, false},
		{[]string{"--msp-id", "019fdb80-0000-7000-8000-000000000001", "--limit", "5001"}, false, true},
		{[]string{"--msp-id", "bad"}, false, true},
	} {
		var out bytes.Buffer
		called := false
		err := runCalendarReconcile(context.Background(), test.args, func(string) string { return "test-database" }, &out, &out, func(_ context.Context, _ string, request calendar.ReconcileRequest) (calendar.ReconcileReport, error) {
			called = true
			if request.Repair != test.repair {
				t.Fatalf("repair=%v", request.Repair)
			}
			return calendar.ReconcileReport{Scanned: 3, Missing: 1}, nil
		})
		if (err != nil) != test.invalid || called == test.invalid {
			t.Fatalf("args=%v called=%v err=%v", test.args, called, err)
		}
	}
}
