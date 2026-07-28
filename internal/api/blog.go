package api

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Backend logger that emits log entries as Wails events so the frontend
// LOGS tab can display them alongside frontend console captures.

var (
	blogCtx context.Context
	blogMu  sync.Mutex
)

// InitLogger stores the Wails context for backend log emission.
func InitLogger(ctx context.Context) {
	blogMu.Lock()
	blogCtx = ctx
	blogMu.Unlock()
}

func emitLog(level, msg string) {
	blogMu.Lock()
	ctx := blogCtx
	blogMu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "backend:log", level, msg)
	}
}

// Blogf logs a formatted message at info level.
func Blogf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[info] %s", msg)
	emitLog("info", msg)
}

// Blog logs a plain message at info level.
func Blog(msg string) {
	log.Printf("[info] %s", msg)
	emitLog("info", msg)
}

// BlogErrf logs a formatted error message.
func BlogErrf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[error] %s", msg)
	emitLog("error", msg)
}

// BlogWarnf logs a formatted warning message.
func BlogWarnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[warn] %s", msg)
	emitLog("warn", msg)
}

// BlogDebugf logs a formatted debug message.
func BlogDebugf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[debug] %s", msg)
	emitLog("debug", msg)
}
