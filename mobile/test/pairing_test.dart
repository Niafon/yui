import 'package:flutter_test/flutter_test.dart';
import 'package:yui_companion/core/pairing.dart';

void main() {
  test('phone pairing defaults to HTTPS and rejects remote plaintext', () {
    expect(Pairing.normalizeBaseUrl('192.168.1.15:8765'),
        'https://192.168.1.15:8765');
    expect(Pairing.normalizeBaseUrl('https://100.110.162.100:8765/'),
        'https://100.110.162.100:8765');
    expect(() => Pairing.normalizeBaseUrl('http://192.168.1.15:8765'),
        throwsFormatException);
    expect(
        () => Pairing.normalizeBaseUrl('0.0.0.0:8765'), throwsFormatException);
  });

  test('QR address is usable only when it contains a reachable host', () {
    expect(
        Pairing.parseQr('{"host":"192.168.1.15:8765","code":"ABCD-EFGH"}')
            ?.baseUrl,
        'https://192.168.1.15:8765');
    expect(
        Pairing.parseQr('{"host":"0.0.0.0:8765","code":"ABCD-EFGH"}'), isNull);
  });
}
