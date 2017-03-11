package serve

import (
	"context"
	"net/http"

	"github.com/NYTimes/gziphandler"
	"github.com/dimfeld/httptreemux"
	"github.com/pkg/errors"
)

func (s *Service) setupRoutes() (http.Handler, error) {
	router := httptreemux.New()
	router.HeadCanUseGet = true
	ctxRoot := router.UsingContext()
	ctxRoot.Handler(mGET, "/", handler(root))
	ctxRoot.Handler(mGET, "/_all_dbs", handler(allDBs))
	ctxRoot.Handler(mGET, "/_log", handler(log))
	ctxRoot.Handler(mPUT, "/:db", handler(createDB))
	ctxRoot.Handler(mHEAD, "/:db", handler(dbExists))
	ctxRoot.Handler(mPOST, "/:db/_ensure_full_commit", handler(flush))
	ctxRoot.Handler(mGET, "/_config", handler(getConfig))
	ctxRoot.Handler(mGET, "/_config/:section", handler(getConfigSection))
	ctxRoot.Handler(mGET, "/_config/:section/:key", handler(getConfigItem))
	ctxRoot.Handler(mGET, "/_session", handler(getSession))
	// ctxRoot.Handler(mDELETE, "/:db", handler(destroyDB) )
	// ctxRoot.Handler(http.MethodGet, "/:db", handler(getDB))

	handle := http.Handler(router)
	if s.Config().GetBool("httpd", "enable_compression") {
		level := s.Config().GetInt("httpd", "compression_level")
		if level == 0 {
			level = 8
		}
		gzipHandler, err := gziphandler.NewGzipLevelHandler(int(level))
		if err != nil {
			return nil, errors.Wrapf(err, "invalid httpd.compression_level '%s'", level)
		}
		s.Info("Enabling HTTPD cmpression, level %d", level)
		handle = gzipHandler(handle)
	}
	handle = requestLogger(s, handle)
	handle = authHandler(s, handle)
	return handle, nil
}

func setContext(s *Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, ClientContextKey, s.Client)
		ctx = context.WithValue(ctx, ServiceContextKey, s)
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}