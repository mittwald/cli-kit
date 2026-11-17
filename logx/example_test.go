package logx_test

import (
	"log/slog"
	"os"

	"github.com/mittwald/cli-kit/logx"
)

func ExampleNewLineHandler() {
	logger := slog.New(logx.NewLineHandler(os.Stdout, &logx.Options{Level: slog.LevelDebug}))
	request := logger.WithGroup("request").With("method", "GET")
	request.Debug("sent", "path", "/v2/projects", "status", 200)
	logger.Warn("login failed", "body", "password=hunter2 \x1b[31mdenied\x1b[0m")
	// Output:
	// sent request.method=GET request.path=/v2/projects request.status=200
	// login failed body="password=[REDACTED] denied"
}
