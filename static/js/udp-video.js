// UDP Video Stream Handler via WebSocket
// Receives JPEG frames from Go backend's UDP receiver via WebSocket

class UDPVideoStream {
  constructor() {
    this.ws = null;
    this.videoElement = null;
    this.isActive = false;
    this.reconnectAttempts = 0;
    this.maxReconnectAttempts = 5;
    this.reconnectDelay = 2000;
    this.frameCount = 0;
    this.startTime = null;
    this.lastFrameTime = null;
  }

  /**
   * Initialize and start UDP video stream
   * @param {HTMLElement} videoElement - The image element to display frames
   */
  start(videoElement) {
    if (this.isActive) {
      console.log('UDP video stream already active');
      return;
    }

    this.videoElement = videoElement;
    this.isActive = true;
    this.frameCount = 0;
    this.startTime = Date.now();
    this.lastFrameTime = Date.now();
    
    this.connectWebSocket();
    
    if (typeof window.log === 'function') {
      window.log('UDP video stream starting...');
    }
  }

  /**
   * Connect to WebSocket video stream endpoint
   */
  connectWebSocket() {
    if (!this.isActive) return;

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws-video`;
    
    console.log('Connecting to UDP video stream:', wsUrl);
    
    try {
      this.ws = new WebSocket(wsUrl);
      
      this.ws.onopen = () => {
        console.log('UDP video WebSocket connected');
        if (typeof window.log === 'function') {
          window.log('UDP video stream connected');
        }
        this.reconnectAttempts = 0;
      };
      
      this.ws.onmessage = (event) => {
        this.handleFrame(event.data);
      };
      
      this.ws.onerror = (error) => {
        console.error('UDP video WebSocket error:', error);
        if (typeof window.log === 'function') {
          window.log('UDP video stream error');
        }
      };
      
      this.ws.onclose = (event) => {
        console.log('UDP video WebSocket closed:', event.code, event.reason);
        
        if (this.isActive && this.reconnectAttempts < this.maxReconnectAttempts) {
          this.reconnectAttempts++;
          console.log(`Reconnecting UDP video stream (attempt ${this.reconnectAttempts}/${this.maxReconnectAttempts})...`);
          
          if (typeof window.log === 'function') {
            window.log(`Reconnecting video stream (${this.reconnectAttempts}/${this.maxReconnectAttempts})...`);
          }
          
          setTimeout(() => {
            this.connectWebSocket();
          }, this.reconnectDelay * this.reconnectAttempts);
        } else if (this.reconnectAttempts >= this.maxReconnectAttempts) {
          console.error('Max reconnection attempts reached for UDP video stream');
          if (typeof window.log === 'function') {
            window.log('UDP video stream: max reconnection attempts reached');
          }
        }
      };
      
    } catch (error) {
      console.error('Failed to create UDP video WebSocket:', error);
      if (typeof window.log === 'function') {
        window.log('Failed to create UDP video WebSocket: ' + error.message);
      }
    }
  }

  /**
   * Handle incoming video frame
   * @param {string} base64Data - Base64 encoded JPEG frame
   */
  handleFrame(base64Data) {
    if (!this.videoElement || !this.isActive) return;
    
    try {
      this.frameCount++;
      const now = Date.now();
      this.lastFrameTime = now;
      
      // Update video element with new frame
      this.videoElement.src = 'data:image/jpeg;base64,' + base64Data;
      
      // Show video element if hidden
      if (this.videoElement.style.display === 'none') {
        this.videoElement.style.display = 'block';
      }
      
      // Log stats periodically (every 100 frames)
      if (this.frameCount % 100 === 0) {
        const elapsed = (now - this.startTime) / 1000;
        const fps = this.frameCount / elapsed;
        console.log(`UDP video: ${this.frameCount} frames, ${fps.toFixed(1)} fps`);
        
        if (typeof window.log === 'function' && this.frameCount % 500 === 0) {
          window.log(`UDP video: ${this.frameCount} frames received (${fps.toFixed(1)} fps)`);
        }
      }
      
    } catch (error) {
      console.error('Error handling video frame:', error);
    }
  }

  /**
   * Stop UDP video stream
   */
  stop() {
    console.log('Stopping UDP video stream...');
    
    this.isActive = false;
    this.reconnectAttempts = this.maxReconnectAttempts; // Prevent reconnection
    
    if (this.ws) {
      try {
        this.ws.close();
      } catch (error) {
        console.error('Error closing UDP video WebSocket:', error);
      }
      this.ws = null;
    }
    
    // Clear video element
    if (this.videoElement) {
      this.videoElement.src = '';
      this.videoElement.style.display = 'none';
    }
    
    if (typeof window.log === 'function') {
      const elapsed = (Date.now() - this.startTime) / 1000;
      const avgFps = this.frameCount / elapsed;
      window.log(`UDP video stream stopped (${this.frameCount} frames, ${avgFps.toFixed(1)} fps avg)`);
    }
    
    // Reset stats
    this.frameCount = 0;
    this.startTime = null;
    this.lastFrameTime = null;
    this.videoElement = null;
  }

  /**
   * Get current streaming status
   */
  getStatus() {
    return {
      isActive: this.isActive,
      frameCount: this.frameCount,
      fps: this.startTime ? this.frameCount / ((Date.now() - this.startTime) / 1000) : 0,
      latency: this.lastFrameTime ? Date.now() - this.lastFrameTime : 0
    };
  }
}

// Create global instance
window.udpVideoStream = new UDPVideoStream();

// Export for use in other modules
if (typeof module !== 'undefined' && module.exports) {
  module.exports = UDPVideoStream;
}
