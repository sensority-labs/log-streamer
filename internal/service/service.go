package service

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
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

func logStreamer(ws *websocket.Conn, logDir, containerId string, tail int) {
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

	// Count the number of lines in the file
	linesCount, err := lineCounter(file)
	if err != nil {
		log.Default().Print(err)
		if err := write(ws, websocket.TextMessage, []byte("Error reading logs for container: "+containerId)); err != nil {
			log.Default().Printf("websocket write error: %v", err)
			return
		}
		return
	}

	// Seek to the beginning of the file after the counting
	if _, err = file.Seek(0, 0); err != nil {
		return
	}

	currentLine := 0
	scanner := bufio.NewScanner(file)

	for {
		for scanner.Scan() {
			currentLine++
			if currentLine <= linesCount-tail {
				continue
			}
			line := scanner.Bytes()
			if err := write(ws, websocket.TextMessage, line); err != nil {
				log.Default().Printf("websocket write error: %v", err)
				return
			}
		}
		// Check for scanning errors (other than EOF)
		if err := scanner.Err(); err != nil {
			log.Default().Print(err)
			if err := write(ws, websocket.TextMessage, []byte("Error reading logs for container: "+containerId)); err != nil {
				log.Default().Printf("websocket write error: %v", err)
				return
			}
			return
		}

		// Wait and try to read new content
		time.Sleep(1 * time.Second)

		// Reset scanner for new content
		if _, err := file.Seek(0, io.SeekCurrent); err != nil {
			return
		}
		// Seek to the current position
		scanner = bufio.NewScanner(file)
	}
}

func serveWs(logDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		containerId := r.PathValue("containerId")
		linesToFetch := 5
		tail := r.URL.Query().Get("tail")
		if tail != "" {
			if parsedLines, err := strconv.Atoi(tail); err == nil {
				linesToFetch = parsedLines
			} else {
				log.Default().Printf("Error parsing tail query parameter, received: %s", tail)
			}
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			var handshakeError websocket.HandshakeError
			if !errors.As(err, &handshakeError) {
				sentry.CaptureException(err)
				log.Fatal(err)
			}
			return
		}

		go logStreamer(ws, logDir, containerId, linesToFetch)
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
