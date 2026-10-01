# Yui Android client

The phone connects to Yui Core over HTTPS and secure WebSockets. Build it with
`flutter build apk` from this directory; the Android runner is under `android/`.

Before pairing, install and trust the public CA certificate from
`../data/tls/yui-ca.crt` on the phone. Enter the computer's reachable HTTPS
address and the one-time code shown by the desktop interface. The bind address
`0.0.0.0` is not a phone address.

The conversation screen supports text, push-to-talk voice, and a camera preview
photo. Voice recording is limited to 50 seconds and uses an Android foreground
service with a visible notification. The photo action opens the system camera
and sends its JPEG preview to the vision endpoint.

Run `flutter analyze` and `flutter test` after changing the Dart client. On a
Windows setup where the Pub cache is on `C:` and the project is on `D:`, Gradle
uses `kotlin.incremental=false` to avoid the Kotlin cross-drive cache failure.
