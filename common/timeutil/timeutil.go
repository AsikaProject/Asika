package timeutil

import "time"

const (
	Millisecond = time.Millisecond
	Second      = time.Second
	Minute      = time.Minute
	Hour        = time.Hour
	Day         = 24 * time.Hour
	Week        = 7 * Day
	TwoWeeks    = 14 * Day
	Month       = 30 * Day
	ThreeMonths = 90 * Day
)
