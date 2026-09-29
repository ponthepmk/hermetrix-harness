class HermetrixVoiceCapture extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ratio = sampleRate / 16000;
    this.position = 0;
    this.frame = new Int16Array(640);
    this.index = 0;
  }

  process(inputs) {
    const samples = inputs[0]?.[0];
    if (!samples) return true;
    for (let i = 0; i < samples.length; i++) {
      this.position++;
      if (this.position < this.ratio) continue;
      this.position -= this.ratio;
      this.frame[this.index++] = Math.max(-32768, Math.min(32767, Math.round(samples[i] * 32767)));
      if (this.index === 640) {
		this.port.postMessage(this.frame.buffer, [this.frame.buffer]);
        this.frame = new Int16Array(640);
        this.index = 0;
      }
    }
    return true;
  }
}

registerProcessor("hermetrix-voice-capture", HermetrixVoiceCapture);
