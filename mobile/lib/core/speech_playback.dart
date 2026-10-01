import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:audioplayers/audioplayers.dart';

import 'session_channel.dart';

/// play completes when the audio has finished, not just when it starts.
abstract interface class SpeechOutput {
  Future<void> play(Uint8List wav);
  Future<void> stop();
  Future<void> dispose();
}

class DeviceSpeechOutput implements SpeechOutput {
  final AudioPlayer _player = AudioPlayer();
  late final StreamSubscription<void> _completion = _player.onPlayerComplete
      .listen((_) => _finish(), onError: (Object error, StackTrace stack) {
    final playing = _playing;
    if (playing != null && !playing.isCompleted) {
      playing.completeError(error, stack);
    }
  });
  Completer<void>? _playing;
  Future<void> _commands = Future<void>.value();

  void _finish() {
    final playing = _playing;
    if (playing != null && !playing.isCompleted) playing.complete();
  }

  Future<void> _command(Future<void> Function() run) {
    final next = _commands.then((_) => run());
    _commands = next.catchError((Object _) {});
    return next;
  }

  @override
  Future<void> play(Uint8List wav) async {
    // Subscribe before starting: even a very short chunk can finish quickly.
    _completion;
    final playing = Completer<void>();
    _playing = playing;
    try {
      await Future.wait<void>([
        _command(() => _player.play(BytesSource(wav, mimeType: 'audio/wav'))),
        playing.future,
      ], eagerError: true);
    } finally {
      if (identical(_playing, playing)) _playing = null;
    }
  }

  @override
  Future<void> stop() async {
    try {
      await _command(_player.stop);
    } finally {
      _finish();
    }
  }

  @override
  Future<void> dispose() async {
    try {
      await stop();
    } finally {
      await _completion.cancel();
      await _player.dispose();
    }
  }
}

/// Orders PCM chunks and discards queued speech on interruption/disconnection.
class SpeechPlayback {
  SpeechPlayback({required this.deviceId, SpeechOutput? output})
      : _output = output ?? DeviceSpeechOutput();

  final String deviceId;
  final SpeechOutput _output;
  Future<void> _tail = Future<void>.value();
  int _generation = 0;
  bool _disposed = false;

  Future<void> handleFrame(Frame frame) async {
    if (_disposed) return;
    if (frame.type == 'barge_in' ||
        frame.type == 'connection.closed' ||
        (frame.type == 'session.state' && frame.payload['state'] == 'closed')) {
      await stop();
      return;
    }
    if (frame.type != 'tts.chunk') return;
    final target = frame.payload['device'] as String?;
    if (target != null && target.isNotEmpty && target != deviceId) return;
    final pcm = base64Decode(frame.payload['audio_b64'] as String);
    if (pcm.isEmpty) return;
    final rate = (frame.payload['sample_rate'] as num?)?.toInt() ?? 16000;
    final wav = pcm16Wave(pcm, rate);
    final generation = _generation;
    final next = _tail.catchError((Object _) {}).then((_) async {
      if (!_disposed && generation == _generation) await _output.play(wav);
    });
    _tail = next;
    await next;
  }

  Future<void> stop() {
    _generation++;
    final previous = _tail.catchError((Object _) {});
    final stopping = _output.stop();
    _tail = Future.wait<void>([previous, stopping]).then((_) {});
    return _tail;
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    try {
      await stop();
    } finally {
      await _output.dispose();
    }
  }
}

/// The core sends little-endian mono PCM16; native players need a WAV header.
Uint8List pcm16Wave(Uint8List pcm, int sampleRate) {
  if (pcm.length.isOdd || sampleRate < 8000 || sampleRate > 192000) {
    throw const FormatException('Некорректный формат голосового ответа');
  }
  final wav = Uint8List(44 + pcm.length);
  final header = ByteData.sublistView(wav);
  wav.setRange(0, 4, ascii.encode('RIFF'));
  header.setUint32(4, 36 + pcm.length, Endian.little);
  wav.setRange(8, 16, ascii.encode('WAVEfmt '));
  header.setUint32(16, 16, Endian.little);
  header.setUint16(20, 1, Endian.little);
  header.setUint16(22, 1, Endian.little);
  header.setUint32(24, sampleRate, Endian.little);
  header.setUint32(28, sampleRate * 2, Endian.little);
  header.setUint16(32, 2, Endian.little);
  header.setUint16(34, 16, Endian.little);
  wav.setRange(36, 40, ascii.encode('data'));
  header.setUint32(40, pcm.length, Endian.little);
  wav.setRange(44, wav.length, pcm);
  return wav;
}
