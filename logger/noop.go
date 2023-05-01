package logger

type NoOp struct{}

func (l NoOp) Logf(format string, args ...interface{}) {}

func (l NoOp) Debug(format string, args ...interface{}) {}

func (l NoOp) Warn(format string, args ...interface{}) {}

func (l NoOp) Info(format string, args ...interface{}) {}

func (l NoOp) Error(format string, args ...interface{}) {}
