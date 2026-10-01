# Lip-sync profile

`profile.json` is the sample MFCC calibration profile from
[wLipSync](https://github.com/mrxz/wLipSync/blob/main/example/profile.json)
(MIT, © 2021 hecomi, © 2024 Noeri Huisman), itself produced with uLipSync.
The local filesystem path recorded by the calibration tool was removed.

The profile classifies the vowels A, I, U, E, O and silence (S). Yui's
`SpeechOutput` maps them to VRM visemes (aa, ih, ou, ee, oh) and to Live2D
`ParamMouthOpenY`/`ParamMouthForm` or `ParamMouthA…O`. Russian vowels а, и,
у, э, о map directly; ы/е/ё/ю/я fall onto the closest neighbour.
