package logging

import (
	"log"
)

func init() {
	log.SetFlags(0)
}

// Info prints informational messages using the required format.
func Info(format string, args ...interface{}) {
	log.Printf("[INFO] "+format, args...)
}

// Warn prints warning messages without causing the agent to exit.
func Warn(format string, args ...interface{}) {
	log.Printf("[WARN] "+format, args...)
}

// Error prints error-level messages.
func Error(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}
