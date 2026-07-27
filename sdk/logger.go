package sdk

import (
	"context"
	"fmt"
	"log"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type ctxKey struct{}

type StdLogger struct {
	*zap.Logger
}

func intLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendInt8((int8(l) + 3) * 10)
}

var AppLogger *StdLogger = NewLogger()

// NewLogger creates a new application logger.
func NewLogger() *StdLogger {
	var cfg zap.Config
	outputLevel := zap.InfoLevel
	levelEnv := os.Getenv("LOG_LEVEL")
	if levelEnv != "" {
		levelFromEnv, err := zapcore.ParseLevel(levelEnv)
		if err != nil {
			log.Println(fmt.Errorf("invalid level, defaulting to INFO: %w", err))
		} else {
			outputLevel = levelFromEnv
		}
	}

	var DgnEnv = os.Getenv("ENVIRONMENT")
	if DgnEnv != "LOCAL" {
		cfg = zap.NewProductionConfig()
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr", "stdout"}
		cfg.InitialFields = map[string]any{"name": "sdk.computer.be"}
		cfg.EncoderConfig.EncodeLevel = intLevelEncoder
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		cfg.EncoderConfig.TimeKey = "time"
		cfg.Level = zap.NewAtomicLevelAt(outputLevel)
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return &StdLogger{Logger: logger}
}

func GetAppLogger() *StdLogger {
	return AppLogger
}

func GetLogger(ctx context.Context) *StdLogger {
	if l, ok := ctx.Value(ctxKey{}).(*StdLogger); ok {
		return l
	} else if l := AppLogger; l != nil {
		return l
	}
	return &StdLogger{Logger: zap.NewNop()}
}

func LoggerWithCtx(ctx context.Context, l *StdLogger) context.Context {
	if lp, ok := ctx.Value(ctxKey{}).(*StdLogger); ok {
		if lp == l {
			return ctx
		}
	}
	return context.WithValue(ctx, ctxKey{}, l)
}

func CtxBasedLogger(ctx context.Context, fields map[string]string) (context.Context, *StdLogger) {
	l := GetLogger(ctx)
	zapFields := make([]zap.Field, 0, len(fields))

	for key, value := range fields {
		zapFields = append(
			zapFields, zap.String(string(key), value),
		)
	}

	l = &StdLogger{Logger: l.With(zapFields...)}
	ctx = LoggerWithCtx(ctx, l)
	return ctx, l
}
