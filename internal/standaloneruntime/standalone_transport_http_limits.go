package standaloneruntime

import (
	"io"
	stdhttp "net/http"
	"time"
)

func httpBodyLimits(cfg standaloneHTTPConfig, next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodPost {
			controller := stdhttp.NewResponseController(w)
			// The deadline applies only to uploading the body, never to MCP streams.
			_ = controller.SetReadDeadline(time.Now().Add(cfg.RequestTimeout))
			r.Body = &deadlineBody{
				ReadCloser: stdhttp.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes),
				controller: controller,
			}
		}

		next.ServeHTTP(w, r)
	})
}

type deadlineBody struct {
	io.ReadCloser

	controller *stdhttp.ResponseController
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		_ = b.controller.SetReadDeadline(time.Time{})
	}
	// Leave failed uploads bounded while net/http disposes of their unread body.
	return n, err
}
