package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

var streamInProgress bool

func handlePost(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	log.Printf("Stream request received")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if streamInProgress {
		log.Printf("Stream blocked - already in progress")
		http.Error(w, "Stream already in progress", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var offer webrtc.SessionDescription
	if err := decode(string(body), &offer); err != nil {
		http.Error(w, "Bad offer", http.StatusBadRequest)
		return
	}
	log.Printf("Received offer")

	// localhost/내부망 접속 감지
	host := r.Host
	isLocalhost := strings.Contains(host, "localhost") ||
		strings.Contains(host, "127.0.0.1") ||
		strings.Contains(host, "::1") ||
		strings.Contains(host, "192.168.") ||
		strings.Contains(host, "10.") ||
		strings.Contains(host, "172.")

	// WebRTC 세션 초기화 (오디오만)
	pc, audioTrack, err := initWebRTCSession(&offer, isLocalhost)
	if err != nil {
		http.Error(w, "WebRTC failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 오디오 UDP 리스너만 초기화 (비디오는 별도 UDP -> WebSocket 경로 사용)
	audioListener, err := initUDPListener("RTP_AUDIO_PORT", 5006, "Audio")
	if err != nil {
		pc.Close()
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// RTP 오디오 전송 시작
	go sendRtpToClient(audioTrack, audioListener)

	// 즉시 정리를 위한 연결 모니터링
	streamInProgress = true
	setupPeerConnection(pc, func() {
		streamInProgress = false
		audioListener.Close()
		pc.Close()
		log.Printf("Stream resources cleaned up")
	})

	// SDP answer 반환
	fmt.Fprint(w, encode(pc.LocalDescription()))
	log.Printf("Stream started (elapsed=%s)", time.Since(start))
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	streamInProgress = false
	log.Printf("Stream state reset by client request (method: %s)", r.Method)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK - Stream state reset"))
}

func handleSubtitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var subtitle SubtitleData
	if err := json.NewDecoder(r.Body).Decode(&subtitle); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 자막을 JSON으로 직렬화하여 WebSocket으로 브로드캐스트
	message, err := json.Marshal(subtitle)
	if err != nil {
		log.Printf("Failed to marshal subtitle: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	hub.broadcast <- message

	log.Printf("Received subtitle: %s [%s] [Speaker %d] %s", subtitle.LangCode, subtitle.Emoji, subtitle.Speaker, subtitle.Text)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := getSystemStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
