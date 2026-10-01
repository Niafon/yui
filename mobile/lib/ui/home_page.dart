import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:permission_handler/permission_handler.dart';

import '../core/audio_capture.dart';
import '../core/core_client.dart';
import '../core/pairing.dart';
import '../core/photo_capture.dart';
import '../core/session_channel.dart';
import '../core/speech_playback.dart';

/// The conversation screen. Sensor state is always visible: the owner can see
/// at a glance whether the microphone, the camera or an external API is in use
/// (CL-003).
class HomePage extends StatefulWidget {
  const HomePage({required this.pairing, super.key});

  final Pairing pairing;

  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  late final CoreClient _client = CoreClient(
    baseUrl: widget.pairing.baseUrl,
    token: widget.pairing.token,
  );

  SessionChannel? _channel;
  StreamSubscription<Frame>? _frames;
  late final SpeechPlayback _speech =
      SpeechPlayback(deviceId: widget.pairing.deviceId);
  final AudioCapture _capture = AudioCapture();
  final PhotoCapture _photo = PhotoCapture();
  final TextEditingController _message = TextEditingController();
  String? _sessionId;
  bool _cameraBusy = false;
  StreamSubscription<Uint8List>? _audioChunks;
  Timer? _recordingLimit;
  bool _recording = false;
  bool _recordingBusy = false;
  int _recordedBytes = 0;
  final List<({String who, String text})> _turns = [];
  String _state = 'не подключено';
  String _runtimeSummary = 'AI: auto';
  String? _error;

  @override
  void initState() {
    super.initState();
    _start();
  }

  Future<void> _start() async {
    try {
      final session = await _client.startSession();
      if (!mounted) return;
      final sessionId = session['id'] as String;
      final channel = SessionChannel(
        baseUrl: widget.pairing.baseUrl,
        token: widget.pairing.token,
        sessionId: sessionId,
      );
      _frames = channel.frames.listen(_onFrame, onError: (Object e) {
        if (mounted) setState(() => _error = '$e');
      });
      await channel.connect();
      if (!mounted) {
        await channel.dispose();
        return;
      }
      setState(() {
        _channel = channel;
        _sessionId = sessionId;
        _state = 'готова';
      });
    } catch (error) {
      // The client never pretends the AI is available (CL-012).
      if (mounted) setState(() => _error = 'Компьютер недоступен: $error');
    }
  }

  void _sendMessage() {
    final text = _message.text.trim();
    if (text.isEmpty || _channel == null) return;
    _channel!.sendText(text);
    _message.clear();
    setState(() => _turns.add((who: 'Вы', text: text)));
  }

  Future<void> _capturePhoto() async {
    final sessionId = _sessionId;
    if (sessionId == null || _cameraBusy) return;
    setState(() => _cameraBusy = true);
    try {
      final jpeg = await _photo.capture();
      if (jpeg == null || jpeg.isEmpty || !mounted) return;
      final question = _message.text.trim().isEmpty
          ? 'Что изображено на этом фото?'
          : _message.text.trim();
      final result = await _client.sendVisionFrame(
          sessionId, base64Encode(jpeg), question);
      if (!mounted) return;
      _message.clear();
      setState(() {
        _turns.add((who: 'Вы · фото', text: question));
        _turns
            .add((who: 'Юи · зрение', text: '${result['description'] ?? ''}'));
      });
    } catch (error) {
      if (mounted) {
        setState(() => _error = 'Не удалось обработать фото: $error');
      }
    } finally {
      if (mounted) setState(() => _cameraBusy = false);
    }
  }

