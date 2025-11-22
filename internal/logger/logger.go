package logger

import (
	"log"
	"sync"
)

var once sync.Once

func initLogger() {
	log.SetFlags(0)
}

// Infof prints informational messages with a fixed prefix that matches the spec.
func Infof(format string, args ...interface{}) {
	once.Do(initLogger)
	log.Printf("[INFO] "+format, args...)
}

// Warnf prints warning messages with the expected prefix.
func Warnf(format string, args ...interface{}) {
	once.Do(initLogger)
	log.Printf("[WARN] "+format, args...)
}

// Errorf prints error messages with the expected prefix.
func Errorf(format string, args ...interface{}) {
	once.Do(initLogger)
	log.Printf("[ERROR] "+format, args...)
}

// Fatalf logs an error and exits the process.
func Fatalf(format string, args ...interface{}) {
	once.Do(initLogger)
	log.Fatalf("[FATAL] "+format, args...)
}
