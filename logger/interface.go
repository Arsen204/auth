// Package logger defines interface for logging. Implementation should be passed by user.
// Also provides NoOp (do-nothing) and Std (redirect to std log) predefined loggers.
package logger

// L defined logger interface used everywhere in the package
type L interface {
	Logf(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}
