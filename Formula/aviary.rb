# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.2'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.2/aviary_v1.0.2_darwin_arm64.tar.gz'
      sha256 'dbedcfe38324f225a608eba8d70a49a4614926fb4db5f6cbb1af1d249abf44eb'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.2/aviary_v1.0.2_darwin_amd64.tar.gz'
      sha256 'c4a6591b49d4ba66034a146b651c61580256968f9c255ec3b5b7e8dccba9666e'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.2/aviary_v1.0.2_linux_arm64.tar.gz'
      sha256 'c968b89b17f60030a042d7725a66e34eb3c8666d8390ea1bf0e833bdfb8a5629'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.2/aviary_v1.0.2_linux_amd64.tar.gz'
      sha256 '8b9e8313f337bc0dc17bb4c206aed1c59bab08e3eb98fcd15eecff34070d84c1'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
