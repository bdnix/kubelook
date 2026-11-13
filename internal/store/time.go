package store

import "time"

// ms converts a time to Unix milliseconds, the storage format for every timestamp.
func ms(t time.Time) int64 { return t.UnixMilli() }

// fromMS converts stored Unix milliseconds back to a UTC time.
func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }
