package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"syscall"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// ---------- WebRTC ----------

func initWebRTCSession(offer *webrtc.SessionDescription, isLocalhost bool) (*webrtc.PeerConnection, *webrtc.TrackLocalStaticRTP, error) {
	log.Printf("Initializing WebRTC session (localhost: %t) - Audio only", isLocalhost)

	// localhost/내부망 접속인 경우 STUN 서버 없이 직접 연결
	var iceServers []webrtc.ICEServer
	if !isLocalhost {
		iceServers = []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}
	}

	// localhost 환경에 최적화된 설정
	config := webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyAll,
		BundlePolicy:       webrtc.BundlePolicyMaxBundle,
	}

	// localhost인 경우 추가 최적화 설정
	var s webrtc.SettingEngine
	if isLocalhost {
		// 로컬 연결용 포트 범위 제한
		s.SetEphemeralUDPPortRange(50000, 50100)
		// 네트워크 인터페이스 필터링
		s.SetIncludeLoopbackCandidate(true)
	}

	var pc *webrtc.PeerConnection
	var err error
	if isLocalhost {
		api := webrtc.NewAPI(webrtc.WithSettingEngine(s))
		pc, err = api.NewPeerConnection(config)
	} else {
		pc, err = webrtc.NewPeerConnection(config)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("NewPeerConnection: %w", err)
	}

	// 오디오 트랙만 생성 및 추가 (비디오는 UDP로 전송)
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion")
	if err != nil {
		pc.Close()
		return nil, nil, fmt.Errorf("audio track: %w", err)
	}

	if _, err = pc.AddTrack(audioTrack); err != nil {
		pc.Close()
		return nil, nil, fmt.Errorf("add audio track: %w", err)
	}

	// SDP 처리
	if err := pc.SetRemoteDescription(*offer); err != nil {
		pc.Close()
		return nil, nil, fmt.Errorf("SetRemoteDescription: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return nil, nil, fmt.Errorf("CreateAnswer: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return nil, nil, fmt.Errorf("SetLocalDescription: %w", err)
	}

	// ICE 수집 완료 대기
	<-webrtc.GatheringCompletePromise(pc)
	return pc, audioTrack, nil
}

// ---------- UDP(RTP) ----------

func initUDPListener(portEnv string, defaultPort int, label string) (*net.UDPConn, error) {
	addr := &net.UDPAddr{
		IP:   net.ParseIP(getenvStr("RTP_BIND_IP", "0.0.0.0")),
		Port: getenvInt(portEnv, defaultPort),
	}

	lc := &net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var err error
			c.Control(func(fd uintptr) {
				err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			})
			return err
		},
	}

	conn, err := lc.ListenPacket(context.Background(), "udp", addr.String())
	if err != nil {
		return nil, fmt.Errorf("%s UDP port %d in use", label, addr.Port)
	}

	udpConn := conn.(*net.UDPConn)
	bufSize := 2 * 1024 * 1024
	udpConn.SetReadBuffer(bufSize)
	udpConn.SetWriteBuffer(bufSize)

	log.Printf("%s UDP listener ready on %s", label, addr)
	return udpConn, nil
}

func sendRtpToClient(track *webrtc.TrackLocalStaticRTP, listener *net.UDPConn) {
	defer listener.Close()

	buf := make([]byte, 2048)
	var pkt rtp.Packet

	for {
		n, _, err := listener.ReadFrom(buf)
		if err != nil {
			return // 연결 종료
		}

		if pkt.Unmarshal(buf[:n]) == nil {
			track.WriteRTP(&pkt)
		}
	}
}

func setupPeerConnection(pc *webrtc.PeerConnection, cleanup func()) {
	var once bool

	doCleanup := func(reason string) {
		if !once {
			once = true
			log.Printf("Connection ended: %s", reason)
			cleanup()
		}
	}

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("Connection: %s", s.String())
		switch s {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed,
			webrtc.PeerConnectionStateDisconnected:
			doCleanup(s.String())
		}
	})

	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		log.Printf("ICE: %s", s.String())
		switch s {
		case webrtc.ICEConnectionStateFailed,
			webrtc.ICEConnectionStateClosed,
			webrtc.ICEConnectionStateDisconnected:
			doCleanup(s.String())
		}
	})

}
