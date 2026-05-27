package telegram

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// HandleFunc is the signature exposed by Router.Handle, also used by middleware.
type HandleFunc func(ctx context.Context, u IncomingUpdate) (string, error)

// RecoverMiddleware turns panics into a user-visible error reply and an
// ERROR log entry. The process keeps running.
func RecoverMiddleware(logger *slog.Logger, next HandleFunc) HandleFunc {
	return func(ctx context.Context, u IncomingUpdate) (reply string, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in handler",
					"recover", r,
					"update_id", u.UpdateID,
					"user_id", u.FromUserID,
					"stack", string(debug.Stack()),
				)
				reply = "❌ внутренняя ошибка, см. логи"
				err = nil
			}
		}()
		return next(ctx, u)
	}
}
