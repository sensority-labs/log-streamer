package service

import (
	"bufio"
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed writing the file to the client.
	writeWait = 10 * time.Second
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // TODO: Add origin check
		},
	}
)

func write(ws *websocket.Conn, msgType int, data []byte) error {
	err := ws.SetWriteDeadline(time.Now().Add(writeWait))
	if err != nil {
		return err
	}
	err = ws.WriteMessage(msgType, data)
	if err != nil {
		return err
	}
	return nil
}

func logStreamer(ws *websocket.Conn, logDir, containerId string) {
	filename := path.Join(logDir, containerId, containerId+"-json.log")
	file, err := os.Open(filename)
	if err != nil {
		sentry.CaptureException(err)
		log.Default().Print(err)
		if err := write(ws, websocket.TextMessage, []byte("Error reading logs for container: "+containerId)); err != nil {
			log.Default().Printf("websocket write error: %v", err)
			return
		}
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Default().Print(err)
		}
	}(file)

	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err.Error() == "EOF" {
				time.Sleep(1 * time.Second)
				continue
			}
			log.Default().Print(err)
			if err := write(ws, websocket.TextMessage, []byte("Log read error")); err != nil {
				log.Default().Printf("websocket write error: %v", err)
				return
			}
		}
		if err := write(ws, websocket.TextMessage, line); err != nil {
			log.Default().Printf("websocket write error: %v", err)
			return
		}
	}
}

func serveWs(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		containerId := r.PathValue("containerId")
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			var handshakeError websocket.HandshakeError
			if !errors.As(err, &handshakeError) {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
			return
		}

		go logStreamer(ws, logDir, containerId)
	}
}

func Start() error {
	logDir := os.Getenv("LOG_DIR")
	if logDir == "" {
		logDir = "/var/lib/docker/containers/"
	}
	http.HandleFunc("/logs/{containerId}", serveWs(logDir))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8088"
	}
	addr := ":" + port
	log.Default().Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		return err
	}
	return nil
}
