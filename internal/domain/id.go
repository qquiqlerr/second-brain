package domain

import (
	"fmt"
	"time"
)

// BuildID derives the canonical note identifier: YYYYMMDD-slug, using the
// date as-is (caller must convert to the target timezone first).
func BuildID(date time.Time, slug string) string {
	return fmt.Sprintf("%s-%s", date.Format("20060102"), slug)
}
