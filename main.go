package main

import (
	"log"
	"net/http"
	"strconv"
)

var hub *Hub

func main() {
	// WebSocket Hub 초기화 (for subtitles)
	hub = newHub()
	go hub.run()
	
	// Video Hub 초기화 (for UDP video streaming)
	videoHub = newVideoHub()
	go videoHub.run()
	
	// Start UDP video receiver on port 5004
	go startUDPVideoReceiver(5004)

	// 정적 파일 서버에 캐시 방지 미들웨어 추가
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", noCacheMiddleware(fs))

	// WebSocket 엔드포인트
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/ws-video", handleVideoWebSocket)

	// HTTP 엔드포인트
	http.HandleFunc("/post", handlePost)
	http.HandleFunc("/reset", handleReset)
	http.HandleFunc("/subtitle", handleSubtitle)
	http.HandleFunc("/status", handleStatus)

	port := getenvInt("HTTP_PORT", 8080)
	addr := ":" + strconv.Itoa(port)

	log.Printf("🚀 OMNISENSE Server starting...")
	log.Printf("📍 Server: http://localhost:%d", port)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("HTTP Server error: ", err)
	}
}

// 캐시 방지 미들웨어 - 개발 중에는 캐시 비활성화
func noCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTML, CSS, JS 파일은 캐시 방지
		if isDevResource(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		} else {
			// 이미지 등 정적 리소스는 짧은 캐시 허용
			w.Header().Set("Cache-Control", "public, max-age=300") // 5분
		}
		next.ServeHTTP(w, r)
	})
}

// 개발 리소스 확인 (HTML, CSS, JS)
func isDevResource(path string) bool {
	return path == "/" || 
		path == "/index.html" ||
		len(path) >= 4 && (path[len(path)-4:] == ".css" || 
		                   path[len(path)-3:] == ".js" ||
		                   path[len(path)-5:] == ".html")
}
