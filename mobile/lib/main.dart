import 'package:flutter/material.dart';

import 'core/pairing.dart';
import 'ui/home_page.dart';
import 'ui/pairing_page.dart';

void main() {
  runApp(const YuiApp());
}

class YuiApp extends StatelessWidget {
  const YuiApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Yui',
      theme: ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFFE3A951),
          brightness: Brightness.dark,
        ),
        scaffoldBackgroundColor: const Color(0xFF14131C),
      ),
      home: FutureBuilder<Pairing?>(
        future: Pairing.load(),
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const Scaffold(body: Center(child: CircularProgressIndicator()));
          }
          final pairing = snapshot.data;
          return pairing == null ? const PairingPage() : HomePage(pairing: pairing);
        },
      ),
    );
  }
}
