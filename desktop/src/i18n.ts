/**
 * Interface strings in Russian and English. Every entry carries both
 * languages side by side, so a missing translation is a type error.
 *
 * Static markup uses data attributes, applied by applyI18n():
 *   data-i18n="key"              → textContent
 *   data-i18n-placeholder="key"  → placeholder
 *   data-i18n-title="key"        → title (and aria-label when present)
 *   data-i18n-aria="key"         → aria-label
 */

export type Lang = "ru" | "en";

const DICT = {
  // App shell
  "app.title": ["Yui", "Yui"],
  "app.connect": ["Подключиться", "Connect"],
  "app.connecting": ["Подключение…", "Connecting…"],
  "app.newChat": ["Новый разговор", "New conversation"],
  "app.newChatConfirm": ["Начать новый разговор? Текущий будет завершён, память сохранится.", "Start a new conversation? The current one ends; memories are kept."],
  "app.detachAvatar": ["Аватар в отдельном окне", "Avatar in a separate window"],
  "app.detachChat": ["Чат в отдельном окне", "Chat in a separate window"],
  "app.settings": ["Настройки", "Settings"],
  "app.state": ["Состояние", "Status"],
  "app.privacy": ["Куда могут уходить данные", "Where data may go"],
  "app.avatarLabel": ["Аватар Юи", "Yui's avatar"],
  "app.avatarDetached": ["Аватар открыт в отдельном окне.", "The avatar is open in a separate window."],
  "app.chatDetached": ["Чат открыт в отдельном окне.", "The chat is open in a separate window."],
  "app.returnHere": ["Вернуть сюда", "Bring back"],
  "app.enableSound": ["Включить звук", "Enable sound"],
  "app.dialogue": ["Диалог", "Conversation"],
  "app.window": ["Окно", "Window"],
  "app.popupBlocked": ["Браузер заблокировал окно. Разрешите всплывающие окна для Yui.", "The browser blocked the window. Allow pop-ups for Yui."],
  "app.avatarWindowTitle": ["Yui — аватар", "Yui — avatar"],
  "app.chatWindowTitle": ["Yui — чат", "Yui — chat"],

  // States and emotions
  "state.idle": ["готова", "ready"],
  "state.listening": ["слушает", "listening"],
  "state.thinking": ["думает", "thinking"],
  "state.speaking": ["говорит", "speaking"],
  "state.acting": ["выполняет действие", "acting"],
  "state.error": ["ошибка", "error"],
  "state.closed": ["сессия закрыта", "session closed"],
  "state.offline": ["не в сессии", "offline"],
  "emotion.joy": ["радость", "joy"],
  "emotion.warm": ["тепло", "warmth"],
  "emotion.concern": ["сочувствие", "concern"],
  "emotion.sad": ["грусть", "sadness"],
  "emotion.alert": ["внимание", "alert"],
  "emotion.surprise": ["удивление", "surprise"],
  "emotion.think": ["раздумье", "thinking"],
  "emotion.calm": ["спокойствие", "calm"],
  "emotion.angry": ["недовольство", "annoyance"],

  // Privacy tag
  "privacy.local": ["только локально", "local only"],
  "privacy.remoteLock": ["выбрана внешняя модель", "external model selected"],
  "privacy.remoteDefault": ["разрешён внешний провайдер", "external provider allowed"],

  // Chat
  "chat.you": ["Вы", "You"],
  "chat.yui": ["Юи", "Yui"],
  "chat.emptyTitle": ["Разговор начинается здесь", "The conversation starts here"],
  "chat.emptyText": ["Подключитесь и напишите Юи или включите микрофон.", "Connect, then write to Yui or turn on the microphone."],
  "chat.newTitle": ["Новый разговор", "New conversation"],
  "chat.newText": ["Напишите Юи или включите микрофон.", "Write to Yui or turn on the microphone."],
  "chat.placeholder": ["Напишите Юи…", "Message Yui…"],
  "chat.inputLabel": ["Сообщение Юи", "Message to Yui"],
  "chat.photo": ["Отправить фото", "Send a photo"],
  "chat.speak": ["Говорить", "Speak"],
  "chat.micOff": ["Выключить микрофон", "Turn the microphone off"],
  "chat.stop": ["Прервать ответ (Esc)", "Stop the reply (Esc)"],
  "chat.send": ["Отправить", "Send"],
  "chat.error": ["Ошибка", "Error"],
  "chat.action": ["Действие", "Action"],
  "chat.reminder": ["Напоминание", "Reminder"],
  "chat.timer": ["Сработал таймер", "Timer finished"],
  "chat.micError": ["Ошибка микрофона", "Microphone error"],
  "chat.confirmError": ["Ошибка подтверждения", "Confirmation error"],
  "chat.confirmationsError": ["Ошибка подтверждений", "Confirmations error"],

  // Tool confirmation and consent
  "tool.title": ["Подтверждение действия", "Confirm action"],
  "tool.approve": ["Подтвердить", "Approve"],
  "tool.reject": ["Отклонить", "Reject"],
  "tool.strong": ["Нужно подтверждение PIN или биометрией на устройстве с поддержкой этой проверки.", "This needs a PIN or biometric confirmation on a device that supports it."],
  "tool.expired": ["Срок подтверждения истёк. Повторите запрос.", "The confirmation expired. Ask again."],
  "consent.title": ["Отправить данные наружу?", "Send data outside?"],
  "consent.text": ["Провайдер «{provider}» запрашивает категорию «{category}». Одноразовое разрешение действует для следующего запроса в течение пяти минут.", "Provider “{provider}” requests the “{category}” category. A one-time permission covers the next request within five minutes."],
  "consent.unknown": ["неизвестный", "unknown"],
  "consent.failed": ["Не удалось сохранить решение: {error}. Повторите попытку.", "Could not save the decision: {error}. Try again."],
  "consent.deny": ["Не отправлять", "Don't send"],
  "consent.once": ["Разрешить один раз", "Allow once"],
  "consent.always": ["Разрешить всегда", "Always allow"],

  // Microphone
  "mic.off": ["Микрофон выключен", "Microphone off"],
  "mic.push": ["Слушаю · до {seconds} с", "Listening · up to {seconds} s"],
  "mic.handsfree": ["Свободный разговор · говорите, когда захотите", "Hands-free · speak whenever you like"],
  "mic.hearing": ["● Слушаю…", "● Listening…"],
  "mic.denied": ["Нет доступа к микрофону. Разрешите микрофон для сайта и откройте его по HTTPS или с компьютера.", "No microphone access. Allow the microphone for this site and open it over HTTPS or on the computer."],
  "mic.notFound": ["Микрофон не найден.", "No microphone found."],
  "mic.unavailable": ["Микрофон недоступен. На телефоне откройте HTTPS-адрес Юи и разрешите доступ к микрофону.", "The microphone is unavailable. On a phone, open Yui's HTTPS address and allow microphone access."],
  "mic.vadFallback": ["Нейросетевой VAD недоступен, использую энергетический: {error}", "Neural VAD unavailable, using the energy detector: {error}"],

  // Photo
  "photo.title": ["Фото", "Photo"],
  "photo.preparing": ["Подготовка фотографии…", "Preparing the photo…"],
  "photo.unreadable": ["Не удалось прочитать фотографию.", "Could not read the photo."],
  "photo.unsupported": ["Браузер не поддерживает обработку фотографий.", "This browser cannot process photos."],
  "photo.cancelled": ["Отправка отменена: соединение изменилось.", "Cancelled: the connection changed."],
  "photo.sending": ["Фото отправляется…", "Sending the photo…"],
  "photo.question": ["Опиши, что видишь на фотографии. Ответь по-русски.", "Describe what you see in the photo. Answer in English."],
  "photo.empty": ["Модель не вернула описание. Попробуйте другое фото.", "The model returned no description. Try another photo."],
  "photo.changed": ["Соединение изменилось. Отправьте фотографию повторно.", "The connection changed. Send the photo again."],
  "photo.failed": ["Не удалось отправить фото: {error}", "Could not send the photo: {error}"],

  // Connection
  "conn.connected": ["подключено · {version}", "connected · {version}"],
  "conn.disconnected": ["не подключено", "not connected"],
  "conn.lost": ["соединение потеряно", "connection lost"],
  "conn.lostError": ["Соединение потеряно", "Connection lost"],
  "conn.notReady": ["Соединение ещё не готово. Подключитесь повторно.", "The connection is not ready yet. Connect again."],
  "conn.retry": ["переподключение через {seconds} с", "reconnecting in {seconds} s"],
  "conn.tokenExpired": ["токен устарел", "token expired"],
  "conn.tokenRejected": ["Токен отклонён ядром. Откройте новую ссылку подключения Yui.", "The core rejected the token. Open a fresh Yui connection link."],
  "conn.connectInMain": ["Подключите Юи в основном окне.", "Connect Yui in the main window."],
  "conn.retrying": ["Нет соединения с ядром, повторяю…", "No connection to the core, retrying…"],

  // Pairing
  "pair.title": ["Подключение к компьютеру", "Connect to the computer"],
  "pair.help": ["Откройте этот сайт на телефоне и введите одноразовый код с компьютера.", "Open this site on the phone and enter the one-time code from the computer."],
  "pair.hostLabel": ["HTTPS-адрес компьютера для телефона", "Computer's HTTPS address for the phone"],
  "pair.codeLabel": ["Код с компьютера", "Code from the computer"],
  "pair.submit": ["Сопрячь", "Pair"],
  "pair.close": ["Закрыть", "Close"],
  "pair.qrLabel": ["QR-код для подключения телефона", "QR code to connect the phone"],
  "pair.needHost": ["Введите HTTPS-адрес компьютера из окна запуска, чтобы показать QR-код.", "Enter the computer's HTTPS address from the launcher to show the QR code."],
  "pair.badHost": ["Укажите доступный HTTPS-адрес компьютера без пути.", "Enter a reachable HTTPS address of the computer without a path."],
  "pair.scan": ["Отсканируйте QR-код в приложении Yui на телефоне.", "Scan the QR code in the Yui app on the phone."],
  "pair.needToken": ["Откройте ссылку с токеном из окна запуска Yui на компьютере.", "Open the link with the token from the Yui launcher on the computer."],
  "pair.codeValid": ["На телефоне откройте HTTPS-адрес компьютера из окна запуска. Код действует до {time}.", "On the phone, open the computer's HTTPS address from the launcher. The code is valid until {time}."],
  "pair.badCode": ["Одноразовый код недействителен, уже использован или истёк. Получите новый код на компьютере.", "The one-time code is invalid, used or expired. Get a new code on the computer."],
  "pair.failed": ["Сопряжение: {detail}", "Pairing: {detail}"],
  "pair.errorCode": ["ошибка {status}", "error {status}"],
  "pair.deviceName": ["Телефон · браузер", "Phone · browser"],

  // Settings: navigation
  "settings.title": ["Настройки", "Settings"],
  "settings.close": ["Закрыть настройки", "Close settings"],
  "settings.sections": ["Разделы настроек", "Settings sections"],
  "tab.character": ["Персонаж", "Character"],
  "tab.avatar": ["Аватар", "Avatar"],
  "tab.voice": ["Голос и микрофон", "Voice & microphone"],
  "tab.models": ["Модели", "Models"],
  "tab.inference": ["AI Runtime", "AI Runtime"],
  "tab.memory": ["Память", "Memory"],
  "tab.ledger": ["Приватность", "Privacy"],
  "tab.devices": ["Устройства", "Devices"],
  "tab.interface": ["Интерфейс", "Interface"],

  // Settings: character
  "character.intro": ["Имя, манера речи и характер Юи. Хранятся в ядре и действуют на всех устройствах.", "Yui's name, speaking style and personality. Stored in the core and shared by every device."],
  "character.name": ["Имя", "Name"],
  "character.mode": ["Режим разговора", "Conversation mode"],
  "mode.normal": ["Обычный", "Normal"],
  "mode.playful": ["Игривый", "Playful"],
  "mode.support": ["Поддержка", "Support"],
  "mode.work": ["Работа", "Work"],
  "mode.learning": ["Обучение", "Learning"],
  "mode.brief": ["Кратко", "Brief"],
  "mode.night": ["Ночной", "Night"],
  "mode.public": ["На людях", "In public"],
  "mode.silent": ["Без голоса", "Silent"],
  "character.style": ["Манера речи", "Speaking style"],
  "character.stylePlaceholder": ["Например: тёплая, с мягким юмором, отвечает коротко", "For example: warm, gentle humour, short answers"],
  "character.relationship": ["Отношения", "Relationship"],
  "character.relationshipPlaceholder": ["Например: близкий друг", "For example: close friend"],
  "character.voice": ["Голос (профиль TTS)", "Voice (TTS profile)"],
  "character.initiative": ["Инициативность", "Initiative"],
  "character.card": ["Импорт карточки (SillyTavern .png/.json)", "Import a card (SillyTavern .png/.json)"],
  "character.cardHint": ["Поля заполнятся из карточки; проверьте их и нажмите «Сохранить».", "Fields are filled from the card; review them and press “Save”."],
  "character.save": ["Сохранить", "Save"],
  "character.connect": ["Подключите ядро, чтобы изменить персонажа.", "Connect the core to edit the character."],
  "character.applies": ["Изменения применятся к следующим ответам.", "Changes apply to the next replies."],
  "character.saving": ["Сохраняю…", "Saving…"],
  "character.saved": ["Сохранено.", "Saved."],
  "character.saveFailed": ["Не удалось сохранить: {error}", "Could not save: {error}"],
  "character.cardLoaded": ["Карточка «{name}» загружена. Проверьте поля и сохраните.", "Card “{name}” loaded. Review the fields and save."],
  "trait.warmth": ["Теплота", "Warmth"],
  "trait.curiosity": ["Любопытство", "Curiosity"],
  "trait.playfulness": ["Игривость", "Playfulness"],
  "trait.directness": ["Прямота", "Directness"],
  "trait.assertiveness": ["Настойчивость", "Assertiveness"],
  "trait.calmness": ["Спокойствие", "Calmness"],
  "card.tooBig": ["Файл карточки больше 20 МБ", "The card file is larger than 20 MB"],
  "card.unreadable": ["Не удалось прочитать карточку: нужен .json или .png с данными персонажа", "Could not read the card: a .json or a .png with character data is required"],
  "card.noName": ["В карточке нет имени персонажа", "The card has no character name"],

  // Settings: avatar
  "avatar.intro": ["Модель, кадрирование и живость движений. Свои модели хранятся только в этом браузере.", "Model, framing and liveliness. Your own models stay in this browser only."],
  "avatar.models": ["Модель аватара", "Avatar model"],
  "avatar.import": ["Импорт .vrm или Live2D .zip", "Import .vrm or Live2D .zip"],
  "avatar.importing": ["Импортирую…", "Importing…"],
  "avatar.imported": ["Добавлено: {name}", "Added: {name}"],
  "avatar.delete": ["Удалить модель", "Delete model"],
  "avatar.deleteConfirm": ["Удалить «{name}» из этого браузера?", "Delete “{name}” from this browser?"],
  "avatar.scale": ["Масштаб", "Scale"],
  "avatar.offset": ["Высота кадра", "Framing height"],
  "avatar.motion": ["Живость движений", "Liveliness"],
  "avatar.backdrop": ["Фон сцены", "Stage background"],
  "backdrop.aurora": ["Мягкое свечение", "Soft glow"],
  "backdrop.plain": ["Однотонный", "Plain"],
  "backdrop.transparent": ["Прозрачный", "Transparent"],
  "avatar.follow": ["Взгляд следит за курсором", "Eyes follow the pointer"],
  "avatar.onTop": ["Отдельное окно поверх других", "Detached window stays on top"],
  "avatar.detach": ["Открыть аватар в отдельном окне", "Open the avatar in a separate window"],
  "avatar.loading": ["Загрузка аватара…", "Loading the avatar…"],
  "avatar.vrmFailed": ["Не удалось загрузить VRM. Выберите другую модель или проверьте WebGL.", "Could not load the VRM. Pick another model or check WebGL."],
  "avatar.live2dFailed": ["Не удалось загрузить Live2D. Проверьте файлы модели и поддержку WebGL.", "Could not load Live2D. Check the model files and WebGL support."],
  "avatar.noteVowels": ["Live2D sample · гласные", "Live2D sample · vowels"],
  "avatar.custom": ["Своя модель", "Custom model"],
  "avatar.storage": ["Хранилище браузера недоступно", "Browser storage is unavailable"],
  "avatar.tooBig": ["Файл больше 200 МБ", "The file is larger than 200 MB"],
  "avatar.notVrm": ["Это не VRM: нет заголовка glTF", "Not a VRM: the glTF header is missing"],
  "avatar.formats": ["Поддерживаются .vrm и .zip с моделью Live2D (.model3.json)", "Supported: .vrm and a .zip with a Live2D model (.model3.json)"],
  "avatar.noModel3": ["В архиве нет файла .model3.json (поддерживаются Cubism 3/4)", "The archive has no .model3.json (Cubism 3/4 are supported)"],
  "avatar.noMoc": ["В архиве не найден файл .moc3", "The archive has no .moc3 file"],
  "avatar.missingFile": ["Файл модели не найден. Импортируйте его заново.", "The model file is missing. Import it again."],
  "avatar.mb": ["МБ", "MB"],

  // Detached avatar window
  "pet.toolbar": ["Окно аватара", "Avatar window"],
  "pet.drag": ["Перетащить (или тяните за модель)", "Drag (or drag the character)"],
  "pet.smaller": ["Отдалить", "Zoom out"],
  "pet.bigger": ["Приблизить", "Zoom in"],
  "pet.onTop": ["Поверх окон", "Always on top"],
  "pet.clickThrough": ["Клики сквозь окно (выход: Ctrl+Shift+Y или трей)", "Click-through (exit: Ctrl+Shift+Y or the tray)"],
  "pet.clickThroughOn": ["Клики проходят сквозь окно · Ctrl+Shift+Y — вернуть", "Clicks pass through · Ctrl+Shift+Y to restore"],
  "pet.close": ["Вернуть в основное окно", "Back to the main window"],

  // Settings: voice
  "voice.intro": ["Воспроизведение, синхронизация губ и способ разговора голосом.", "Playback, lip sync and how you talk by voice."],
  "voice.output": ["Голос Юи", "Yui's voice"],
  "voice.volume": ["Громкость", "Volume"],
  "voice.lipSensitivity": ["Чувствительность губ", "Lip sensitivity"],
  "voice.lipEngine": ["Синхронизация губ", "Lip sync"],
  "lip.auto": ["Гласные (MFCC, точнее)", "Vowels (MFCC, more accurate)"],
  "lip.spectral": ["Спектр (легче)", "Spectrum (lighter)"],
  "voice.lipActive": ["Сейчас работает", "Active now"],
  "lip.mfcc": ["гласные (wLipSync MFCC)", "vowels (wLipSync MFCC)"],
  "lip.spectralActive": ["спектральная оценка", "spectral estimate"],
  "voice.preview": ["Проба голоса", "Voice preview"],
  "voice.previewFailed": ["Не удалось загрузить пример голоса", "Could not load the voice sample"],
  "voice.mic": ["Микрофон", "Microphone"],
  "voice.micMode": ["Режим", "Mode"],
  "micMode.push": ["Нажми и говори", "Push to talk"],
  "micMode.handsfree": ["Свободный разговор (VAD)", "Hands-free (VAD)"],
  "voice.vadEngine": ["Определение речи", "Speech detection"],
  "vad.silero": ["Нейросеть Silero (точнее)", "Silero neural network (more accurate)"],
  "vad.energy": ["По громкости (легче)", "By loudness (lighter)"],
  "voice.vadSensitivity": ["Чувствительность VAD", "VAD sensitivity"],
  "voice.bargeIn": ["Перебивать Юи голосом (нужно эхоподавление; ядро решает, разрешено ли)", "Interrupt Yui by voice (needs echo cancellation; the core decides if it is allowed)"],
  "voice.hotkeys": ["Горячие клавиши (приложение для ПК)", "Hotkeys (desktop app)"],
  "voice.hotkeysText": ["Ctrl+Shift+Space — микрофон из любой программы · Ctrl+Shift+A — показать/скрыть аватар · Ctrl+Shift+Y — клики сквозь аватар", "Ctrl+Shift+Space — microphone from any app · Ctrl+Shift+A — show/hide the avatar · Ctrl+Shift+Y — click-through avatar"],

  // Settings: models
  "models.intro": ["Модель для диалога, голоса и других задач.", "Models for dialogue, voice and other tasks."],
  "models.add": ["Добавить", "Add"],
  "models.source": ["Источник моделей", "Model source"],
  "models.all": ["Все", "All"],
  "models.local": ["Локальные", "Local"],
  "models.providers": ["Провайдеры", "Providers"],
  "models.purpose": ["Назначение", "Purpose"],
  "models.provider": ["Провайдер", "Provider"],
  "models.allProviders": ["Все провайдеры", "All providers"],
  "models.search": ["Найти", "Search"],
  "models.searchPlaceholder": ["Название или ID", "Name or ID"],
  "models.connect": ["Подключите ядро, чтобы увидеть модели.", "Connect the core to see models."],
  "models.available": ["Доступные модели", "Available models"],
  "models.details": ["Сведения о модели", "Model details"],
  "models.pick": ["Выберите модель из списка, чтобы увидеть её параметры.", "Pick a model to see its details."],
  "kind.llm": ["Диалог", "Dialogue"],
  "kind.stt": ["Распознавание речи", "Speech recognition"],
  "kind.tts": ["Голос", "Voice"],
  "kind.vision": ["Зрение", "Vision"],
  "kind.embeddings": ["Память", "Memory"],
  "service.local": ["Локальный сервер", "Local server"],
  "service.mock": ["Встроенная", "Built-in"],
  "service.custom": ["Другой сервис", "Other service"],
  "service.customLocal": ["Другой локальный сервер", "Other local server"],
  "service.customProvider": ["Другой провайдер", "Other provider"],
  "op.mock": ["Встроенная тестовая модель", "Built-in test model"],
  "op.worker": ["Локальный worker", "Local worker"],
  "op.cloud": ["Облачный API", "Cloud API"],
  "op.gpu": ["Локально · GPU", "Local · GPU"],
  "op.cpu": ["Локально · CPU", "Local · CPU"],
  "op.localApi": ["Локальный API", "Local API"],
  "models.localLabel": ["Локальная", "Local"],
  "models.external": ["Внешний провайдер", "External provider"],
  "fact.purpose": ["Назначение", "Purpose"],
  "fact.family": ["Семейство", "Family"],
  "fact.operation": ["Способ работы", "Runs as"],
  "fact.selection": ["Выбор", "Selection"],
  "fact.auto": ["Участвует в автоподборе", "Takes part in auto-selection"],
  "fact.manual": ["Выбирается вручную", "Selected manually"],
  "fact.quality": ["Качество", "Quality"],
  "fact.qualityValue": ["{quality}/100 · оценка для автовыбора", "{quality}/100 · auto-selection score"],
  "fact.voice": ["Голос", "Voice"],
  "fact.key": ["Ключ", "Key"],
  "fact.keyReady": ["{env} · доступен", "{env} · available"],
  "fact.keyMissing": ["{env} · не задан", "{env} · not set"],
  "fact.profile": ["Профиль", "Profile"],
  "fact.userAdded": ["Добавлен в настройках", "Added in settings"],
  "fact.fromConfig": ["Из конфигурации Yui", "From Yui's configuration"],
  "fact.tags": ["Метки", "Tags"],
  "fact.notSet": ["Не указано", "Not set"],
  "fact.notNeeded": ["Не требуется", "Not needed"],
  "fact.builtInWorker": ["Встроенный worker", "Built-in worker"],
  "models.noteMock": ["Тестовый провайдер работает без внешнего сервера.", "The test provider works without an external server."],
  "models.noteLocal": ["Локальный сервер должен быть запущен. Доступность проверится при запросе.", "The local server must be running. Availability is checked on request."],
  "models.noteRemote": ["Передача данных этому провайдеру контролируется разрешениями Yui.", "Data sent to this provider is controlled by Yui's permissions."],
  "models.selected": ["Выбрана", "Selected"],
  "models.use": ["Использовать", "Use"],
  "models.setKey": ["Задайте {env} в окружении ядра и перезапустите Yui.", "Set {env} in the core's environment and restart Yui."],
  "models.noMatch": ["По этим условиям моделей нет.", "No models match these filters."],
  "models.none": ["Пока нет добавленных моделей.", "No models added yet."],
  "models.canAdd": ["Можно добавить OpenAI-совместимую модель.", "You can add an OpenAI-compatible model."],
  "models.default": ["По умолчанию", "Default"],
  "models.localShort": ["локально", "local"],
  "models.externalShort": ["внешний", "external"],
  "models.needKey": ["Нужен API-ключ", "API key needed"],
  "models.count": ["{visible} из {total} моделей", "{visible} of {total} models"],
  "models.saving": ["Сохраняю выбор…", "Saving the choice…"],
  "models.loadRuntime": ["Сначала загрузите настройки AI Runtime.", "Load the AI Runtime settings first."],
  "models.saved": ["Выбор сохранён. Новая модель применится при следующем запросе.", "Saved. The new model applies to the next request."],
  "models.saveFailed": ["Не удалось выбрать модель: {error}", "Could not select the model: {error}"],
  "models.loading": ["Загружаю модели…", "Loading models…"],
  "models.loadFailed": ["Не удалось загрузить модели: {error}", "Could not load models: {error}"],
  "models.added": ["Модель добавлена. Выберите «Использовать», чтобы включить её.", "Model added. Choose “Use” to enable it."],
  "addModel.title": ["Добавить модель", "Add a model"],
  "addModel.intro": ["Подключите модель через OpenAI-совместимый сервер или сервис синтеза речи. Для локальной модели сервер должен быть запущен на этом компьютере.", "Connect a model through an OpenAI-compatible server or a speech service. A local model's server must run on this computer."],
  "addModel.source": ["Источник", "Source"],
  "addModel.local": ["Локальная", "Local"],
  "addModel.provider": ["Внешний провайдер", "External provider"],
  "addModel.service": ["Сервис", "Service"],
  "addModel.id": ["ID модели", "Model ID"],
  "addModel.idPlaceholder": ["Например, qwen3.5:9b", "For example, qwen3.5:9b"],
  "addModel.endpoint": ["Адрес API", "API address"],
  "addModel.keyEnv": ["Переменная с API-ключом", "API key environment variable"],
  "addModel.voice": ["Голос (ID у сервиса)", "Voice (service ID)"],
  "addModel.voicePlaceholder": ["nova · ID голоса ElevenLabs · ru-RU-SvetlanaNeural", "nova · ElevenLabs voice ID · en-US-AvaNeural"],
  "addModel.hintLocal": ["Локальный API должен слушать 127.0.0.1 или localhost. Ключ оставьте пустым, если он не нужен.", "The local API must listen on 127.0.0.1 or localhost. Leave the key empty if it is not needed."],
  "addModel.hintRemote": ["Укажите имя переменной с ключом в окружении ядра. Сам ключ здесь не вводится и не сохраняется.", "Enter the name of the core environment variable holding the key. The key itself is never entered or stored here."],
  "addModel.hintSpeech": ["Облачный голос получает текст ответов только после вашего разрешения.", "A cloud voice receives reply text only after you allow it."],
  "addModel.submit": ["Добавить", "Add"],
  "addModel.cancel": ["Отмена", "Cancel"],
  "addModel.close": ["Закрыть", "Close"],

  // Settings: runtime
  "runtime.intro": ["Как ядро выбирает модель под нагрузку компьютера.", "How the core picks a model for the computer's load."],
  "runtime.mode": ["Режим", "Mode"],
  "runtimeMode.auto": ["Авто", "Auto"],
  "runtimeMode.max": ["Максимальное качество", "Maximum quality"],
  "runtimeMode.balanced": ["Баланс", "Balanced"],
  "runtimeMode.gaming": ["Игровой", "Gaming"],
  "runtimeMode.manual": ["Вручную", "Manual"],
  "runtime.lock": ["Закрепить модель", "Lock a model"],
  "runtime.autoPick": ["Автовыбор", "Automatic"],
  "runtime.minQuality": ["Минимальное качество", "Minimum quality"],
  "runtime.downgrade": ["Разрешить понижать модель", "Allow downgrading the model"],
  "runtime.model": ["Рабочая LLM", "Active LLM"],
  "runtime.game": ["Игра", "Game"],
  "runtime.none": ["Решение ещё не принималось.", "No decision yet."],
  "runtime.noReason": ["Выбрано без дополнительного пояснения.", "Selected without further explanation."],
  "runtime.remoteOption": ["внешняя модель ↗", "external model ↗"],
  "runtime.free": ["{mb} МБ свободно", "{mb} MB free"],
  "runtime.noGame": ["нет", "none"],
  "runtime.active": ["активна", "active"],
  "runtime.pendingSwitch": ["Выбрана {locked}; последний вызов был на {active}. Новая модель применится при следующем запросе.", "{locked} is selected; the last call used {active}. The new model applies to the next request."],
  "runtime.notCalled": ["ещё не вызывалась", "not called yet"],
  "runtime.lockedPending": ["Выбрана {locked}. Рабочая модель появится после первого запроса.", "{locked} is selected. The active model appears after the first request."],
  "runtime.firstRequest": ["Рабочая модель появится после первого запроса.", "The active model appears after the first request."],
  "runtime.waiting": ["ожидает вызова", "waiting for a call"],
  "runtime.off": ["выключен", "off"],
  "runtime.statusFailed": ["Не удалось получить состояние AI Runtime.", "Could not read the AI Runtime status."],
  "runtime.connectFirst": ["Сначала подключите ядро, затем выбирайте модель.", "Connect the core first, then pick a model."],

  // Settings: memory
  "memory.intro": ["Что Юи запомнила. Закрепите важное, подтвердите спорное или удалите лишнее.", "What Yui remembers. Pin what matters, confirm what is uncertain, delete the rest."],
  "memory.addPlaceholder": ["Запомнить вручную: например, «я пью чай без сахара»", "Remember manually: for example, “I take tea without sugar”"],
  "memory.add": ["Запомнить", "Remember"],
  "memory.emptyNew": ["Новых воспоминаний пока нет.", "No new memories yet."],
  "memory.empty": ["Сохранённых воспоминаний пока нет.", "No saved memories yet."],
  "memory.loadFailed": ["Не удалось загрузить память: {error}", "Could not load memory: {error}"],
  "memory.changeFailed": ["Не удалось изменить память: {error}", "Could not change memory: {error}"],
  "memory.saveFailed": ["Не удалось сохранить: {error}", "Could not save: {error}"],
  "memory.pinned": ["закреплено", "pinned"],
  "memory.confirm": ["Подтвердить", "Confirm"],
  "memory.pin": ["Закрепить", "Pin"],
  "memory.unpin": ["Открепить", "Unpin"],
  "memory.delete": ["Удалить", "Delete"],

  // Settings: privacy
  "privacy.intro": ["Какие категории данных уходили к моделям и какие разрешения действуют.", "Which data categories went to models and which permissions are in force."],
  "privacy.grants": ["Постоянные разрешения", "Standing permissions"],
  "privacy.noGrants": ["Постоянных разрешений нет.", "No standing permissions."],
  "privacy.log": ["Журнал передачи данных", "Data transfer log"],
  "privacy.noCalls": ["Вызовов модели пока нет.", "No model calls yet."],
  "privacy.connect": ["Подключите ядро.", "Connect the core."],
  "privacy.allowed": ["Разрешено: {category}", "Allowed: {category}"],
  "privacy.denied": ["Запрещено: {category}", "Denied: {category}"],
  "privacy.revoke": ["Отозвать", "Revoke"],
  "privacy.grantsFailed": ["Не удалось загрузить разрешения: {error}", "Could not load permissions: {error}"],
  "ledger.unknownType": ["тип провайдера неизвестен", "provider type unknown"],
  "ledger.remote": ["внешний", "external"],
  "ledger.local": ["локальный", "local"],
  "ledger.attempt": ["Категории попытки вызова", "Categories of the attempted call"],
  "ledger.categories": ["Категории вызова", "Call categories"],
  "ledger.sentOut": ["Передано вовне", "Sent outside"],
  "ledger.sentLocal": ["Передано локально", "Sent locally"],
  "ledger.none": ["без категорий", "no categories"],
  "ledger.excluded": ["исключено: {list}", "excluded: {list}"],
  "ledger.model": ["модель", "model"],
  "ledger.loadFailed": ["Не удалось загрузить журнал: {error}", "Could not load the log: {error}"],

  // Settings: devices
  "devices.intro": ["Телефоны и браузеры, подключённые к этому компьютеру.", "Phones and browsers connected to this computer."],
  "devices.pair": ["Подключить телефон", "Connect a phone"],
  "devices.connect": ["Подключите ядро, чтобы увидеть устройства.", "Connect the core to see devices."],
  "devices.none": ["Подключённых устройств нет.", "No connected devices."],
  "devices.paired": ["{kind} · сопряжено {date}", "{kind} · paired {date}"],
  "devices.seen": [" · был {time}", " · seen {time}"],
  "devices.revoke": ["Отключить устройство", "Disconnect device"],
  "devices.revokeConfirm": ["Отключить «{name}»? Устройству понадобится новое сопряжение.", "Disconnect “{name}”? The device will need to pair again."],
  "devices.loadFailed": ["Не удалось загрузить устройства: {error}", "Could not load devices: {error}"],

  // Settings: interface
  "ui.intro": ["Оформление и поведение этого окна.", "Appearance and behaviour of this window."],
  "ui.language": ["Язык интерфейса", "Interface language"],
  "lang.auto": ["Как в системе", "System"],
  "ui.theme": ["Тема", "Theme"],
  "theme.system": ["Как в системе", "System"],
  "theme.dark": ["Тёмная", "Dark"],
  "theme.light": ["Светлая", "Light"],
  "ui.accent": ["Акцент", "Accent"],
  "ui.enter": ["Enter отправляет, Shift+Enter — новая строка", "Enter sends, Shift+Enter adds a line"],
  "ui.ribbon": ["Показывать волну активности", "Show the activity wave"],
  "ui.connection": ["Соединение", "Connection"],
  "ui.core": ["Ядро", "Core"],
  "ui.coreAddress": ["Адрес ядра", "Core address"],
  "ui.reconnect": ["Переподключиться", "Reconnect"],
} as const satisfies Record<string, readonly [string, string]>;

