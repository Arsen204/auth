package logger

import "github.com/rs/zerolog"

type zlogAdaptor struct {
	l *zerolog.Logger
}

func NewZlogAdaptor(l *zerolog.Logger) L {
	return &zlogAdaptor{l: l}
}

func (a zlogAdaptor) Logf(format string, args ...interface{}) {
	a.l.Info().Msgf(format, args...)
}

func (a zlogAdaptor) Debug(format string, args ...interface{}) {
	a.l.Debug().Msgf(format, args...)
}

func (a zlogAdaptor) Warn(format string, args ...interface{}) {
	a.l.Warn().Msgf(format, args...)
}

func (a zlogAdaptor) Info(format string, args ...interface{}) {
	a.l.Info().Msgf(format, args...)
}

func (a zlogAdaptor) Error(format string, args ...interface{}) {
	a.l.Error().Msgf(format, args...)
}
