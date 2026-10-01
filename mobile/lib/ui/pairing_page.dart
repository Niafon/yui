import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../core/pairing.dart';
import 'home_page.dart';

/// First run: scan the QR shown on the PC, or type the code by hand.
class PairingPage extends StatefulWidget {
  const PairingPage({super.key});

  @override
  State<PairingPage> createState() => _PairingPageState();
}

class _PairingPageState extends State<PairingPage> {
  final _host = TextEditingController();
  final _code = TextEditingController();
  String? _error;
  bool _busy = false;

  Future<void> _scanQr() async {
    final scanner = MobileScannerController(formats: [BarcodeFormat.qrCode]);
    var resolved = false;
    try {
      final parsed = await showDialog<({String baseUrl, String code})>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: const Text('Сканировать QR-код Yui'),
          content: SizedBox(
            width: 300,
            height: 300,
            child: MobileScanner(
              controller: scanner,
              onDetect: (capture) {
                if (resolved) return;
                final raw = capture.barcodes.firstOrNull?.rawValue;
                if (raw == null) return;
                resolved = true;
                Navigator.of(dialogContext).pop(Pairing.parseQr(raw));
              },
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('Отмена'),
            ),
          ],
        ),
      );
      if (!mounted) return;
      if (parsed == null && resolved) {
        setState(() => _error = 'QR-код не содержит доступный HTTPS-адрес Yui');
      } else if (parsed != null) {
        setState(() {
          _host.text = parsed.baseUrl;
          _code.text = parsed.code;
          _error = null;
        });
      }
    } finally {
      await scanner.dispose();
    }
  }

  @override
  void dispose() {
    _host.dispose();
    _code.dispose();
    super.dispose();
  }

  Future<void> _pair() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final pairing = await Pairing.claim(
        baseUrl: _host.text.trim(),
        code: _code.text.trim(),
        deviceName: 'Android',
      );
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => HomePage(pairing: pairing)),
      );
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Подключение к компьютеру')),
      body: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
                'Откройте сцену Yui на компьютере и нажмите «Подключить телефон».'),
            const SizedBox(height: 20),
            TextField(
              controller: _host,
              keyboardType: TextInputType.url,
              decoration: const InputDecoration(
                labelText: 'HTTPS-адрес компьютера',
                hintText: 'https://192.168.1.15:8765',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _code,
              textCapitalization: TextCapitalization.characters,
              decoration: const InputDecoration(
                  labelText: 'Код с экрана', hintText: '4TQ9-B2KM'),
            ),
            const SizedBox(height: 20),
            OutlinedButton.icon(
              onPressed: _busy ? null : _scanQr,
              icon: const Icon(Icons.qr_code_scanner),
              label: const Text('Сканировать QR-код'),
            ),
            const SizedBox(height: 12),
            FilledButton(
              onPressed: _busy ? null : _pair,
              child: Text(_busy ? 'Подключаюсь…' : 'Подключить'),
            ),
            if (_error != null) ...[
              const SizedBox(height: 16),
              Text(_error!, style: const TextStyle(color: Color(0xFFD9635B))),
            ],
          ],
        ),
      ),
    );
  }
}