export type Key = keyof typeof DICT;

const KEY = "yui.prefs";
function initialLang(): Lang {
  try {
    const stored = (JSON.parse(localStorage.getItem(KEY) ?? "{}") as { lang?: string }).lang;
    if (stored === "ru" || stored === "en") return stored;
  } catch { /* storage blocked */ }
  return detectLang();
}

export function detectLang(): Lang {
  const list = typeof navigator === "undefined" ? [] : navigator.languages ?? [navigator.language];
  for (const tag of list) {
    const base = tag?.toLowerCase().split("-")[0];
    if (base === "ru" || base === "uk" || base === "be" || base === "kk") return "ru";
    if (base === "en") return "en";
  }
  return "en";
}

let current: Lang = typeof window === "undefined" ? "ru" : initialLang();
const listeners = new Set<(lang: Lang) => void>();

export function lang(): Lang { return current; }

export function t(key: Key, params?: Record<string, string | number>): string {
  const entry = DICT[key] as readonly [string, string] | undefined;
  let text: string = entry ? entry[current === "en" ? 1 : 0] : key;
  if (params) for (const [name, value] of Object.entries(params)) text = text.split(`{${name}}`).join(String(value));
  return text;
}

/** Whether a string is a known key (used for dynamic lookups like states). */
export function has(key: string): key is Key { return key in DICT; }

export function setLang(next: Lang): void {
  if (next === current) return;
  current = next;
  if (typeof document !== "undefined") applyI18n();
  for (const listener of listeners) listener(next);
}

export function onLang(listener: (lang: Lang) => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function applyI18n(root: ParentNode = document): void {
  if (typeof document !== "undefined") document.documentElement.lang = current;
  for (const node of root.querySelectorAll<HTMLElement>("[data-i18n]")) node.textContent = t(node.dataset.i18n as Key);
  for (const node of root.querySelectorAll<HTMLInputElement>("[data-i18n-placeholder]")) node.placeholder = t(node.dataset.i18nPlaceholder as Key);
  for (const node of root.querySelectorAll<HTMLElement>("[data-i18n-title]")) {
    const text = t(node.dataset.i18nTitle as Key);
    node.title = text;
    if (node.hasAttribute("aria-label")) node.setAttribute("aria-label", text);
  }
  for (const node of root.querySelectorAll<HTMLElement>("[data-i18n-aria]")) node.setAttribute("aria-label", t(node.dataset.i18nAria as Key));
}

/** Every key with both translations, for tests. */
export const dictionary = DICT as Record<Key, readonly [string, string]>;
