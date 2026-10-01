import 'dart:async';
import 'package:flutter/services.dart';

/// Android foreground microphone capture; each event is PCM16, 16 kHz mono.
class AudioCapture {
  static const _methods = MethodChannel('yui/audio');
  static const _events = EventChannel('yui/audio_chunks');

  Stream<Uint8List> get chunks => _events.receiveBroadcastStream().map((event) {
        if (event is Uint8List) return event;
        throw const FormatException('Неизвестный формат аудио');
      });

  Future<void> start() => _methods.invokeMethod<void>('start');
  Future<void> stop() => _methods.invokeMethod<void>('stop');
}
