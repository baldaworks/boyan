package standaloneruntime //nolint:testpackage // Exercises status within the package.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestLoggingResponseController(t *testing.T) {
	writer := httptest.NewRecorder()
	handler := httpLoggingMiddleware(zerolog.Nop(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
	}))
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/mcp", nil))

	if !writer.Flushed {
		t.Fatal("response was not flushed")
	}
}

func TestStatusRecorderFirstFinalStatus(t *testing.T) {
	for _, writeBody := range []bool{false, true} {
		writer := httptest.NewRecorder()

		recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
		if writeBody {
			if _, err := recorder.Write([]byte("body")); err != nil {
				t.Fatal(err)
			}
		} else {
			recorder.WriteHeader(http.StatusCreated)
		}

		recorder.WriteHeader(http.StatusInternalServerError)

		if recorder.status != writer.Code {
			t.Errorf("logged %d, sent %d", recorder.status, writer.Code)
		}
	}
}

func TestStatusRecorderFlushCommitsStatus(t *testing.T) {
	writer := httptest.NewRecorder()

	recorder := &statusRecorder{ResponseWriter: writer, status: http.StatusOK}
	requireNoError(t, http.NewResponseController(recorder).Flush(), "%v")

	recorder.WriteHeader(http.StatusInternalServerError)

	if recorder.status != writer.Code {
		t.Errorf("logged %d, sent %d", recorder.status, writer.Code)
	}
}
