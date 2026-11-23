# Voice Messages

Aviary turns speech into text with a local [whisper.cpp](https://github.com/ggml-org/whisper.cpp) engine. Audio never leaves your machine. Transcription covers two places:

- **Channel voice messages.** A voice note sent over Slack, Discord, or Signal reaches the agent as text.
- **The chat composer's mic button.** Record a message in the control panel and review the transcript before sending it.

## Install

Transcription needs three things: whisper.cpp's `whisper-cli`, a whisper model, and `ffmpeg` to convert audio to the 16 kHz mono WAV that `whisper-cli` reads.

```sh
aviary deps install
```

This installs `whisper-cli` (Homebrew package `whisper-cpp`) and `ffmpeg` with Homebrew, or with winget on Windows, and downloads the configured model (default `base.en`) to `deps/whisper/` under Aviary's data directory. You can also click **Install** under **Voice transcription** on the **System > Tools** page. On systems without Homebrew or winget, install the two programs yourself, either on your `PATH` or in `deps/bin/` under Aviary's data directory.

### Docker

The official Docker image already includes everything transcription needs, so it works out of the box with no install step and no network access: `whisper-cli` (built from whisper.cpp), `ffmpeg`, and the `base.en` model at `/opt/aviary/models/ggml-base.en.bin`. The image's `AVIARY_CONFIG_TRANSCRIPTION_MODEL` environment variable points at that file and is used whenever `aviary.yaml` sets no `transcription.model`. To use another model, set `transcription.model` to a model name (and run `aviary deps install` to download it) or to the path of a ggml model file, for example one mounted into the container.

Check what is installed with `aviary deps status`, or on **System > Tools**, which shows each piece, its path, and the model name.

## Channel voice messages

Slack, Discord, and Signal attachments are treated as audio when their MIME type is `audio/*` (or `video/webm` and `application/ogg`, which some platforms use for voice notes), or, when the type is missing or generic, when the file name ends in `.ogg`, `.opus`, `.m4a`, `.aac`, `.mp3`, `.wav`, `.webm`, `.flac`, `.amr`, or `.caf`.

Each one is transcribed and the agent gets:

```text
[voice message 0:12, transcribed]: Can you check whether the deploy finished?
[file attached: voice.ogg (audio/ogg, 18.2 KB) saved at /path/to/voice.ogg]
```

Text sent with the voice note comes first, and the saved file stays available to the agent as with any [attachment](./channels.md#attachments).

The transcript is stored in session history with the message, so it survives restarts. Transcripts are also cached by the audio's content, so a redelivered message is never transcribed twice. The control panel's chat history shows the full transcript with an audio player for the original recording.

If transcription fails, or the engine is not installed, the message still reaches the agent with a note instead of the transcript:

```text
[voice message, not transcribed: transcription unavailable: whisper-cli missing; run `aviary deps install`, or Install on the System > Tools page]
```

## Composer mic button

The mic button sits beside **Send** in the chat composer.

1. Click it to start recording. The first time, the browser asks for microphone access. While recording, the composer shows the elapsed time and a **Cancel** button that discards the audio.
2. Click it again to stop. The recording is uploaded to the Aviary server and transcribed; the mic and **Send** buttons are disabled until the transcript comes back.
3. The transcript is **inserted into the composer at the cursor**, after any text you already typed. It is never sent automatically: edit it if needed, then send it like any message.

Browsers only allow microphone access on secure pages. Open the control panel over `https://` or at `localhost`; on a plain `http://` LAN address the mic button is disabled and explains why. The Aviary server, the Docker image, and `pnpm dev` all serve HTTPS by default; from a phone, accept or trust the self-signed certificate as described in [TLS Certificates](/guide/operations#tls-certificates). If microphone access was denied, allow it in the browser's site settings and try again. When the server has no transcription engine, the composer shows what is missing and how to install it.

## Configuration

```yaml
transcription:
  enabled: true      # false: voice messages stay plain file attachments
  engine: local      # whisper.cpp; the only engine today
  model: base.en     # whisper.cpp model name, or a path to a ggml model file
  language: auto     # or an ISO 639-1 code such as "en"
```

See [`transcription`](../reference/config.md#transcription) in the configuration reference. English-only models (`*.en`) are smaller and faster for English; use a multilingual model such as `base` or `small` with `language: auto` for other languages. Run `aviary deps install` again after changing `model` to download it.