  Future<void> _toggleRecording() async {
    if (_recordingBusy || _channel == null) return;
    setState(() => _recordingBusy = true);
    try {
      if (_recording) {
        await _stopRecording();
      } else {
        final permission = await Permission.microphone.request();
        if (!permission.isGranted) {
          throw StateError(
              'Разрешите доступ к микрофону в настройках телефона');
        }
        _recordedBytes = 0;
        _audioChunks = _capture.chunks.listen((chunk) {
          if (!_recording || chunk.isEmpty) return;
          _recordedBytes += chunk.length;
          _channel?.sendAudio(chunk);
        }, onError: (Object error) {
          if (mounted) setState(() => _error = 'Микрофон: $error');
        });
        setState(() => _recording = true);
        await _capture.start();
        if (!mounted) {
          await _capture.stop();
          return;
        }
        _channel?.setMicrophone(muted: false);
        _recordingLimit = Timer(const Duration(seconds: 50), () {
          if (mounted && _recording) unawaited(_toggleRecording());
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = 'Не удалось записать голос: $error');
      await _audioChunks?.cancel();
      _audioChunks = null;
      if (_recording) {
        await _capture.stop();
        _channel?.clearAudio();
        setState(() => _recording = false);
      }
    } finally {
      if (mounted) setState(() => _recordingBusy = false);
    }
  }

  Future<void> _stopRecording({bool discard = false}) async {
    _recordingLimit?.cancel();
    await _capture.stop();
    await _audioChunks?.cancel();
    _audioChunks = null;
    if (discard || _recordedBytes == 0) {
      _channel?.clearAudio();
      if (!discard && mounted) {
        setState(() => _error = 'Звук не был записан. Попробуйте ещё раз.');
      }
    } else {
      _channel?.endUtterance();
    }
    _channel?.setMicrophone(muted: true);
    if (mounted) setState(() => _recording = false);
  }

  void _onFrame(Frame frame) {
    if (!mounted) return;
    unawaited(_speech.handleFrame(frame).catchError((Object error) {
      if (mounted) {
        setState(() => _error = 'Не удалось воспроизвести ответ: $error');
      }
    }));
    switch (frame.type) {
      case 'session.state':
        setState(() => _state = '${frame.payload['state']}');
      case 'transcript.final':
        setState(
            () => _turns.add((who: 'Вы', text: '${frame.payload['text']}')));
      case 'turn.done':
        setState(
            () => _turns.add((who: 'Юи', text: '${frame.payload['text']}')));
      case 'proactive.reminder':
        setState(() =>
            _turns.add((who: 'Напоминание', text: '${frame.payload['text']}')));
      case 'inference.decision':
        setState(() {
          final model = frame.payload['model_family'] ??
              frame.payload['model'] ??
              frame.payload['provider_id'];
          final backend = frame.payload['backend'] ?? 'auto';
          _runtimeSummary = 'AI: $model · $backend';
        });
      case 'error':
        setState(() =>
            _error = '${frame.payload['stage']}: ${frame.payload['error']}');
      case 'connection.closed':
        setState(() => _state = 'соединение потеряно');
    }
  }

  Future<void> _showInferenceSettings() async {
    try {
      final status = await _client.inferenceStatus();
      final prefs =
          Map<String, dynamic>.from(status['preferences'] as Map? ?? const {});
      final rawModels = status['models'] as List? ?? const [];
      final families = rawModels
          .whereType<Map>()
          .where((m) => m['kind'] == 'llm')
          .map((m) => '${m['model_family'] ?? m['model'] ?? ''}')
          .where((v) => v.isNotEmpty)
          .toSet()
          .toList()
        ..sort();

      var mode = '${prefs['mode'] ?? 'auto'}';
      var lockedModel = '${prefs['locked_model'] ?? ''}';
      var allowDowngrade = prefs['allow_auto_downgrade'] != false;
      var minimumQuality = (prefs['minimum_quality'] as num?)?.toDouble() ?? 55;

      if (!mounted) return;
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (sheetContext) => StatefulBuilder(
          builder: (context, setSheetState) => SafeArea(
            child: Padding(
              padding: EdgeInsets.fromLTRB(
                  20, 20, 20, 20 + MediaQuery.viewInsetsOf(context).bottom),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text('AI Runtime',
                      style: Theme.of(context).textTheme.titleLarge),
                  const SizedBox(height: 16),
                  DropdownButtonFormField<String>(
                    initialValue: mode,
                    decoration: const InputDecoration(labelText: 'Режим'),
                    items: const [
                      DropdownMenuItem(value: 'auto', child: Text('Auto')),
                      DropdownMenuItem(
                          value: 'max_quality', child: Text('Max quality')),
                      DropdownMenuItem(
                          value: 'balanced', child: Text('Balanced')),
                      DropdownMenuItem(value: 'gaming', child: Text('Gaming')),
                      DropdownMenuItem(value: 'manual', child: Text('Manual')),
                    ],
                    onChanged: (v) =>
                        v == null ? null : setSheetState(() => mode = v),
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    initialValue:
                        families.contains(lockedModel) ? lockedModel : '',
                    decoration: const InputDecoration(labelText: 'Model lock'),
                    items: [
                      const DropdownMenuItem(
                          value: '', child: Text('Автовыбор')),
                      ...families.map(
                          (f) => DropdownMenuItem(value: f, child: Text(f))),
                    ],
                    onChanged: (v) =>
                        setSheetState(() => lockedModel = v ?? ''),
                  ),
                  const SizedBox(height: 12),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Разрешить Auto понижать модель'),
                    value: allowDowngrade,
                    onChanged: (v) => setSheetState(() => allowDowngrade = v),
                  ),
                  Text('Минимальное качество: ${minimumQuality.round()}'),
                  Slider(
                    min: 1,
                    max: 100,
                    divisions: 99,
                    value: minimumQuality < 1.0
                        ? 1.0
                        : (minimumQuality > 100.0 ? 100.0 : minimumQuality),
                    onChanged: (v) => setSheetState(() => minimumQuality = v),
                  ),
                  const SizedBox(height: 8),
                  FilledButton(
                    onPressed: () async {
                      if (mode == 'manual' &&
                          lockedModel.isEmpty &&
                          families.isNotEmpty) {
                        lockedModel = families.first;
                      }
                      final updated = await _client.updateInferencePreferences({
                        'mode': mode,
                        'locked_model': lockedModel,
                        'allow_auto_downgrade': allowDowngrade,
                        'minimum_quality': minimumQuality.round(),
                      });
                      final last = updated['last_decision'] as Map?;
                      if (last != null && mounted) {
                        setState(() {
                          _runtimeSummary =
                              'AI: ${last['model_family'] ?? last['model'] ?? last['provider_id']} · ${last['backend'] ?? 'auto'}';
                        });
                      }
                      if (sheetContext.mounted) {
                        Navigator.of(sheetContext).pop();
                      }
                    },
                    child: const Text('Применить'),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
    } catch (error) {
      if (mounted) setState(() => _error = 'AI Runtime: $error');
    }
  }

  @override
  void dispose() {
    _recordingLimit?.cancel();
    if (_recording) unawaited(_capture.stop());
    unawaited(_audioChunks?.cancel());
    _message.dispose();
    unawaited(_frames?.cancel());
    unawaited(_speech.dispose());
    _channel?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('Юи · $_state'),
        actions: [
          IconButton(
            tooltip: 'Прервать ответ',
            icon: const Icon(Icons.stop),
            onPressed: _channel == null
                ? null
                : () {
                    _channel!.cancel();
                    unawaited(_speech.stop().catchError((Object error) {
                      if (mounted) {
                        setState(() =>
                            _error = 'Не удалось остановить голос: $error');
                      }
                    }));
                  },
          ),
          IconButton(
            tooltip: 'AI Runtime',
            icon: const Icon(Icons.tune),
            onPressed: _showInferenceSettings,
          ),
          IconButton(
            tooltip: _cameraBusy ? 'Камера открыта' : 'Сделать фото',
            icon: Icon(_cameraBusy ? Icons.camera : Icons.photo_camera),
            onPressed: _sessionId == null || _cameraBusy ? null : _capturePhoto,
          ),
          IconButton(
            tooltip: _recording ? 'Микрофон записывает' : 'Микрофон выключен',
            icon: Icon(_recording ? Icons.mic : Icons.mic_off),
            onPressed: _recording && !_recordingBusy ? _toggleRecording : null,
          ),
        ],
      ),
      body: Column(
        children: [
          if (_error != null)
            Container(
              width: double.infinity,
              color: const Color(0x33D9635B),
              padding: const EdgeInsets.all(12),
              child: Text(_error!),
            ),
          Align(
            alignment: Alignment.centerLeft,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
              child: ActionChip(
                avatar: const Icon(Icons.memory, size: 16),
                label: Text(_runtimeSummary),
                onPressed: _showInferenceSettings,
              ),
            ),
          ),
          Expanded(
            child: ListView.builder(
              padding: const EdgeInsets.all(16),
              itemCount: _turns.length,
              itemBuilder: (context, index) {
                final turn = _turns[index];
                return Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(turn.who,
                          style: Theme.of(context).textTheme.labelSmall),
                      Text(turn.text),
                    ],
                  ),
                );
              },
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _message,
                    textInputAction: TextInputAction.send,
                    onSubmitted: (_) => _sendMessage(),
                    decoration: const InputDecoration(hintText: 'Напишите Юи'),
                  ),
                ),
                IconButton(
                  tooltip: 'Отправить сообщение',
                  icon: const Icon(Icons.send),
                  onPressed: _channel == null ? null : _sendMessage,
                ),
              ],
            ),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _channel == null || _recordingBusy ? null : _toggleRecording,
        icon: Icon(_recording ? Icons.stop : Icons.graphic_eq),
        label: Text(_recording ? 'Закончить запись' : 'Сказать'),
      ),
    );
  }
}
