// WebRTC 스트리밍 관리

// 전역 변수 - window 객체에 직접 할당
window.pc = null;
window.isStreaming = false;

// 로깅 함수 - window 객체에 할당하여 전역에서 접근 가능
window.log = msg => {
  const timestamp = new Date().toLocaleTimeString();
  const logsElement = document.getElementById('logs');
  if (logsElement) {
    logsElement.innerHTML += `[${timestamp}] ${msg}<br>`;
    logsElement.scrollTop = logsElement.scrollHeight;
  }
}

// PeerConnection 초기화 함수 - window 객체에 할당
window.initializePeerConnection = function() {
  // localhost 접속 감지 (더 정확한 감지)
  const hostname = window.location.hostname;
  const isLocalhost = hostname === 'localhost' || 
                     hostname === '127.0.0.1' ||
                     hostname === '::1' ||
                     hostname.startsWith('192.168.') ||
                     hostname.startsWith('10.') ||
                     hostname.startsWith('172.');
  
  // localhost/내부망인 경우 STUN 서버 없이 직접 연결
  const config = {
    iceServers: isLocalhost ? [] : [
      { urls: 'stun:stun.l.google.com:19302' }
    ],
    iceCandidatePoolSize: isLocalhost ? 0 : 10,
    bundlePolicy: 'max-bundle',
    rtcpMuxPolicy: 'require',
    iceTransportPolicy: 'all'
  };
  
  log(isLocalhost ? `Local network detected (${hostname}) - using direct connection` : `Remote connection (${hostname}) - using STUN server`);

  const newPc = new RTCPeerConnection(config);

  newPc.ontrack = function (event) {
    var el = document.createElement(event.track.kind);
    el.srcObject = event.streams[0];
    el.autoplay = true;
    el.controls = false;

    // Only handle audio track - video is now handled by UDP stream
    if (event.track.kind === 'audio') {
      document.body.appendChild(el);
    }
    
    updateStreamStatus('오디오 스트림이 연결되었습니다');
  };

  newPc.oniceconnectionstatechange = e => {
    log(`ICE Connection state: ${newPc.iceConnectionState}`);
    
    // 연결 상태에 따라 UI 업데이트 함수 호출
    if (newPc.iceConnectionState === 'connected') {
      log('WebRTC connection established successfully');
      updateStreamStatus(window.t('msg_streaming_complete'));
      if (typeof window.announceToScreenReader === 'function') {
        window.announceToScreenReader(window.t('msg_streaming_connected'));
      }
      // 연결 통계 로깅 시작
      logConnectionStats();
    } else if (newPc.iceConnectionState === 'disconnected') {
      log('WebRTC connection disconnected');
      updateStreamStatus(window.t('msg_streaming_disconnected'));
    } else if (newPc.iceConnectionState === 'failed') {
      log('WebRTC connection failed');
      updateStreamStatus(window.t('msg_streaming_failed'));
      
      // 실패 원인 분석
      analyzeConnectionFailure().then(reason => {
        const errorMsg = `연결 실패: ${reason}`;
        showErrorMessage(errorMsg);
      });
    } else if (newPc.iceConnectionState === 'checking') {
      log('Checking ICE connection...');
      updateStreamStatus(window.t('msg_streaming_checking'));
    }
  };
  
  // ICE candidate 로깅
  newPc.onicecandidate = event => {
    if (event.candidate) {
      const candidate = event.candidate;
      log(`ICE Candidate: ${candidate.type} ${candidate.protocol} ${candidate.address || 'N/A'}:${candidate.port || 'N/A'}`);
      
      // 연결 가능한 후보가 있는지 확인
      if (candidate.type === 'host' || candidate.type === 'srflx') {
        log(`Valid candidate found: ${candidate.type}`);
      }
    } else {
      log('All ICE candidates have been sent');
    }
  };
  
  // ICE gathering state 모니터링
  newPc.onicegatheringstatechange = () => {
    log(`ICE gathering state: ${newPc.iceGatheringState}`);
  };
  
  // 연결 상태 감시
  newPc.onconnectionstatechange = () => {
    log(`Connection state changed: ${newPc.connectionState}`);
  };

  // Offer to receive only audio track (video is now UDP-based)
  newPc.addTransceiver('audio', {'direction': 'recvonly'});
  newPc.createOffer().then(d => newPc.setLocalDescription(d)).catch(log);

  return newPc;
}

