package sockjs

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

var (
	cFrame              = closeFrame(2010, "Another connection still open")
	xhrStreamingPrelude = strings.Repeat("h", 2048)
)

func (h *handler) xhrSend(rw http.ResponseWriter, req *http.Request) {
	if req.Body == nil {
		httpError(rw, "Payload expected.", http.StatusInternalServerError)
		return
	}
	var messages []string
	err := json.NewDecoder(req.Body).Decode(&messages)
	if err == io.EOF {
		httpError(rw, "Payload expected.", http.StatusInternalServerError)
		return
	}
	if _, ok := err.(*json.SyntaxError); ok || err == io.ErrUnexpectedEOF {
		httpError(rw, "Broken JSON encoding.", http.StatusInternalServerError)
		return
	}
	sessionID, err := h.parseSessionID(req.URL)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	h.sessionsMux.Lock()
	defer h.sessionsMux.Unlock()
	if sess, ok := h.sessions[sessionID]; !ok {
		http.NotFound(rw, req)
	} else {
		_ = sess.accept(messages...)                                 // TODO(igm) reponse with SISE in case of err?
		rw.Header().Set("content-type", "text/plain; charset=UTF-8") // Ignored by net/http (but protocol test complains), see https://code.google.com/p/go/source/detail?r=902dc062bff8
		rw.WriteHeader(http.StatusNoContent)
	}
}

type xhrFrameWriter struct{}

func (*xhrFrameWriter) write(w io.Writer, frame string) (int, error) {
	return fmt.Fprintf(w, "%s\n", frame)
}

func (h *handler) xhrPoll(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("content-type", "application/javascript; charset=UTF-8")
	sess, _ := h.sessionByRequest(req) // TODO(igm) add err handling, although err should not happen as handler should not pass req in that case
	receiver := newHTTPReceiver(rw, 1, new(xhrFrameWriter))
	if err := sess.attachReceiver(receiver); err != nil {
		receiver.sendFrame(cFrame)
		receiver.close()
		return
	}

	select {
	case <-receiver.doneNotify():
	case <-receiver.interruptedNotify():
	}
}

func (h *handler) xhrStreaming(rw http.ResponseWriter, req *http.Request) {
	log.Println("begin xhrstreaming")
	defer log.Println("end xhrstreaming")
	rw.Header().Set("content-type", "application/javascript; charset=UTF-8")
	fmt.Fprintf(rw, "%s\n", xhrStreamingPrelude)
	log.Println("before flush")
	rw.(http.Flusher).Flush()
	log.Println("after flush")

	sess, _ := h.sessionByRequest(req)
	receiver := newHTTPReceiver(rw, h.options.ResponseLimit, new(xhrFrameWriter))

	log.Println("before attach")
	if err := sess.attachReceiver(receiver); err != nil {
		receiver.sendFrame(cFrame)
		receiver.close()
		return
	}
	log.Println("after attach")

	log.Println("before select")
	select {
	case <-receiver.doneNotify():
	case <-receiver.interruptedNotify():
	}
	log.Println("after select")
}
