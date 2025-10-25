package floxy

import (
	"time"
)

type EngineOption func(engine *Engine)

func WithEngineCancelInterval(interval time.Duration) EngineOption {
	return func(engine *Engine) {
		engine.cancelWorkerInterval = interval
	}
}

func WithEngineTxManager(txManager TxManager) EngineOption {
	return func(engine *Engine) {
		engine.txManager = txManager
	}
}
