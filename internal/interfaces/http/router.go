package httpapi

import (
	"net/http"

	"open-ima/internal/application/chat"
	"open-ima/internal/application/ingest"
	kbapp "open-ima/internal/application/knowledgebase"
	"open-ima/internal/application/port"
	"open-ima/internal/application/reading"
	settingsapp "open-ima/internal/application/settings"
)

// Deps 是路由装配所需的应用用例与端口实现。
type Deps struct {
	KnowledgeBase *kbapp.Service
	Ingest        *ingest.Service
	Chat          *chat.Service
	Reading       *reading.Service
	Settings      *settingsapp.Service
	Store         port.FileStore
}

// NewRouter 注册全部 API 路由与健康检查;静态文件与前端由装配根挂载。
func NewRouter(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	(&knowledgeBaseHandler{service: deps.KnowledgeBase}).register(mux)
	(&mediasHandler{ingest: deps.Ingest, store: deps.Store, maxBytes: 50 << 20}).register(mux)
	(&chatHandler{service: deps.Chat}).register(mux)
	(&readingHandler{reading: deps.Reading}).register(mux)
	(&settingsHandler{service: deps.Settings}).register(mux)
	return mux
}