// WebRTC 연결 정리를 위한 함수 - window 객체에 할당
window.cleanupWebRTC = function() {
  if (!window.pc) return;
  
  try {
    log('즉시 WebRTC 연결 정리 시작...');
    
    // Stop UDP video stream
    if (window.udpVideoStream) {
      window.udpVideoStream.stop();
    }
    
    // 모든 트랙과 미디어 스트림 즉시 정리
    const senders = pc.getSenders();
    senders.forEach(sender => {
      if (sender.track) {
        sender.track.stop();
      }
    });
    
    const receivers = pc.getReceivers();
    receivers.forEach(receiver => {
      if (receiver.track) {
        receiver.track.stop();
      }
    });
    
    // 모든 이벤트 리스너 제거
    window.pc.oniceconnectionstatechange = null;
    window.pc.ontrack = null;
    window.pc.onicecandidate = null;
    window.pc.onconnectionstatechange = null;
    window.pc.ondatachannel = null;
    window.pc.onicegatheringstatechange = null;
    window.pc.onsignalingstatechange = null;
    
    // 연결 즉시 종료
    window.pc.close();
    window.pc = null;
    
    // 비디오 엘리먼트 정리
    const videoPlayer = document.getElementById('remoteVideo');
    if (videoPlayer) {
      const videos = videoPlayer.getElementsByTagName('video');
      Array.from(videos).forEach(video => {
        if (video.srcObject) {
          const tracks = video.srcObject.getTracks();
          tracks.forEach(track => track.stop());
          video.srcObject = null;
        }
        video.remove();
      });
      
      const audios = videoPlayer.getElementsByTagName('audio');
      Array.from(audios).forEach(audio => {
        if (audio.srcObject) {
          const tracks = audio.srcObject.getTracks();
          tracks.forEach(track => track.stop());
          audio.srcObject = null;
        }
        audio.remove();
      });
    }
    
    // 서버에 즉시 리셋 요청 (포트 해제)
    fetch('/reset', {
      method: 'POST',
      keepalive: true
    }).catch(e => console.log('Reset request sent:', e));
    
    // 상태 초기화
    window.isStreaming = false;
    if (typeof window.updateUIForStreamingState === 'function') {
      window.updateUIForStreamingState(false);
    }
    
    window.log('WebRTC 연결과 포트가 즉시 정리되었습니다');
  } catch (e) {
    console.error('Error cleaning up WebRTC:', e);
    // 에러가 발생해도 서버 리셋은 시도
    fetch('/reset', { method: 'POST', keepalive: true }).catch(() => {});
  }
}

// 스트리밍 시작 함수 - window 객체에 할당
window.startStreaming = function() {
  window.log('Initiating streaming connection...');
  
  // 스트리밍 상태 활성화 및 UI 업데이트
  window.isStreaming = true;
  if (typeof window.updateUIForStreamingState === 'function') {
    window.updateUIForStreamingState(true);
  }
  
  if (typeof window.announceToScreenReader === 'function') {
    window.announceToScreenReader(window.t('msg_streaming_start'));
  }
  
  // Start UDP video stream
  if (window.udpVideoStream) {
    const videoPlayer = document.getElementById('remoteVideo');
    if (videoPlayer) {
      // Clear placeholder and create img element for UDP video
      videoPlayer.innerHTML = '';
      const videoImg = document.createElement('img');
      videoImg.id = 'udpVideoFrame';
      videoImg.style.width = '100%';
      videoImg.style.height = '100%';
      videoImg.style.objectFit = 'contain';
      videoImg.style.display = 'none'; // Hidden until first frame arrives
      videoPlayer.appendChild(videoImg);
      
      window.udpVideoStream.start(videoImg);
      window.log('UDP video stream initialized');
    }
  }
  
  // ICE 후보 수집이 완료될 때까지 대기
  if (window.pc.iceGatheringState === 'gathering') {
    window.log('Waiting for ICE gathering to complete...');
    window.pc.addEventListener('icegatheringstatechange', function handler() {
      if (window.pc.iceGatheringState === 'complete') {
        window.pc.removeEventListener('icegatheringstatechange', handler);
        sendOfferToServer();
      }
    });
  } else {
    sendOfferToServer();
  }
}

function sendOfferToServer() {
  fetch('/post', {
      method: 'POST',
      // 현재 window.pc.localDescription을 사용
      body: btoa(JSON.stringify(window.pc.localDescription))
  })
  .then(response => {
    if (!response.ok) {
      return response.text().then(text => {
        throw new Error(text || `HTTP error! status: ${response.status}`);
      });
    }
    return response.text();
  })
  .then(data => {
    window.log('Received response from server');
    try {
      window.pc.setRemoteDescription(new RTCSessionDescription(JSON.parse(atob(data))));
    } catch (err) {
      throw new Error(`Failed to set remote description: ${err.message}`);
    }
  })
  .then(() => {
    window.log('Remote description set successfully');
    // 연결 상태 모니터링 시작
    monitorConnectionHealth();
  })
  .catch(error => {
    let errorMsg;
    
    // 특정 에러 메시지 처리
    if (error.message.includes('Stream already in progress')) {
      errorMsg = window.t('msg_already_streaming');
    } else if (error.message.includes('Failed to fetch')) {
      errorMsg = window.t('msg_network_error');
    } else {
      errorMsg = `${window.t('msg_error_occurred')}: ${error.message}`;
    }
    
    // 에러 표시 및 로깅
    showErrorMessage(errorMsg);
    window.log(`Error: ${error.message}`);
    if (typeof window.announceToScreenReader === 'function') {
      window.announceToScreenReader(`${window.t('msg_error_occurred')}: ${errorMsg}`);
    }
    console.error('Error:', error);
    
    // UI 상태 초기화
    if (typeof window.updateUIForStreamingState === 'function') {
      window.updateUIForStreamingState(false);
    }
  });
}

