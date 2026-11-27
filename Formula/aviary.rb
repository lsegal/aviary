# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.5'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.5/aviary_v1.0.5_darwin_arm64.tar.gz'
      sha256 '10657755052cd2eb2d55f958036dd4cf1dad1cd0cad263ac82961c08a9f23661'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.5/aviary_v1.0.5_darwin_amd64.tar.gz'
      sha256 'e3ca487cc4e113b2e72ff00daa5b6c30637fc82bc0bb55e6b88acfc7b592a428'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.5/aviary_v1.0.5_linux_arm64.tar.gz'
      sha256 '46f6802c8dee9a69075e63d3508affd16a8b5c284926cb7e0960869b4c2f58a1'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.5/aviary_v1.0.5_linux_amd64.tar.gz'
      sha256 '0a562efb814ea6f1f8b5ee232ab1fa6ab7405540962e9223e73c1740b1303faa'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
