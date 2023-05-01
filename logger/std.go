package logger

import "github.com/rs/zerolog/log"

type Std struct{}

func (l Std) Logf(format string, args ...interface{}) { log.Printf(format, args...) }

func (l Std) Debug(format string, args ...interface{}) { log.Printf(format, args...) }

func (l Std) Warn(format string, args ...interface{}) { log.Printf(format, args...) }

func (l Std) Info(format string, args ...interface{}) { log.Printf(format, args...) }

func (l Std) Error(format string, args ...interface{}) { log.Printf(format, args...) }
