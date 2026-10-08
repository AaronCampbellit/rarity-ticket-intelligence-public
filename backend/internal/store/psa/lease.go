package psa

import "time"

// leaseMicroseconds preserves a positive caller duration when PostgreSQL lease
// timestamps are expressed at microsecond precision. PostgreSQL cannot retain
// the sub-microsecond tail, so round up rather than shorten the lease.
func leaseMicroseconds(lease time.Duration) int64 {
	if lease <= 0 {
		return 0
	}
	microseconds := lease / time.Microsecond
	if lease%time.Microsecond != 0 {
		microseconds++
	}
	return int64(microseconds)
}
