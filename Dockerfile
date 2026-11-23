FROM node:25-bookworm AS web-builder

WORKDIR /src

COPY package.json pnpm-lock.yaml ./
RUN npm install -g pnpm@8.6.0
RUN pnpm install --frozen-lockfile

COPY . .
RUN pnpm build:web

FROM golang:1.27-bookworm AS go-builder

ARG AVIARY_VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /src/internal/server/webdist ./internal/server/webdist

RUN CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X github.com/lsegal/aviary/internal/buildinfo.Version=${AVIARY_VERSION}" \
  -o /out/aviary \
  ./cmd/aviary

FROM debian:bookworm-slim AS whisper-builder

# Pin whisper.cpp to a release tag and the commit it points at, so builds are
# reproducible.
ARG WHISPER_CPP_VERSION=v1.9.5
ARG WHISPER_CPP_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037

RUN apt-get update && apt-get install -y --no-install-recommends \
  build-essential \
  ca-certificates \
  cmake \
  git \
  && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH

# GGML_NATIVE=OFF keeps the build from targeting the build machine's CPU. On
# amd64, GGML_CPU_ALL_VARIANTS builds one CPU backend per instruction-set level
# and picks the best one at runtime. Bookworm's gcc cannot build ggml's armv9
# variants, so arm64 gets one baseline armv8-a backend that runs on any arm64
# host. whisper-cli finds its shared libraries and CPU backend next to itself
# through $ORIGIN.
RUN case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
    amd64) cpu_variants=ON ;; \
    *) cpu_variants=OFF ;; \
  esac \
  && git clone --depth 1 --branch "${WHISPER_CPP_VERSION}" https://github.com/ggml-org/whisper.cpp /src/whisper.cpp \
  && test "$(git -C /src/whisper.cpp rev-parse HEAD)" = "${WHISPER_CPP_COMMIT}" \
  && cmake -S /src/whisper.cpp -B /build \
    -DCMAKE_BUILD_TYPE=Release \
    -DBUILD_SHARED_LIBS=ON \
    -DCMAKE_BUILD_RPATH='$ORIGIN' \
    -DGGML_NATIVE=OFF \
    -DGGML_BACKEND_DL=ON \
    -DGGML_CPU_ALL_VARIANTS="${cpu_variants}" \
    -DWHISPER_BUILD_TESTS=OFF \
    -DWHISPER_BUILD_SERVER=OFF \
  && cmake --build /build --config Release --target whisper-cli -j"$(nproc)" \
  && mkdir -p /out/whisper.cpp /out/samples \
  && cp -a /build/bin/whisper-cli /build/bin/*.so* /out/whisper.cpp/ \
  && cp /src/whisper.cpp/samples/jfk.wav /out/samples/jfk.wav \
  && /out/whisper.cpp/whisper-cli --help >/dev/null

FROM debian:bookworm-slim AS whisper-model

ARG WHISPER_MODEL=base.en
ARG WHISPER_MODEL_SHA256=a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /out \
  && curl -fsSLo "/out/ggml-${WHISPER_MODEL}.bin" "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${WHISPER_MODEL}.bin" \
  && echo "${WHISPER_MODEL_SHA256}  /out/ggml-${WHISPER_MODEL}.bin" | sha256sum -c -

# whisper-smoke is whisper-cli, the model, and the sample clip alone, so CI can
# check the whisper.cpp build on an architecture without building the full
# image. The final stage does not use it.
FROM debian:bookworm-slim AS whisper-smoke

RUN apt-get update && apt-get install -y --no-install-recommends libgomp1 \
  && rm -rf /var/lib/apt/lists/*

COPY --from=whisper-builder /out/whisper.cpp /opt/whisper.cpp
COPY --from=whisper-builder /out/samples /opt/aviary/samples
COPY --from=whisper-model /out /opt/aviary/models

FROM debian:bookworm-slim

ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  ffmpeg \
  fonts-liberation \
  git \
  libasound2 \
  libatk-bridge2.0-0 \
  libatk1.0-0 \
  libatspi2.0-0 \
  libcups2 \
  libdbus-1-3 \
  libdrm2 \
  libgbm1 \
  libgomp1 \
  libgtk-3-0 \
  libnspr4 \
  libnss3 \
  libu2f-udev \
  libvulkan1 \
  libwayland-client0 \
  libxcomposite1 \
  libxdamage1 \
  libxfixes3 \
  libxkbcommon0 \
  libxrandr2 \
  sudo \
  xdg-utils \
  && rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
    amd64) \
      curl -fsSLo /tmp/google-chrome.deb https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb \
      && apt-get update \
      && apt-get install -y --no-install-recommends /tmp/google-chrome.deb \
      && rm -f /tmp/google-chrome.deb \
      ;; \
    arm64) \
      apt-get update \
      && apt-get install -y --no-install-recommends chromium chromium-sandbox \
      && ln -sf /usr/bin/chromium /usr/bin/google-chrome \
      && ln -sf /usr/bin/chromium /usr/bin/google-chrome-stable \
      ;; \
    *) \
      echo "unsupported TARGETARCH: ${TARGETARCH:-$(dpkg --print-architecture)}" >&2 \
      && exit 1 \
      ;; \
  esac \
  && rm -rf /var/lib/apt/lists/*

RUN useradd -m linuxbrew -d /home/linuxbrew -s /bin/bash \
  && echo "linuxbrew ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/linuxbrew \
  && chmod 0440 /etc/sudoers.d/linuxbrew

USER linuxbrew
RUN /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
RUN /home/linuxbrew/.linuxbrew/bin/brew install gogcli himalaya
RUN chmod 777 /home/linuxbrew /home/linuxbrew/.linuxbrew

USER root
RUN useradd -m bot -d /home/bot -s /bin/bash \
  && mkdir -p /home/bot/.local/bin \
  && chown -R bot:bot /home/bot/.local

COPY --from=whisper-builder /out/whisper.cpp /opt/whisper.cpp
COPY --from=whisper-builder /out/samples /opt/aviary/samples
COPY --from=whisper-model /out /opt/aviary/models
RUN ln -s /opt/whisper.cpp/whisper-cli /usr/local/bin/whisper-cli

COPY --from=go-builder /out/aviary /usr/local/bin/aviary

USER bot
ENV PATH="/home/bot/.local/bin:/home/linuxbrew/.linuxbrew/bin:${PATH}"
ENV AVIARY_SANDBOX=true
ENV AVIARY_CONFIG_SERVER_EXTERNAL_ACCESS=true
# The bundled whisper model, used unless aviary.yaml sets transcription.model.
ENV AVIARY_CONFIG_TRANSCRIPTION_MODEL=/opt/aviary/models/ggml-base.en.bin
WORKDIR /home/bot
EXPOSE 1455 45289 16677

CMD ["aviary", "serve"]
