import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:yui_companion/core/session_channel.dart';
import 'package:yui_companion/core/speech_playback.dart';

class FakeOutput implements SpeechOutput {
  final played = <Uint8List>[];
  Completer<void>? active;
  int stops = 0;
  bool disposed = false;

  @override
  Future<void> play(Uint8List wav) {
    expect(active, isNull, reason: 'speech chunks must not overlap');
    played.add(wav);
    active = Completer<void>();
    return active!.future;
  }

  void finish() {
    final old = active;
    active = null;
    old?.complete();
  }

  @override
  Future<void> stop() async {
    stops++;
    finish();
  }

  @override
  Future<void> dispose() async {
    disposed = true;
  }
}

Frame audio({String device = 'phone', int rate = 22050}) => Frame('tts.chunk', {
      'device': device,
      'sample_rate': rate,
      'audio_b64': base64Encode([0, 0, 255, 127]),
    });

void main() {
  test('WAV preserves little-endian PCM, rate and duration', () {
    final bytes = pcm16Wave(Uint8List.fromList([0, 0, 255, 127]), 22050);
    final header = ByteData.sublistView(bytes);
    expect(ascii.decode(bytes.sublist(0, 4)), 'RIFF');
    expect(header.getUint32(4, Endian.little), bytes.length - 8);
    expect(header.getUint32(24, Endian.little), 22050);
    expect(header.getUint32(28, Endian.little), 44100);
    expect(header.getUint32(40, Endian.little), 4);
    expect(bytes.sublist(44), [0, 0, 255, 127]);
    expect(() => pcm16Wave(Uint8List(3), 16000), throwsFormatException);
  });

  test('frames play sequentially and ignore another output device', () async {
    final output = FakeOutput();
    final speech = SpeechPlayback(deviceId: 'phone', output: output);
    await speech.handleFrame(audio(device: 'desktop'));
    expect(output.played, isEmpty);
    final first = speech.handleFrame(audio());
    final second = speech.handleFrame(audio(rate: 16000));
    await Future<void>.delayed(Duration.zero);
    expect(output.played.length, 1);
    output.finish();
    await first;
    await Future<void>.delayed(Duration.zero);
    expect(output.played.length, 2);
    output.finish();
    await second;
    await speech.dispose();
  });

  test('interruption drops queued speech and accepts next reply', () async {
    final output = FakeOutput();
    final speech = SpeechPlayback(deviceId: 'phone', output: output);
    final first = speech.handleFrame(audio());
    final queued = speech.handleFrame(audio());
    await Future<void>.delayed(Duration.zero);
    await speech.handleFrame(Frame('barge_in', {}));
    await Future.wait([first, queued]);
    expect(output.played.length, 1);
    final next = speech.handleFrame(audio());
    await Future<void>.delayed(Duration.zero);
    expect(output.played.length, 2);
    await speech.handleFrame(Frame('connection.closed', {}));
    await next;
    await speech.dispose();
    expect(output.disposed, isTrue);
    await speech.handleFrame(audio());
    expect(output.played.length, 2);
  });
}