// 연결 통계 로깅
function logConnectionStats() {
  if (!window.pc) return;
  
  window.pc.getStats().then(stats => {
    stats.forEach((report) => {
      if (report.type === 'inbound-rtp' && report.kind === 'video') {
        window.log(`Video: ${report.framesPerSecond || 0} fps, ` +
            `${report.bytesReceived || 0} bytes received, ` +
            `${report.packetsLost || 0} packets lost`);
      }
      if (report.type === 'candidate-pair' && report.state === 'succeeded') {
        window.log(`Connection: ${report.localCandidateId} -> ${report.remoteCandidateId}, ` +
            `RTT: ${report.currentRoundTripTime ? (report.currentRoundTripTime * 1000).toFixed(0) : 'N/A'}ms`);
      }
    });
  });
}

// 연결 실패 원인 분석
async function analyzeConnectionFailure() {
  if (!window.pc) return '알 수 없는 오류';
  
  try {
    const stats = await window.pc.getStats();
    let hasHostCandidate = false;
    let hasServerReflexive = false;
    let failedPairs = 0;
    
    stats.forEach((report) => {
      if (report.type === 'local-candidate') {
        if (report.candidateType === 'host') hasHostCandidate = true;
        if (report.candidateType === 'srflx') hasServerReflexive = true;
      }
      if (report.type === 'candidate-pair' && report.state === 'failed') {
        failedPairs++;
      }
    });
    
    // 원인 분석
    if (!hasHostCandidate) {
      return '네트워크 인터페이스를 찾을 수 없습니다.';
    } else if (failedPairs > 0) {
      return `${failedPairs}개의 연결 시도가 실패했습니다. 방화벽이나 NAT 설정을 확인해주세요.`;
    } else {
      return '서버와 연결할 수 없습니다.';
    }
  } catch (error) {
    return '연결 상태를 분석할 수 없습니다.';
  }
}

// 연결 상태 건강도 모니터링
function monitorConnectionHealth() {
    const checkInterval = 60000;
    const healthCheckInterval = setInterval(() => {
    if (!window.pc || window.pc.connectionState === 'closed') {
      clearInterval(healthCheckInterval);
      return;
    }
    
    // 연결 통계 확인
    window.pc.getStats().then(stats => {
      let hasActiveConnection = false;
      let packetsReceived = 0;
      let lastPacketsReceived = 0;
      
      stats.forEach((report) => {
        if (report.type === 'candidate-pair' && report.state === 'succeeded') {
          hasActiveConnection = true;
        }
        if (report.type === 'inbound-rtp' && report.kind === 'video') {
          packetsReceived = report.packetsReceived || 0;
        }
      });
      
      // 연결은 있지만 패킷을 받지 못하는 경우
      if (hasActiveConnection && packetsReceived === lastPacketsReceived && packetsReceived > 0) {
        window.log('Warning: Connection is active but no new packets received');
      }
      
      lastPacketsReceived = packetsReceived;
    });
  }, checkInterval);
}

window.updateStreamStatus = function(message) {
  const statusElement = document.getElementById('stream-status');
  if (statusElement) {
    statusElement.textContent = message;
  }
}

window.showErrorMessage = function(message) {
  window.log('ERROR: ' + message);
  window.updateStreamStatus(message);
  
  const existingError = document.querySelector('.error-message');
  if (existingError) existingError.remove();
  
  const errorBox = document.createElement('div');
  errorBox.className = 'error-message';
  errorBox.textContent = message;
  
  const videoContainer = document.querySelector('.video-container');
  if (videoContainer) {
    videoContainer.appendChild(errorBox);
    setTimeout(() => {
      errorBox.remove();
    }, 5000);
  }
  
  if (typeof window.announceToScreenReader === 'function') {
    window.announceToScreenReader(message);
  }
}

// Export functions and variables for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    initializePeerConnection,
    cleanupWebRTC,
    startStreaming,
    getPeerConnection: () => pc,
    isStreaming: () => isStreaming,
    setIsStreaming: (value) => { isStreaming = value; }
  };
}

// Make variables available globally
window.pc = pc;
window.isStreaming = isStreaming;
